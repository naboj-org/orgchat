package main

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
)

func (s *Server) embedAuthEndpoint(c echo.Context) error {
	var body struct {
		Token string `json:"token"`
	}
	if err := c.Bind(&body); err != nil {
		return err
	}
	s.login(c.Response(), c.Request(), body.Token, false)
	return nil
}

// Mint only from the authenticated account, never from client-supplied grants.
// The new tab exchanges this short-lived credential through the existing login.
func validHandoff(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9') && !(char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

func (s *Server) popoutAuthEndpoint(c echo.Context) error {
	var body struct {
		Handoff string `json:"handoff"`
	}
	if err := c.Bind(&body); err != nil {
		return err
	}
	if !validHandoff(body.Handoff) {
		fail(c.Response(), http.StatusBadRequest, "INVALID_HANDOFF", "Provide a valid popout handoff.")
		return nil
	}
	account := requestAccount(c)
	now := time.Now()
	claims := AuthClaims{
		User:     account.User,
		Channels: account.Channels,
		Admin:    account.Admin,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   account.User.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(30 * time.Second)),
		},
	}
	if s.audience != "" {
		claims.Audience = jwt.ClaimStrings{s.audience}
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		s.internalError(c.Response(), err)
		return nil
	}
	if len(token) > 16384 {
		fail(c.Response(), http.StatusBadRequest, "TOKEN_TOO_LARGE", "Chat access is too large to open in a new tab.")
		return nil
	}

	created, err := s.store.db.SetNX(c.Request().Context(), key("popout", body.Handoff), token, 30*time.Second).Result()
	if err != nil {
		s.internalError(c.Response(), err)
		return nil
	}
	if !created {
		return c.NoContent(http.StatusConflict)
	}
	return c.JSON(http.StatusOK, map[string]bool{"ready": true})
}

// The random handoff is a short-lived bearer credential. Claiming it is atomic
// and requires no existing cookie or relationship with the opening window.
func (s *Server) claimPopoutAuth(c echo.Context) error {
	var body struct {
		Handoff string `json:"handoff"`
	}
	if err := c.Bind(&body); err != nil {
		return err
	}
	if !validHandoff(body.Handoff) {
		return c.NoContent(http.StatusBadRequest)
	}
	token, err := s.store.db.GetDel(c.Request().Context(), key("popout", body.Handoff)).Result()
	if err == redis.Nil {
		return c.NoContent(http.StatusAccepted)
	}
	if err != nil {
		s.internalError(c.Response(), err)
		return nil
	}
	s.login(c.Response(), c.Request(), token, false)
	return nil
}

const cookieCheckPrefix = "orgchat_probe_"

func (s *Server) startCookieCheck(c echo.Context) error {
	// A unique cookie per probe avoids false positives and races between tabs.
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		s.internalError(c.Response(), err)
		return nil
	}
	probe := base64.RawURLEncoding.EncodeToString(bytes)
	// Same attributes as the actual session, intentionally NOT partitioned.
	http.SetCookie(c.Response(), s.browserCookie(cookieCheckPrefix+probe, "1", 60))
	return c.JSON(http.StatusOK, map[string]string{"probe": probe})
}

func (s *Server) finishCookieCheck(c echo.Context) error {
	probe := c.QueryParam("probe")
	decoded, err := base64.RawURLEncoding.DecodeString(probe)
	if err != nil || len(decoded) != 16 || base64.RawURLEncoding.EncodeToString(decoded) != probe {
		fail(c.Response(), http.StatusBadRequest, "INVALID_PROBE", "Provide a valid cookie probe.")
		return nil
	}
	name := cookieCheckPrefix + probe
	cookie, err := c.Request().Cookie(name)
	available := err == nil && cookie.Value == "1"
	http.SetCookie(c.Response(), s.browserCookie(name, "", -1))
	return c.JSON(http.StatusOK, map[string]bool{"cookiesAvailable": available})
}
