package main

import (
	"embed"
	"encoding/json"
	"io"
	"net/http"
)

//go:embed static/index.html
var browserFiles embed.FS

func NewHandler(service *QueryService) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		body, _ := browserFiles.ReadFile("static/index.html")
		w.Write(body)
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
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'")
		mux.ServeHTTP(w, r)
	})
}
