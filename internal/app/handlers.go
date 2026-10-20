package app

import (
	"embed"
	"encoding/json"
	"io"
	"net/http"
)

//go:embed static/index.html static/app.js static/app.css static/vendor/*.js
var browserFiles embed.FS

func NewHandler(service *QueryService) http.Handler {
	mux := http.NewServeMux()
	assets := map[string]string{
		"app.js": "static/app.js", "app.css": "static/app.css",
		"marked.js": "static/vendor/marked.js", "purify.js": "static/vendor/purify.js",
	}
	for name, path := range assets {
		mux.HandleFunc("GET /assets/"+name, func(w http.ResponseWriter, r *http.Request) {
			contentType := "text/javascript; charset=utf-8"
			if name == "app.css" {
				contentType = "text/css; charset=utf-8"
			}
			w.Header().Set("Content-Type", contentType)
			body, _ := browserFiles.ReadFile(path)
			w.Write(body)
		})
	}
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		body, _ := browserFiles.ReadFile("static/index.html")
		w.Write(body)
	})
	mux.HandleFunc("GET /api/client-config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"mode": "local"})
	})
	mux.HandleFunc("POST /api/questions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		r.Body = http.MaxBytesReader(w, r.Body, 16384)
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		var req QuestionRequest
		if err := dec.Decode(&req); err != nil {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(Outcome{Kind: "error", Message: "Please send a valid question."})
			return
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(Outcome{Kind: "error", Message: "Please send a single question."})
			return
		}
		json.NewEncoder(w).Encode(service.Ask(r.Context(), req))
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		policy := "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'none'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'"
		if service.Config.Environment == "production" || service.Config.Environment == "preview" {
			issuer := service.Config.ClerkIssuer
			policy = "default-src 'self'; script-src 'self' " + issuer + " https://challenges.cloudflare.com https://*.protect.clerk.com; style-src 'self' 'unsafe-inline'; connect-src 'self' " + issuer + " https://*.protect.clerk.com:*; img-src 'self' https://img.clerk.com data:; worker-src 'self' blob:; frame-src " + issuer + " https://challenges.cloudflare.com https://*.protect.clerk.com; form-action 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'"
		}
		w.Header().Set("Content-Security-Policy", policy)
		mux.ServeHTTP(w, r)
	})
}
