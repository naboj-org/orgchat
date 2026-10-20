package main

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"

	"github.com/labstack/echo/v4"
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
