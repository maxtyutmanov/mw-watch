// Package web serves the stats UI and JSON API on a loopback address only.
package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"time"

	"github.com/maxtyutmanov/mw-watch/internal/store"
)

//go:embed static
var staticFS embed.FS

// CheckLoopback rejects listen addresses that would expose the server beyond this machine.
func CheckLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("refusing to listen on %q: only loopback addresses are allowed", addr)
}

// Handler returns the HTTP handler for the UI and the /api/stats endpoint.
func Handler(st *store.Store) http.Handler {
	static, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(static))
	mux.HandleFunc("GET /api/stats", func(w http.ResponseWriter, r *http.Request) {
		stats(st, w, r)
	})
	return guard(mux)
}

// guard only answers requests addressed to a loopback host name, which stops
// DNS-rebinding pages from reading the API, and sets restrictive headers.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func stats(st *store.Store, w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, err := time.Parse(time.RFC3339, q.Get("from"))
	if err != nil {
		http.Error(w, "bad from: "+err.Error(), http.StatusBadRequest)
		return
	}
	to, err := time.Parse(time.RFC3339, q.Get("to"))
	if err != nil {
		http.Error(w, "bad to: "+err.Error(), http.StatusBadRequest)
		return
	}
	g, err := store.ParseGranularity(q.Get("granularity"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	loc := time.Local
	if tz := q.Get("tz"); tz != "" {
		if loc, err = time.LoadLocation(tz); err != nil {
			http.Error(w, "bad tz: "+err.Error(), http.StatusBadRequest)
			return
		}
	}
	points, err := st.Aggregate(from, to, g, loc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"points": points})
}
