package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

const cookieName = "orgchat_session"

type AuthClaims struct {
	User     User      `json:"user"`
	Channels []Channel `json:"channels"`
	jwt.RegisteredClaims
}

func validIdentifier(value string) bool {
	return strings.TrimSpace(value) != "" && len(value) <= 256 && utf8.ValidString(value)
}

func (s *Server) auth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")

	raw := r.URL.Query().Get("token")
	if raw == "" || len(raw) > 16384 {
		fail(w, http.StatusBadRequest, "INVALID_TOKEN", "Provide a signed JWT in the token query parameter.")
		return
	}

	s.login(w, r, raw, true)
}

// Both entry points share token validation, grants, and session creation.
func (s *Server) login(w http.ResponseWriter, r *http.Request, raw string, redirect bool) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	if raw == "" || len(raw) > 16384 {
		fail(w, http.StatusBadRequest, "INVALID_TOKEN", "Provide a signed JWT of at most 16 KiB.")
		return
	}

	options := []jwt.ParserOption{
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	}
	if s.issuer != "" {
		options = append(options, jwt.WithIssuer(s.issuer))
	}
	if s.audience != "" {
		options = append(options, jwt.WithAudience(s.audience))
	}

	var claims AuthClaims
	_, err := jwt.ParseWithClaims(raw, &claims, func(token *jwt.Token) (any, error) {
		return s.secret, nil
	}, options...)
	if err != nil {
		fail(w, http.StatusUnauthorized, "INVALID_TOKEN", "JWT is invalid or expired.")
		return
	}

	if !validIdentifier(claims.User.ID) || strings.TrimSpace(claims.User.Username) == "" ||
		len(claims.User.Username) > 256 || len(claims.User.Affiliation) > 256 ||
		(claims.Subject != "" && claims.Subject != claims.User.ID) ||
		claims.Channels == nil || len(claims.Channels) > 256 {
		fail(w, http.StatusBadRequest, "INVALID_CLAIMS", "Provide a user and a channel list; sub, if supplied, must match user.id.")
		return
	}

	seen := make(map[string]bool)
	for _, channel := range claims.Channels {
		if !validIdentifier(channel.ID) || strings.TrimSpace(channel.Name) == "" ||
			len(channel.Name) > 256 || strings.Contains(channel.ID, "/") || seen[channel.ID] {
			fail(w, http.StatusBadRequest, "INVALID_CLAIMS", "Channels need unique IDs without slashes and nonempty names of at most 256 bytes.")
			return
		}
		seen[channel.ID] = true
	}

	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		s.internalError(w, err)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(bytes)
	account := Account{User: claims.User, Channels: claims.Channels}

	if err := s.store.saveAccount(r.Context(), account, token); err != nil {
		s.internalError(w, err)
		return
	}

	// Keep existing sessions and unchanged sockets alive. Changed profiles or
	// grants require a reconnect so every socket uses the latest account.
	s.hub.closeStaleUser(account)

	http.SetCookie(w, s.browserCookie(cookieName, token, 24*60*60))
	if redirect {
		http.Redirect(w, r, "/", http.StatusSeeOther)
	} else {
		writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
	}
}

// Secure sessions support cross-site iframes where third-party cookies are
// permitted. Local HTTP development uses Lax; production embeds require HTTPS.
func (s *Server) browserCookie(name, value string, maxAge int) *http.Cookie {
	sameSite := http.SameSiteLaxMode
	if s.secureCookie {
		sameSite = http.SameSiteNoneMode
	}
	return &http.Cookie{
		Name: name,
		Value: value,
		Path: "/",
		HttpOnly: true,
		Secure: s.secureCookie,
		SameSite: sameSite,
		MaxAge: maxAge,
		Expires: time.Now().Add(time.Duration(maxAge) * time.Second),
	}
}

func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (Account, string, bool) {
	var empty Account

	cookie, err := r.Cookie(cookieName)
	if err != nil || len(cookie.Value) != 43 {
		fail(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in first.")
		return empty, "", false
	}

	userID, err := s.store.db.Get(r.Context(), sessionKey(cookie.Value)).Result()
	if errors.Is(err, redis.Nil) {
		fail(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Session expired. Sign in again.")
		return empty, "", false
	}
	if err != nil {
		s.internalError(w, err)
		return empty, "", false
	}

	account, err := s.store.account(r.Context(), userID)
	if errors.Is(err, redis.Nil) {
		fail(w, http.StatusUnauthorized, "UNAUTHENTICATED", "User no longer exists.")
		return empty, "", false
	}
	if err != nil {
		s.internalError(w, err)
		return empty, "", false
	}

	return account, cookie.Value, true
}

func (s *Server) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		if r.Header.Get("Sec-Fetch-Site") != "cross-site" {
			return true
		}

		// Cross-site ancestors can taint Fetch Metadata inside an iframe.
		// Allow read-only HTTP requests without Origin; browsers still prevent
		// other sites from reading the response through the same-origin policy.
		// Auth navigation is handled separately, and WS must pass its check.
		readOnly := r.Method == http.MethodGet || r.Method == http.MethodHead
		return readOnly && r.URL.Path != "/api/auth" && r.URL.Path != "/api/ws"
	}
	if s.origins[origin] {
		return true
	}

	u, err := url.Parse(origin)
	if err != nil || u.Host != r.Host {
		return false
	}

	scheme := "http"
	if s.secureCookie || r.TLS != nil {
		scheme = "https"
	}

	return u.Scheme == scheme && u.User == nil && u.RawQuery == "" && u.Path == "" && u.Fragment == ""
}
