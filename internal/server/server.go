package server

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fn-terminal/internal/config"
	"fn-terminal/internal/identity"
	"fn-terminal/internal/terminal"
	"fn-terminal/internal/wire"
	"github.com/coder/websocket"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const Prefix = "/app/fnterminal"

// Gateways commonly drop tunnels silent for about a minute, so ping well inside
// that window. Pings stop at the web process and never refresh the broker's idle
// timer. Several misses are tolerated so a brief network stall keeps its shell.
const (
	keepAliveInterval  = 25 * time.Second
	keepAliveTimeout   = 10 * time.Second
	keepAliveMaxMissed = 3
)

// Written into the terminal because the page otherwise keeps showing a live-looking
// prompt while it discards every keystroke.
var expiryNotices = map[string]string{
	wire.ReasonIdleTimeout: "\r\n\x1b[33m[飞牛终端] 长时间无操作，会话已断开。请点击右上角“重新连接”打开新的 Shell。\x1b[0m\r\n",
	wire.ReasonMaxDuration: "\r\n\x1b[33m[飞牛终端] 已达到会话时长上限，会话已断开。请点击右上角“重新连接”打开新的 Shell。\x1b[0m\r\n",
}

//go:embed static
var assets embed.FS

type Connector func(context.Context, identity.Claims, terminal.Size) (net.Conn, error)
type Options struct {
	Config      config.Config
	AppUID      uint32
	Development bool
	Connect     Connector
	Logger      *slog.Logger
}

func New(o Options) http.Handler {
	logger := o.Logger
	if logger == nil {
		logger = slog.Default()
	}
	tickets := newTicketStore()
	root, _ := fs.Sub(assets, "static")
	files := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; font-src 'self'; frame-ancestors 'self'; base-uri 'none'; object-src 'none'")
		if o.Development && !loopbackHost(r.Host) {
			http.Error(w, "开发服务仅接受本机访问", http.StatusForbidden)
			return
		}
		method := "GET"
		if r.URL.Path == Prefix+"/api/ws-ticket" {
			method = "POST"
		}
		if r.Method != method {
			w.Header().Set("Allow", method)
			http.Error(w, "method not allowed", 405)
			return
		}
		// Validate the browser origin before allocating a PTY or resolving a user.
		if r.URL.Path == Prefix+"/ws" && !validOrigin(r, o.Config.AllowedOrigins) {
			logger.Warn("terminal websocket rejected", "stage", "origin", "request_host", r.Host, "origin", r.Header.Get("Origin"))
			http.Error(w, "origin rejected", 403)
			return
		}
		c, e := claims(r, o)
		if e != nil {
			http.Error(w, "请从已登录的飞牛桌面打开此应用", 401)
			return
		}
		p := identity.Policy{MinUID: o.Config.MinUID, DeniedUID: o.AppUID, AllowedUsers: o.Config.AllowedUsers}
		if o.Development {
			p.DeniedUID = 0
		}
		if _, e = identity.Resolve(c, p); e != nil {
			http.Error(w, "此系统账号未获准使用交互式终端", 403)
			return
		}
		switch r.URL.Path {
		case Prefix + "/api/ws-ticket":
			if !o.Config.Enabled {
				http.Error(w, "终端尚未由管理员启用", 403)
				return
			}
			issueConnectionTicket(w, r, c, o.Config.AllowedOrigins, tickets)
		case Prefix + "/api/me":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(struct {
				identity.Claims
				Enabled     bool `json:"enabled"`
				Development bool `json:"development"`
				IdleSeconds int  `json:"idle_seconds"`
				MaxSeconds  int  `json:"max_seconds"`
				MaxPerUser  int  `json:"max_per_user"`
			}{c, o.Config.Enabled, o.Development, o.Config.IdleSeconds, o.Config.MaxSeconds, o.Config.MaxPerUser})
		case Prefix + "/ws":
			if !o.Config.Enabled {
				http.Error(w, "终端尚未由管理员启用", 403)
				return
			}
			if !tickets.consume(requestTicket(r), c, r.Header.Get("Origin"), time.Now()) {
				logger.Warn("terminal websocket rejected", "stage", "ticket", "uid", c.UID)
				http.Error(w, "连接凭据无效或已过期，请重新连接", 403)
				return
			}
			if o.Connect == nil {
				http.Error(w, "终端服务不可用", 503)
				return
			}
			serveWS(w, r, c, o.Connect, logger)
		case "/", Prefix:
			http.Redirect(w, r, Prefix+"/", http.StatusTemporaryRedirect)
		default:
			if !strings.HasPrefix(r.URL.Path, Prefix+"/") {
				http.NotFound(w, r)
				return
			}
			http.StripPrefix(Prefix, files).ServeHTTP(w, r)
		}
	})
}
func claims(r *http.Request, o Options) (identity.Claims, error) {
	if !o.Development {
		return identity.FromGateway(r.Header)
	}
	a, e := identity.Lookup(uint32(os.Geteuid()))
	return identity.Claims{UID: a.UID, Username: a.Username}, e
}
func loopbackHost(host string) bool {
	h, _, e := net.SplitHostPort(host)
	if e != nil {
		h = host
	}
	h = strings.Trim(h, "[]")
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
func validOrigin(r *http.Request, allowed []string) bool {
	if len(r.Header.Values("Origin")) != 1 {
		return false
	}
	origin := r.Header.Get("Origin")
	if !wellFormedOrigin(origin) {
		return false
	}
	if len(allowed) > 0 {
		for _, s := range allowed {
			if origin == s {
				return true
			}
		}
		return false
	}
	return true // A bound one-use ticket, rather than a rewritten Host, proves the source.
}

func wellFormedOrigin(origin string) bool {
	if len(origin) > 2048 || strings.Contains(origin, "*") {
		return false
	}
	u, e := url.Parse(origin)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.Opaque == ""
}

type control struct {
	Type string `json:"type"`
	Rows uint16 `json:"rows,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
}

func decodeControl(b []byte) (control, error) {
	var c control
	if len(b) > 256 {
		return c, errors.New("control too large")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(&c); e != nil {
		return c, e
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return c, errors.New("extra data")
	}
	if c.Type == "ack" && c.Rows == 0 && c.Cols == 0 {
		return c, nil
	}
	if c.Type != "resize" || !(terminal.Size{Rows: c.Rows, Cols: c.Cols}).Valid() {
		return c, errors.New("invalid resize")
	}
	return c, nil
}

// keepAlive pings until ctx ends and reports a peer that stopped answering.
// ws.Ping needs a concurrent ws.Read to observe the pong.
func keepAlive(ctx context.Context, ws *websocket.Conn, interval, timeout time.Duration, maxMissed int, dead func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	missed := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		c, stop := context.WithTimeout(ctx, timeout)
		e := ws.Ping(c)
		stop()
		if ctx.Err() != nil {
			return
		}
		if e == nil {
			missed = 0
			continue
		}
		if missed++; missed >= maxMissed {
			dead()
			return
		}
	}
}
func serveWS(w http.ResponseWriter, r *http.Request, c identity.Claims, connect Connector, logger *slog.Logger) {
	conn, e := connect(r.Context(), c, terminal.Size{Rows: 24, Cols: 80})
	if e != nil {
		logger.Warn("terminal websocket failed", "stage", "broker", "uid", c.UID, "error", e)
		http.Error(w, "无法创建终端：请检查启用状态、账号权限或会话上限", 503)
		return
	}
	defer conn.Close()
	// Identity + Origin are bound by the consumed ticket; the gateway may rewrite Host.
	ws, e := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, Subprotocols: []string{websocketProtocol}, CompressionMode: websocket.CompressionDisabled})
	if e != nil {
		logger.Warn("terminal websocket failed", "stage", "upgrade", "uid", c.UID, "error", e)
		return
	}
	defer ws.CloseNow()
	ws.SetReadLimit(wire.MaxPayload)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	send := func(k websocket.MessageType, b []byte) error {
		c, stop := context.WithTimeout(ctx, 5*time.Second)
		defer stop()
		return ws.Write(c, k, b)
	}
	ready, _ := json.Marshal(map[string]any{"type": "ready", "username": c.Username, "uid": c.UID})
	if send(websocket.MessageText, ready) != nil {
		return
	}
	readerDone := make(chan struct{})
	// A browser acknowledges rendered chunks; at most 8 PTY chunks can queue in xterm.
	credits := make(chan struct{}, 8)
	for i := 0; i < 8; i++ {
		credits <- struct{}{}
	}
	go func() {
		defer close(readerDone)
		defer conn.Close()
		for {
			k, b, e := ws.Read(ctx)
			if e != nil {
				return
			}
			kind := wire.Data
			payload := b
			if k == websocket.MessageText {
				ctl, e := decodeControl(b)
				if e != nil {
					_ = ws.Close(websocket.StatusPolicyViolation, "无效的终端控制消息")
					return
				}
				if ctl.Type == "ack" {
					select {
					case credits <- struct{}{}:
					default:
					}
					continue
				}
				kind = wire.Resize
				payload, _ = json.Marshal(terminal.Size{Rows: ctl.Rows, Cols: ctl.Cols})
			}
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if wire.Write(conn, kind, payload) != nil {
				return
			}
		}
	}()
	defer func() { cancel(); conn.Close(); ws.CloseNow(); <-readerDone }()
	go keepAlive(ctx, ws, keepAliveInterval, keepAliveTimeout, keepAliveMaxMissed, func() {
		logger.Info("terminal websocket peer unresponsive", "uid", c.UID)
		// Unblocks the reader, whose exit releases the broker stream and PTY.
		_ = ws.CloseNow()
	})
	for {
		k, p, e := wire.Read(conn)
		if e != nil {
			break
		}
		switch k {
		case wire.Data:
			select {
			case <-credits:
			case <-ctx.Done():
				return
			case <-readerDone:
				return
			case <-time.After(10 * time.Second):
				_ = ws.Close(websocket.StatusPolicyViolation, "终端显示响应超时")
				return
			}
			if send(websocket.MessageBinary, p) != nil {
				return
			}
		case wire.Exit:
			var ended struct {
				Reason string `json:"reason"`
			}
			reason := "session_end"
			if json.Unmarshal(p, &ended) == nil && ended.Reason == "shell_exit" {
				reason = "shell_exit"
			}
			if notice, ok := expiryNotices[ended.Reason]; ok {
				_ = send(websocket.MessageBinary, []byte(notice))
			}
			payload, _ := json.Marshal(map[string]string{"type": "exit", "message": "会话已结束", "reason": reason})
			if send(websocket.MessageText, payload) != nil {
				return
			}
			_ = ws.Close(websocket.StatusNormalClosure, "会话已结束")
			return
		default:
			return
		}
	}
	_ = ws.Close(websocket.StatusGoingAway, "会话已结束或连接中断")
}
