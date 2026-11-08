package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"github.com/danielgormly/devctl/dumps"
)

func TestHandlerCrossOriginWrites(t *testing.T) {
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/databases/{engine}/query", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := s.handler()

	tests := []struct {
		name      string
		host      string
		origin    string
		fetchSite string
		want      int
	}{
		{"cross-site page", "127.0.0.1:4000", "https://evil.example", "cross-site", http.StatusForbidden},
		{"same-site test site", "devctl.test", "https://shop.test", "same-site", http.StatusForbidden},
		{"other test site from a browser without Sec-Fetch-Site", "devctl.test", "https://shop.test", "", http.StatusForbidden},
		{"dashboard on 127.0.0.1", "127.0.0.1:4000", "http://127.0.0.1:4000", "same-origin", http.StatusNoContent},
		{"dashboard on localhost", "localhost:4000", "http://localhost:4000", "same-origin", http.StatusNoContent},
		{"dashboard through the devctl.test vhost", "devctl.test", "https://devctl.test", "same-origin", http.StatusNoContent},
		{"devctl.test vhost from a browser without Sec-Fetch-Site", "devctl.test", "https://devctl.test", "", http.StatusNoContent},
		{"CLI with no Origin or Sec-Fetch-Site", "127.0.0.1:4000", "", "", http.StatusNoContent},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/databases/sqlite/query", strings.NewReader(`{"sql":"DROP TABLE users"}`))
			req.Host = tc.host
			req.Header.Set("Content-Type", "text/plain")
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.fetchSite != "" {
				req.Header.Set("Sec-Fetch-Site", tc.fetchSite)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestDumpsWebSocketOrigin(t *testing.T) {
	s := &Server{mux: http.NewServeMux(), dumps: &dumps.Server{Hub: dumps.NewHub()}}
	s.mux.HandleFunc("GET /ws/dumps", s.handleDumpsWS)
	srv := httptest.NewServer(s.handler())
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/dumps"

	tests := []struct {
		name   string
		origin string
		want   int
	}{
		{"other site", "https://evil.example", http.StatusForbidden},
		{"same-site test site", "https://shop.test", http.StatusForbidden},
		{"dashboard origin", srv.URL, http.StatusSwitchingProtocols},
		{"no Origin", "", http.StatusSwitchingProtocols},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			header := http.Header{}
			if tc.origin != "" {
				header.Set("Origin", tc.origin)
			}
			conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
			if conn != nil {
				conn.Close()
			}
			if resp == nil {
				t.Fatalf("dial: %v", err)
			}
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d (err %v)", resp.StatusCode, tc.want, err)
			}
		})
	}
}
