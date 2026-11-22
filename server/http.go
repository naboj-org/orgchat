package main

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/charmbracelet/log"
	"github.com/labstack/echo/v4"
)

func (s *Server) health(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), 2*time.Second)
	defer cancel()

	c.Response().Header().Set("Cache-Control", "no-store")

	if err := s.store.db.Ping(ctx).Err(); err != nil {
		return c.NoContent(http.StatusServiceUnavailable)
	}

	return c.NoContent(http.StatusOK)
}

func requireJSON(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		mediaType, _, err := mime.ParseMediaType(c.Request().Header.Get(echo.HeaderContentType))
		if err != nil || mediaType != echo.MIMEApplicationJSON {
			return echo.ErrUnsupportedMediaType
		}

		return next(c)
	}
}

func (s *Server) internalError(w http.ResponseWriter, err error) {
	log.Error("request failed", "error", err)
	fail(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error.")
}

func (s *Server) bootstrap(c echo.Context) error {
	w, r := c.Response(), c.Request()
	account := requestAccount(c)

	type channelState struct {
		Channel
		Unread            bool    `json:"unread"`
		LastReadMessageID *string `json:"lastReadMessageId"`
	}

	channels := make([]channelState, 0, len(account.Channels))
	for _, channel := range account.Channels {
		last, unread, err := s.store.readState(r.Context(), account.User.ID, channel.ID)
		if err != nil {
			s.internalError(w, err)
			return nil
		}

		channels = append(channels, channelState{Channel: channel, Unread: unread, LastReadMessageID: last})
	}

	writeJSON(w, http.StatusOK, map[string]any{"currentUser": account.User, "channels": channels, "admin": account.Admin})
	return nil
}

func (s *Server) history(c echo.Context) error {
	w, r := c.Response(), c.Request()
	channel := c.Param("channelId")

	limit := 50
	if r.URL.Query().Has("limit") {
		var err error
		limit, err = strconv.Atoi(r.URL.Query().Get("limit"))
		if err != nil || limit < 1 || limit > 100 {
			fail(w, http.StatusBadRequest, "INVALID_LIMIT", "Limit must be an integer from 1 to 100.")
			return nil
		}
	}

	before := r.URL.Query().Get("before")
	if r.URL.Query().Has("before") && !validStreamID(before) {
		fail(w, http.StatusBadRequest, "INVALID_CURSOR", "Cursor does not belong to this channel.")
		return nil
	}

	messages, next, err := s.store.history(r.Context(), channel, before, limit)
	if errors.Is(err, errCursor) {
		fail(w, http.StatusBadRequest, "INVALID_CURSOR", "Cursor does not belong to this channel.")
		return nil
	}
	if err != nil {
		s.internalError(w, err)
		return nil
	}

	users := make([]User, 0)
	seen := make(map[string]bool)
	for _, message := range messages {
		if seen[message.AuthorID] {
			continue
		}

		author, err := s.store.account(r.Context(), message.AuthorID)
		if err != nil {
			s.internalError(w, err)
			return nil
		}

		users = append(users, author.User)
		seen[message.AuthorID] = true
	}

	writeJSON(w, http.StatusOK, map[string]any{"messages": messages, "users": users, "nextBefore": next})
	return nil
}

// Match JavaScript's string.length used by the mock server (UTF-16 code units).
func textLength(text string) int {
	return len(utf16.Encode([]rune(text)))
}

func (s *Server) send(c echo.Context) error {
	w, r := c.Response(), c.Request()
	account := requestAccount(c)
	channel := c.Param("channelId")

	var body struct {
		Text            string `json:"text"`
		ClientMessageID string `json:"clientMessageId"`
	}
	if err := c.Bind(&body); err != nil {
		return err
	}

	text := strings.TrimSpace(body.Text)
	if text == "" || textLength(body.Text) > 10000 {
		fail(w, http.StatusBadRequest, "INVALID_MESSAGE", "Message must contain 1–10000 characters.")
		return nil
	}
	if strings.TrimSpace(body.ClientMessageID) == "" || textLength(body.ClientMessageID) > 128 {
		fail(w, http.StatusBadRequest, "INVALID_CLIENT_MESSAGE_ID", "Provide a clientMessageId of 1–128 characters.")
		return nil
	}

	message, created, err := s.store.send(r.Context(), account.User.ID, channel, text, body.ClientMessageID)
	if errors.Is(err, errConflict) {
		fail(w, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "This clientMessageId was used for different text.")
		return nil
	}
	if err != nil {
		s.internalError(w, err)
		return nil
	}

	result := MessageResult{Message: message, Author: account.User}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
		s.hub.message(channel, Event{Type: "message.created", Data: result})
	}

	writeJSON(w, status, result)
	return nil
}

func validStreamID(id string) bool {
	parts := strings.Split(id, "-")
	if len(parts) != 2 {
		return false
	}

	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, c := range part {
			if c < '0' || c > '9' {
				return false
			}
		}
		if _, err := strconv.ParseUint(part, 10, 64); err != nil {
			return false
		}
	}

	return true
}

func (s *Server) markRead(c echo.Context) error {
	w, r := c.Response(), c.Request()
	account := requestAccount(c)
	channel := c.Param("channelId")

	var body struct {
		LastReadMessageID string `json:"lastReadMessageId"`
	}
	if err := c.Bind(&body); err != nil {
		return err
	}
	if !validStreamID(body.LastReadMessageID) {
		fail(w, http.StatusBadRequest, "INVALID_MESSAGE_ID", "Message does not belong to this channel.")
		return nil
	}

	id, err := s.store.markRead(r.Context(), account.User.ID, channel, body.LastReadMessageID)
	if err != nil {
		s.internalError(w, err)
		return nil
	}
	if id == "" {
		fail(w, http.StatusBadRequest, "INVALID_MESSAGE_ID", "Message does not belong to this channel.")
		return nil
	}

	result := ReadPosition{ChannelID: channel, LastReadMessageID: id}
	s.hub.read(account.User.ID, Event{Type: "channel.read", Data: result})
	writeJSON(w, http.StatusOK, result)
	return nil
}
