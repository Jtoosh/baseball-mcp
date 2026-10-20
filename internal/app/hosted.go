package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"strings"
	"time"

	clerk "github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/jwks"
	clerkjwt "github.com/clerk/clerk-sdk-go/v2/jwt"
	"github.com/clerk/clerk-sdk-go/v2/session"
	"github.com/clerk/clerk-sdk-go/v2/user"
)

type ClerkAuth struct {
	Issuer, Origin string
	JWK            *clerk.JSONWebKey
	keys           *jwks.Client
	sessions       *session.Client
	users          *user.Client
}

func NewClerkAuth(key, issuer, origin string, config *clerk.ClientConfig) *ClerkAuth {
	if config == nil {
		config = &clerk.ClientConfig{BackendConfig: clerk.BackendConfig{Key: &key, HTTPClient: &http.Client{Timeout: 10 * time.Second}}}
	}
	return &ClerkAuth{Issuer: issuer, Origin: origin, keys: jwks.NewClient(config), sessions: session.NewClient(config), users: user.NewClient(config)}
}

func (a *ClerkAuth) Authenticate(ctx context.Context, r *http.Request) (string, error) {
	if origin := r.Header.Get("Origin"); origin != "" && origin != a.Origin {
		return "", errors.New("origin denied")
	}
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, "Bearer ") {
		return "", errors.New("missing session")
	}
	claims, err := clerkjwt.Verify(ctx, &clerkjwt.VerifyParams{Token: strings.TrimPrefix(value, "Bearer "), JWK: a.JWK, JWKSClient: a.keys, AuthorizedPartyHandler: func(p string) bool { return p == a.Origin }})
	if err != nil {
		return "", err
	}
	if claims.Issuer != a.Issuer || claims.Subject == "" || claims.SessionID == "" {
		return "", errors.New("session binding denied")
	}
	s, err := a.sessions.Get(ctx, claims.SessionID)
	if err != nil {
		return "", err
	}
	if s.Status != "active" || s.UserID != claims.Subject {
		return "", errors.New("inactive session")
	}
	u, err := a.users.Get(ctx, claims.Subject)
	if err != nil {
		return "", err
	}
	var eligibility struct {
		Approved bool `json:"baseball_access"`
	}
	if json.Unmarshal(u.PrivateMetadata, &eligibility) != nil || !eligibility.Approved {
		return "", errors.New("user approval required")
	}
	if u.ID != claims.Subject || u.Banned || u.Locked {
		return "", errors.New("user denied")
	}
	return claims.Subject, nil
}

type hostedContextKey struct{}
type hostedIdentity struct{ User, Admission, LimiterKey string }
type HostedOptions struct {
	IPLimitKey     string
	Mode           string
	Auth           *ClerkAuth
	PublishableKey string
	State          *OperationalStore
}

func publicError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(Outcome{Kind: "error", Message: message})
}

func NewHostedHandler(service *QueryService, options HostedOptions) http.Handler {
	service.Config.Environment = options.Mode
	service.Config.DeveloperSQL = false
	service.Config.DeveloperSkipClarification = false
	if options.Auth != nil {
		service.Config.ClerkIssuer = options.Auth.Issuer
	}
	if options.State != nil {
		service.State = options.State
	}
	base := NewHandler(service)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method == "GET" && r.URL.Path == "/api/client-config" {
			w.Header().Set("Content-Type", "application/json")
			issuer := ""
			if options.Auth != nil {
				issuer = options.Auth.Issuer
			}
			json.NewEncoder(w).Encode(map[string]string{"mode": options.Mode, "publishable_key": options.PublishableKey, "issuer": issuer})
			return
		}
		if r.URL.Path == "/api/questions" {
			ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
			defer cancel()
			if options.Auth == nil {
				publicError(w, 503, "Sign-in is temporarily unavailable. Please try again later.")
				return
			}
			id, err := options.Auth.Authenticate(ctx, r)
			if err != nil {
				service.logFailure("authorize question", err)
				publicError(w, 401, "Please sign in with an invited account to ask a question.")
				return
			}
			key, err := networkLimiterKey(r, options.IPLimitKey)
			if err != nil {
				service.logFailure("identify question network", err)
				publicError(w, 503, "Questions are temporarily unavailable. Please try again later.")
				return
			}
			r = r.WithContext(context.WithValue(ctx, hostedContextKey{}, &hostedIdentity{User: id, LimiterKey: key, Admission: randomToken()}))
		}
		base.ServeHTTP(w, r)
	})
}

// Only the Vercel ingress may supply this header; never expose the hosted
// origin directly. Do not fall back to arbitrary forwarding headers or TCP peers.
func networkLimiterKey(r *http.Request, secret string) (string, error) {
	if len(secret) < 32 {
		return "", errors.New("network limiter key unavailable")
	}
	values := r.Header.Values("X-Vercel-Forwarded-For")
	if len(values) != 1 {
		return "", errors.New("network address unavailable")
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(values[0]))
	if err != nil || addr.Zone() != "" || addr.IsUnspecified() || addr.IsMulticast() {
		return "", errors.New("network address invalid")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("baseball-network-v1:" + addr.Unmap().String()))
	return "ip:v1:" + hex.EncodeToString(mac.Sum(nil)), nil
}
