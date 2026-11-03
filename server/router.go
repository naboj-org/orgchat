package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

const (
	accountContextKey = "orgchat.account"
	sessionContextKey = "orgchat.session"
)

func (s *Server) router() *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.HTTPErrorHandler = s.handleRouteError
	e.Pre(s.browserSecurity)

	e.GET("/healthz", s.health)
	e.HEAD("/healthz", s.health)

	static := echo.StaticDirectoryHandler(os.DirFS(env("STATIC_DIR", "../frontend/dist")), false)
	e.Match([]string{http.MethodGet, http.MethodHead}, "/*", static)

	api := e.Group("/api")
	api.GET("/auth", s.authEndpoint)
	api.POST("/auth/embed", s.embedAuthEndpoint, requireJSON, middleware.BodyLimit("32K"))
	api.POST("/cookie-check", s.startCookieCheck)
	api.GET("/cookie-check", s.finishCookieCheck)

	private := api.Group("", s.requireSession)
	private.POST("/auth/popout", s.popoutAuthEndpoint)
	private.GET("/bootstrap", s.bootstrap)
	private.GET("/ws", s.websocketEndpoint)

	channels := private.Group("/channels/:channelId", s.requireChannel)
	channels.GET("/messages", s.history)
	channels.POST("/messages", s.send, requireJSON, middleware.BodyLimit("32K"))
	channels.PUT("/read", s.markRead, requireJSON, middleware.BodyLimit("32K"))

	return e
}

func requestAccount(c echo.Context) Account {
	return c.Get(accountContextKey).(Account)
}

func (s *Server) requireSession(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		account, token, ok := s.authenticate(c.Response(), c.Request())
		if !ok {
			return nil
		}

		c.Set(accountContextKey, account)
		c.Set(sessionContextKey, token)

		return next(c)
	}
}

func (s *Server) requireChannel(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		channel := c.Param("channelId")
		if c.Request().URL.RawPath != "" {
			decoded, err := url.PathUnescape(channel)
			if err != nil {
				fail(c.Response(), http.StatusNotFound, "CHANNEL_NOT_FOUND", "Channel not found.")
				return nil
			}

			channel = decoded
			c.SetParamValues(channel)
		}

		if !requestAccount(c).canAccess(channel) {
			fail(c.Response(), http.StatusNotFound, "CHANNEL_NOT_FOUND", "Channel not found.")
			return nil
		}

		return next(c)
	}
}

func (s *Server) authEndpoint(c echo.Context) error {
	s.auth(c.Response(), c.Request())
	return nil
}

func (s *Server) websocketEndpoint(c echo.Context) error {
	token := c.Get(sessionContextKey).(string)
	s.websocket(c.Response(), c.Request(), requestAccount(c), token)
	return nil
}

func (s *Server) browserSecurity(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		r := c.Request()
		w := c.Response()

		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")

		// Public frontend files can be loaded from other sites, including the
		// widget loader and iframe. Origin restrictions apply only to the API.
		if r.URL.Path != "/api" && !strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-cache")
			return next(c)
		}

		w.Header().Set("Cache-Control", "no-store")

		// A signed login link may be followed from the identity provider's site.
		loginNavigation := r.Method == http.MethodGet && r.URL.Path == "/api/auth" &&
			r.Header.Get("Origin") == "" && r.Header.Get("Sec-Fetch-Mode") == "navigate"

		allowed := s.originAllowed(r)
		if !allowed && !loginNavigation {
			fail(w, http.StatusForbidden, "ORIGIN_FORBIDDEN", "Origin is not allowed.")
			return nil
		}

		if origin := r.Header.Get("Origin"); allowed && origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return nil
		}

		return next(c)
	}
}

func (s *Server) handleRouteError(err error, c echo.Context) {
	if c.Response().Committed {
		return
	}

	var routeError *echo.HTTPError
	if !errors.As(err, &routeError) {
		s.internalError(c.Response(), err)
		return
	}

	switch routeError.Code {
	case http.StatusBadRequest:
		var typeError *json.UnmarshalTypeError

		if errors.As(err, &typeError) {
			fail(c.Response(), http.StatusBadRequest, "INVALID_BODY", "Request fields have invalid types.")
		} else {
			fail(c.Response(), http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON.")
		}
	case http.StatusUnsupportedMediaType:
		fail(c.Response(), http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Use application/json.")
	case http.StatusRequestEntityTooLarge:
		fail(c.Response(), http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "Request body is too large.")
	case http.StatusNotFound:
		fail(c.Response(), http.StatusNotFound, "NOT_FOUND", "Endpoint not found.")
	case http.StatusMethodNotAllowed:
		fail(c.Response(), http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed.")
	default:
		s.internalError(c.Response(), err)
	}
}
