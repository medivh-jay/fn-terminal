package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fn-terminal/internal/broker"
	"fn-terminal/internal/config"
	"fn-terminal/internal/identity"
	"fn-terminal/internal/terminal"
	"fn-terminal/internal/wire"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestWebSocketExitReasonsAreAllowlisted(t *testing.T) {
	for _, tc := range []struct{ name, payload, want string }{
		{"shell", `{"reason":"shell_exit","message":"private details"}`, "shell_exit"},
		{"session", `{"reason":"session_end"}`, "session_end"},
		{"legacy", `{"message":"done"}`, "session_end"},
		{"unknown", `{"reason":"unexpected","secret":"private details"}`, "session_end"},
		{"malformed", `{"reason":"shell_exit"`, "session_end"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.Enabled = true
			peers := make(chan net.Conn, 1)
			h := New(Options{Config: cfg, Development: true, Connect: func(context.Context, identity.Claims, terminal.Size) (net.Conn, error) {
				local, peer := net.Pipe()
				peers <- peer
				return local, nil
			}})
			srv := httptest.NewServer(h)
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+Prefix+"/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {srv.URL}}, Subprotocols: []string{websocketProtocol, ticketProtocolPrefix + obtainTicket(t, h, srv.URL)}})
			if err != nil {
				t.Fatal(err)
			}
			defer ws.CloseNow()
			peer := <-peers
			defer peer.Close()
			if _, _, err := ws.Read(ctx); err != nil {
				t.Fatal(err)
			}
			go wire.Write(peer, wire.Exit, []byte(tc.payload))
			kind, payload, err := ws.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var ended struct{ Type, Reason, Message string }
			if err := json.Unmarshal(payload, &ended); err != nil || kind != websocket.MessageText || ended.Type != "exit" || ended.Reason != tc.want || ended.Message != "会话已结束" {
				t.Fatalf("unexpected exit control: %s (%v)", payload, err)
			}
			if strings.Contains(string(payload), "private details") || strings.Contains(string(payload), "secret") {
				t.Fatalf("broker details leaked: %s", payload)
			}
		})
	}
}

func TestRejectedOriginLogsBothHostsWithoutCredentials(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	c := config.Defaults()
	c.AllowedOrigins = []string{"https://trusted.example"}
	h := New(Options{Config: c, Logger: logger})
	r := httptest.NewRequest("GET", "http://localhost/app/fnterminal/ws", nil)
	r.Header.Set("Origin", "https://nas.example.com")
	r.Header.Set("Cookie", "NAS_SECRET_COOKIE")
	r.Header.Set("Authorization", "Bearer NAS_SECRET_TOKEN")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("status=%d", w.Code)
	}
	out := logs.String()
	if !strings.Contains(out, "stage=origin") || !strings.Contains(out, "nas.example.com") || !strings.Contains(out, "request_host=localhost") {
		t.Fatalf("missing useful handshake context: %s", out)
	}
	if strings.Contains(out, "NAS_SECRET") {
		t.Fatalf("credentials leaked: %s", out)
	}
}

func TestGatewayRewrittenHostRequiresExactConfiguredOrigin(t *testing.T) {
	for _, tc := range []struct {
		name    string
		allowed []string
		origin  string
		accept  bool
	}{
		{"automatic", nil, "https://nas.example.com", true},
		{"configured", []string{"https://nas.example.com"}, "https://nas.example.com", true},
		{"other-site", []string{"https://nas.example.com"}, "https://evil.example", false},
		{"suffix-spoof", []string{"https://nas.example.com"}, "https://nas.example.com.evil.example", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := config.Defaults()
			c.Enabled = true
			c.AllowedOrigins = tc.allowed
			peers := make(chan net.Conn, 1)
			h := New(Options{Config: c, Development: true, Connect: func(context.Context, identity.Claims, terminal.Size) (net.Conn, error) {
				local, peer := net.Pipe()
				peers <- peer
				return local, nil
			}})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.Host = "localhost" // Match the Host forwarded by fnOS's Unix gateway.
				h.ServeHTTP(w, r)
			}))
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var protocols []string
			if tc.accept {
				protocols = []string{websocketProtocol, ticketProtocolPrefix + obtainTicket(t, h, tc.origin)}
			}
			ws, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+Prefix+"/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {tc.origin}}, Subprotocols: protocols})
			if !tc.accept {
				if err == nil {
					ws.CloseNow()
					t.Fatal("untrusted origin accepted")
				}
				if resp == nil || resp.StatusCode != http.StatusForbidden || len(peers) != 0 {
					t.Fatalf("origin rejection did not precede broker connection: response=%v", resp)
				}
				return
			}
			if err != nil {
				t.Fatalf("configured origin rejected after Host rewrite: %v", err)
			}
			defer ws.CloseNow()
			defer (<-peers).Close()
			_, data, err := ws.Read(ctx)
			var message struct{ Type string }
			if err != nil || json.Unmarshal(data, &message) != nil || message.Type != "ready" {
				t.Fatalf("terminal did not become ready: %s %v", data, err)
			}
		})
	}
}

func TestBrokerConnectionFailureIsLoggedWithoutLeakingToResponse(t *testing.T) {
	var logs bytes.Buffer
	c := config.Defaults()
	c.Enabled = true
	path := filepath.Join(t.TempDir(), "missing.sock")
	h := New(Options{Config: c, Development: true, Logger: slog.New(slog.NewTextHandler(&logs, nil)), Connect: func(ctx context.Context, cl identity.Claims, s terminal.Size) (net.Conn, error) {
		return broker.Dial(ctx, path, cl, s)
	}})
	r := httptest.NewRequest("GET", "http://localhost/app/fnterminal/ws", nil)
	r.Header.Set("Origin", "http://localhost")
	r.Header.Set("Sec-WebSocket-Protocol", websocketProtocol+", "+ticketProtocolPrefix+obtainTicket(t, h, "http://localhost"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatalf("status=%d", w.Code)
	}
	if !strings.Contains(logs.String(), "stage=broker") || !strings.Contains(logs.String(), "missing.sock") {
		t.Fatalf("missing connector cause: %s", logs.String())
	}
	if strings.Contains(w.Body.String(), path) {
		t.Fatal("internal path exposed in browser response")
	}
}

func TestProductionRejectsMissingIdentityAndCrossOriginBeforeConnecting(t *testing.T) {
	c := config.Defaults()
	c.Enabled = true
	h := New(Options{Config: c, AppUID: 2000, Connect: func(context.Context, identity.Claims, terminal.Size) (net.Conn, error) {
		t.Fatal("unauthorized request reached broker")
		return nil, nil
	}})
	r := httptest.NewRequest("GET", "http://nas/app/fnterminal/api/me", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("missing identity: %d", w.Code)
	}
	r = httptest.NewRequest("GET", "http://nas/app/fnterminal/ws", nil)
	r.Header.Set("Origin", "https://evil.example")
	r.Header.Set("X-Trim-Userid", "1000")
	r.Header.Set("X-Trim-Username", "medivh")
	r.Header.Set("X-Trim-Isadmin", "false")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross origin: %d", w.Code)
	}
}
func TestDevelopmentRejectsDNSRebindingHost(t *testing.T) {
	h := New(Options{Config: config.Defaults(), Development: true})
	r := httptest.NewRequest("GET", "http://attacker.example/app/fnterminal/api/me", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("accepted non-loopback Host: %d", w.Code)
	}
}
func TestOutputAcknowledgementDoesNotAdmitOtherControlMessages(t *testing.T) {
	if _, e := decodeControl([]byte(`{"type":"ack"}`)); e != nil {
		t.Fatal(e)
	}
	if _, e := decodeControl([]byte(`{"type":"exec","command":"id"}`)); e == nil {
		t.Fatal("admitted command control")
	}
}
func TestRealWebSocketRoundTripAndInvalidControl(t *testing.T) {
	path := filepath.Join(t.TempDir(), "b.sock")
	l, e := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer l.Close()
	c := config.Defaults()
	c.Enabled = true
	b := broker.New(c, uint32(os.Getuid()))
	b.Development = true
	go b.Serve(ctx, l)
	h := New(Options{Config: c, Development: true, Connect: func(ctx context.Context, cl identity.Claims, s terminal.Size) (net.Conn, error) {
		return broker.Dial(ctx, path, cl, s)
	}})
	srv := httptest.NewServer(h)
	defer srv.Close()
	resp, e := http.Get(srv.URL + "/app/fnterminal/api/me")
	if e != nil {
		t.Fatal(e)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var me struct {
		Username   string `json:"username"`
		UID        uint32 `json:"uid"`
		MaxPerUser int    `json:"max_per_user"`
	}
	if e = json.Unmarshal(body, &me); e != nil || me.UID != uint32(os.Getuid()) || me.MaxPerUser != c.MaxPerUser {
		t.Fatalf("wrong current identity: %s %v", body, e)
	}
	wsctx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	ws, _, e := websocket.Dial(wsctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/app/fnterminal/ws?uid=0", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {srv.URL}}, Subprotocols: []string{websocketProtocol, ticketProtocolPrefix + obtainTicket(t, h, srv.URL)}})
	if e != nil {
		t.Fatal(e)
	}
	defer ws.CloseNow()
	if e = ws.Write(wsctx, websocket.MessageBinary, []byte("printf '\\nWEB_OK中文\\n'\n")); e != nil {
		t.Fatal(e)
	}
	var output []byte
	for !strings.Contains(string(output), "\r\nWEB_OK中文\r\n") {
		typ, p, e := ws.Read(wsctx)
		if e != nil {
			t.Fatal(e)
		}
		if typ == websocket.MessageBinary {
			output = append(output, p...)
			if e = ws.Write(wsctx, websocket.MessageText, []byte(`{"type":"ack"}`)); e != nil {
				t.Fatal(e)
			}
		}
	}
	if e = ws.Write(wsctx, websocket.MessageText, []byte(`{"type":"resize","rows":0,"cols":80}`)); e != nil {
		t.Fatal(e)
	}
	for {
		_, _, e = ws.Read(wsctx)
		if e != nil {
			break
		}
	}
	if websocket.CloseStatus(e) != websocket.StatusPolicyViolation {
		t.Fatalf("invalid resize not rejected: %v", e)
	}
}
