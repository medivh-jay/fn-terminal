package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fn-terminal/internal/config"
	"fn-terminal/internal/identity"
	"fn-terminal/internal/terminal"
	"github.com/coder/websocket"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func ticketRequest(origin string) *http.Request {
	b, _ := json.Marshal(map[string]string{"origin": origin})
	r := httptest.NewRequest("POST", "http://localhost"+Prefix+"/api/ws-ticket", strings.NewReader(string(b)))
	r.Header.Set("Origin", origin)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Fnterminal-Request", "1")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	return r
}

func obtainTicket(t *testing.T, h http.Handler, origin string) string {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, ticketRequest(origin))
	var response struct {
		Ticket string `json:"ticket"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Ticket == "" {
		t.Fatalf("ticket response: %d %s", w.Code, w.Body)
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("ticket response cacheable or cross-origin readable")
	}
	return response.Ticket
}

func TestTicketEndpointRejectsCrossSiteSimpleRequestsAndUnauthenticatedUsers(t *testing.T) {
	for _, name := range []string{"simple-form", "cross-site", "same-site", "mismatched-origin", "preflight", "disabled", "missing-identity"} {
		t.Run(name, func(t *testing.T) {
			c := config.Defaults()
			c.Enabled = name != "disabled"
			h := New(Options{Config: c, Development: name != "missing-identity"})
			r := ticketRequest("https://nas.fnos.net")
			switch name {
			case "simple-form":
				r.Header.Del("X-Fnterminal-Request")
			case "cross-site", "same-site":
				r.Header.Set("Sec-Fetch-Site", name)
			case "mismatched-origin":
				r.Header.Set("Origin", "https://evil.example")
			case "preflight":
				r.Method = "OPTIONS"
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code < 400 || w.Header().Get("Access-Control-Allow-Origin") != "" || strings.Contains(w.Body.String(), `"ticket"`) {
				t.Fatalf("unsafe ticket issuance: %d %s", w.Code, w.Body)
			}
		})
	}
}

func TestAutomaticLANAndFNIDConnectionsWithRewrittenHost(t *testing.T) {
	for _, origin := range []string{"http://192.168.1.20:5666", "https://nas.fnos.net", "http://[fd00::1234]:5666"} {
		t.Run(origin, func(t *testing.T) {
			c := config.Defaults()
			c.Enabled = true
			peers := make(chan net.Conn, 1)
			h := New(Options{Config: c, Development: true, Connect: func(context.Context, identity.Claims, terminal.Size) (net.Conn, error) {
				local, peer := net.Pipe()
				peers <- peer
				return local, nil
			}})
			token := obtainTicket(t, h, origin)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { r.Host = "localhost"; h.ServeHTTP(w, r) }))
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			dial := func(token, origin string) (*websocket.Conn, *http.Response, error) {
				return websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+Prefix+"/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {origin}}, Subprotocols: []string{"fnterminal.v1", "fnterminal-ticket." + token}})
			}
			for _, bad := range []struct{ token, origin string }{{"invalid", origin}, {token, "https://evil.example"}} {
				ws, resp, err := dial(bad.token, bad.origin)
				if err == nil {
					ws.CloseNow()
					t.Fatal("unbound connection accepted")
				}
				if resp == nil || resp.StatusCode != 403 || len(peers) != 0 {
					t.Fatal("rejection reached broker")
				}
			}
			ws, _, err := dial(token, origin)
			if err != nil {
				t.Fatalf("automatic connection failed: %v", err)
			}
			defer ws.CloseNow()
			defer (<-peers).Close()
			_, data, err := ws.Read(ctx)
			if err != nil || !strings.Contains(string(data), `"type":"ready"`) || ws.Subprotocol() != "fnterminal.v1" {
				t.Fatalf("not ready: %s %v", data, err)
			}
			replay, resp, err := dial(token, origin)
			if err == nil {
				replay.CloseNow()
				t.Fatal("ticket replay accepted")
			}
			if resp == nil || resp.StatusCode != 403 || len(peers) != 0 {
				t.Fatal("ticket replay reached broker")
			}
		})
	}
}

func TestRejectedTicketDoesNotReachBrokerOrLeakItsSecret(t *testing.T) {
	var logs bytes.Buffer
	c := config.Defaults()
	c.Enabled = true
	h := New(Options{Config: c, Development: true, Logger: slog.New(slog.NewTextHandler(&logs, nil)), Connect: func(context.Context, identity.Claims, terminal.Size) (net.Conn, error) {
		t.Fatal("rejected ticket reached broker")
		return nil, nil
	}})
	token := obtainTicket(t, h, "https://nas.fnos.net")
	for _, tc := range []struct{ protocols, origin string }{
		{websocketProtocol + ", " + ticketProtocolPrefix + token, "https://evil.example"},
		{websocketProtocol + ", " + ticketProtocolPrefix + token + ", " + ticketProtocolPrefix + token, "https://nas.fnos.net"},
		{websocketProtocol + ", " + websocketProtocol, "https://nas.fnos.net"},
	} {
		r := httptest.NewRequest("GET", "http://localhost"+Prefix+"/ws", nil)
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Sec-WebSocket-Protocol", tc.protocols)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 || strings.Contains(w.Body.String(), token) || strings.Contains(logs.String(), token) {
			t.Fatalf("unsafe ticket rejection: %d %s %s", w.Code, w.Body, logs.String())
		}
	}
}
