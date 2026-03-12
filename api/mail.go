package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"
)

func (s *Server) mailpitBaseURL() string {
	httpPort, err := s.queries.GetSetting(context.Background(), "mailpit_http_port")
	if err != nil {
		httpPort = "8025"
	}
	return "http://127.0.0.1:" + httpPort
}

func (s *Server) handleMailConfig(w http.ResponseWriter, r *http.Request) {
	httpPort, err := s.queries.GetSetting(context.Background(), "mailpit_http_port")
	if err != nil {
		httpPort = "8025"
	}
	smtpPort, err := s.queries.GetSetting(context.Background(), "mailpit_smtp_port")
	if err != nil {
		smtpPort = "1025"
	}
	writeJSON(w, map[string]string{
		"http_port": httpPort,
		"smtp_port": smtpPort,
	})
}

// handleMailProxy reverse-proxies all /api/mail/* requests to the Mailpit
// HTTP API at 127.0.0.1:{httpPort}. It strips the /api/mail prefix so that
// e.g. GET /api/mail/api/v1/messages → GET /api/v1/messages on Mailpit.
func (s *Server) handleMailProxy(w http.ResponseWriter, r *http.Request) {
	base := s.mailpitBaseURL()
	target, err := url.Parse(base)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid mailpit url: %v", err), http.StatusInternalServerError)
		return
	}

	// Strip /api/mail prefix.
	stripped := strings.TrimPrefix(r.URL.Path, "/api/mail")
	if stripped == "" {
		stripped = "/"
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.URL.Path = stripped
			req.URL.RawQuery = r.URL.RawQuery
			req.Host = target.Host
			// Remove hop-by-hop headers.
			req.Header.Del("X-Forwarded-For")
		},
	}
	proxy.ServeHTTP(w, r)
}

// handleMailWS proxies the browser WebSocket at /ws/mail to
// ws://127.0.0.1:{httpPort}/api/events (Mailpit's event stream).
func (s *Server) handleMailWS(w http.ResponseWriter, r *http.Request) {
	base := s.mailpitBaseURL()
	mailpitWS := strings.Replace(base, "http://", "ws://", 1) + "/api/events"

	// Upgrade browser connection.
	clientConn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("mail ws: upgrade: %v", err)
		return
	}
	defer clientConn.Close()

	// Dial Mailpit.
	mailpitConn, _, err := websocket.DefaultDialer.Dial(mailpitWS, nil)
	if err != nil {
		log.Printf("mail ws: dial mailpit: %v", err)
		return
	}
	defer mailpitConn.Close()

	errc := make(chan error, 2)

	// Mailpit → browser.
	go func() {
		for {
			mt, msg, err := mailpitConn.ReadMessage()
			if err != nil {
				errc <- err
				return
			}
			if err := clientConn.WriteMessage(mt, msg); err != nil {
				errc <- err
				return
			}
		}
	}()

	// Browser → Mailpit (ping/pong, close frames).
	go func() {
		for {
			mt, msg, err := clientConn.ReadMessage()
			if err != nil {
				errc <- err
				return
			}
			if err := mailpitConn.WriteMessage(mt, msg); err != nil {
				errc <- err
				return
			}
		}
	}()

	<-errc
}
