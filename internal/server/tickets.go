package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"fn-terminal/internal/identity"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"
)

const websocketProtocol = "fnterminal.v1"
const ticketProtocolPrefix = "fnterminal-ticket."

type connectionTicket struct {
	claims identity.Claims
	origin string
	// opaque is set when the ticket request carried "Origin: null" (the fnOS
	// iOS app WebView); the WebSocket may then also arrive with "null".
	opaque  bool
	expires time.Time
}

type ticketStore struct {
	mu      sync.Mutex
	pending map[string]connectionTicket
}

func newTicketStore() *ticketStore { return &ticketStore{pending: make(map[string]connectionTicket)} }

func (s *ticketStore) issue(claims identity.Claims, origin string, opaque bool, now time.Time) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for token, ticket := range s.pending {
		if !now.Before(ticket.expires) {
			delete(s.pending, token)
			continue
		}
		if ticket.claims.UID == claims.UID {
			count++
		}
	}
	if count >= 8 || len(s.pending) >= 1024 {
		return "", errors.New("too many pending connection tickets")
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(secret[:])
	s.pending[token] = connectionTicket{claims: claims, origin: origin, opaque: opaque, expires: now.Add(time.Minute)}
	return token, nil
}

func (s *ticketStore) consume(token string, claims identity.Claims, origin string, now time.Time) bool {
	if len(token) != 43 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ticket, ok := s.pending[token]
	if !ok {
		return false
	}
	if !now.Before(ticket.expires) {
		delete(s.pending, token)
		return false
	}
	if ticket.claims != claims || (ticket.origin != origin && !(ticket.opaque && origin == opaqueOrigin)) {
		return false
	}
	delete(s.pending, token)
	return true
}

// No CORS is granted. The non-simple custom header prevents a foreign page
// from issuing a credentialed request; its browser preflight is rejected.
func issueConnectionTicket(w http.ResponseWriter, r *http.Request, claims identity.Claims, allowed []string, tickets *ticketStore) {
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	sites := r.Header.Values("Sec-Fetch-Site")
	reason := ""
	switch {
	case len(r.Header.Values("X-Fnterminal-Request")) != 1 || r.Header.Get("X-Fnterminal-Request") != "1":
		reason = "missing X-Fnterminal-Request header"
	case err != nil || mediaType != "application/json":
		reason = fmt.Sprintf("content-type %q", r.Header.Get("Content-Type"))
	case len(sites) > 1 || (len(sites) == 1 && sites[0] != "same-origin"):
		reason = fmt.Sprintf("sec-fetch-site %q", strings.Join(sites, ","))
	case !validOrigin(r, allowed):
		reason = fmt.Sprintf("origin %q not allowed (host %q, allowed %q)", strings.Join(r.Header.Values("Origin"), ","), r.Host, allowed)
	}
	if reason != "" {
		slog.Warn("terminal ticket rejected", "reason", reason)
		http.Error(w, "连接凭据请求被拒绝: "+reason, http.StatusForbidden)
		return
	}
	var input struct {
		Origin string `json:"origin"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	var extra any
	header := r.Header.Get("Origin")
	opaque := header == opaqueOrigin
	if d.Decode(&input) != nil || d.Decode(&extra) != io.EOF || !wellFormedOrigin(input.Origin) || (input.Origin != header && !opaque) {
		reason := fmt.Sprintf("body origin %q != header origin %q", input.Origin, r.Header.Get("Origin"))
		slog.Warn("terminal ticket rejected", "reason", reason)
		http.Error(w, "连接凭据请求无效: "+reason, http.StatusBadRequest)
		return
	}
	token, err := tickets.issue(claims, input.Origin, opaque, time.Now())
	if err != nil {
		http.Error(w, "连接请求过多，请稍后重试", http.StatusTooManyRequests)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(struct {
		Ticket string `json:"ticket"`
	}{token})
}

func requestTicket(r *http.Request) string {
	var protocols []string
	for _, line := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, value := range strings.Split(line, ",") {
			protocols = append(protocols, strings.TrimSpace(value))
		}
	}
	if len(protocols) != 2 {
		return ""
	}
	var token string
	fixed := false
	for _, protocol := range protocols {
		if protocol == websocketProtocol {
			fixed = true
		} else if strings.HasPrefix(protocol, ticketProtocolPrefix) {
			token = strings.TrimPrefix(protocol, ticketProtocolPrefix)
		} else {
			return ""
		}
	}
	if !fixed {
		return ""
	}
	return token
}
