package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

type hostedContextKey struct{}
type hostedIdentity struct{ User, Admission, LimiterKey string }
type HostedOptions struct {
	IPLimitKey   string
	Mode         string
	PublicOrigin string
	State        *OperationalStore
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
	if options.State != nil {
		service.State = options.State
	}
	base := NewHandler(service)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method == "GET" && r.URL.Path == "/api/client-config" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"mode": options.Mode, "questions_paused": service.Config.QuestionsPaused})
			return
		}
		if r.URL.Path == "/api/questions" {
			if r.Method == http.MethodPost && service.Config.QuestionsPaused {
				publicError(w, http.StatusServiceUnavailable, "Questions are temporarily unavailable")
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
			defer cancel()
			origin := options.PublicOrigin
			if origin == "" {
				origin = service.Config.PublicOrigin
			}
			if !websiteSubmission(r, origin) {
				publicError(w, http.StatusForbidden, "Please submit your question through this website.")
				return
			}

			key, err := networkLimiterKey(r, options.IPLimitKey)
			if err != nil {
				service.logFailure("identify question network", err)
				publicError(w, 503, "Questions are temporarily unavailable. Please try again later.")
				return
			}
			r = r.WithContext(context.WithValue(ctx, hostedContextKey{}, &hostedIdentity{LimiterKey: key, Admission: randomToken()}))
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

// This is a best-effort website boundary, not proof that a human used the UI.
// Scripts can imitate these headers. No CORS response permits other websites.
func websiteSubmission(r *http.Request, origin string) bool {
	if r.Method != http.MethodPost || origin == "" || r.Header.Get("Origin") != origin ||
		r.Header.Get("Sec-Fetch-Site") != "same-origin" || r.Header.Get("Sec-Fetch-Mode") != "same-origin" ||
		r.Header.Get("Sec-Fetch-Dest") != "empty" || r.Header.Get("X-Baseball-Website") != "1" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mediaType == "application/json"
}
