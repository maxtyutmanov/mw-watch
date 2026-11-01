package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/maxtyutmanov/mw-watch/internal/store"
)

func TestCheckLoopback(t *testing.T) {
	for addr, ok := range map[string]bool{
		"127.0.0.1:47823": true,
		"localhost:1":     true,
		"[::1]:80":        true,
		"0.0.0.0:47823":   false,
		":47823":          false,
		"192.168.1.5:80":  false,
	} {
		if err := CheckLoopback(addr); (err == nil) != ok {
			t.Errorf("CheckLoopback(%q) = %v", addr, err)
		}
	}
}

func TestHandler(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	start := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	st.Mark(start.Unix(), true, true)
	h := Handler(st)

	get := func(host, url string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", url, nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	if rec := get("evil.example:47823", "/api/stats"); rec.Code != http.StatusForbidden {
		t.Errorf("foreign host: status %d", rec.Code)
	}
	if rec := get("localhost:47823", "/"); rec.Code != http.StatusOK {
		t.Errorf("index: status %d", rec.Code)
	}

	rec := get("127.0.0.1:47823", "/api/stats?from=2026-11-01T10:00:00Z&to=2026-11-01T11:00:00Z&granularity=1h&tz=UTC")
	if rec.Code != http.StatusOK {
		t.Fatalf("stats: status %d: %s", rec.Code, rec.Body)
	}
	var body struct{ Points []store.Point }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Points) != 1 || body.Points[0].ActiveMin != 1 || body.Points[0].ScreenMin != 1 {
		t.Fatalf("points = %+v", body.Points)
	}

	if rec := get("localhost", "/api/stats?from=x&to=y&granularity=1h"); rec.Code != http.StatusBadRequest {
		t.Errorf("bad params: status %d", rec.Code)
	}
}
