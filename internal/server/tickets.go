package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fn-terminal/internal/identity"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"
)

const websocketProtocol = "fnterminal.v1"
const ticketProtocolPrefix = "fnterminal-ticket."

type connectionTicket struct {
	claims  identity.Claims
	origin  string
	expires time.Time
}

type ticketStore struct {
	mu      sync.Mutex
	pending map[string]connectionTicket
}

func newTicketStore() *ticketStore { return &ticketStore{pending: make(map[string]connectionTicket)} }

func (s *ticketStore) issue(claims identity.Claims, origin string, now time.Time) (string, error) {
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
	s.pending[token] = connectionTicket{claims: claims, origin: origin, expires: now.Add(time.Minute)}
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
	if ticket.claims != claims || ticket.origin != origin {
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
	if len(r.Header.Values("X-Fnterminal-Request")) != 1 || r.Header.Get("X-Fnterminal-Request") != "1" || err != nil || mediaType != "application/json" || len(sites) > 1 || (len(sites) == 1 && sites[0] != "same-origin") || !validOrigin(r, allowed) {
		http.Error(w, "连接凭据请求被拒绝", http.StatusForbidden)
		return
	}
	var input struct {
		Origin string `json:"origin"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(&input) != nil || d.Decode(&extra) != io.EOF || input.Origin != r.Header.Get("Origin") {
		http.Error(w, "连接凭据请求无效", http.StatusBadRequest)
		return
	}
	token, err := tickets.issue(claims, input.Origin, time.Now())
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
