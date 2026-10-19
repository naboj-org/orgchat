package main

import (
	"encoding/json"
	"net/http"
)

type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Affiliation string `json:"affiliation"`
}

type Channel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Account struct {
	User     User      `json:"user"`
	Channels []Channel `json:"channels"`
}

type Message struct {
	ID        string `json:"id"`
	ChannelID string `json:"channelId"`
	AuthorID  string `json:"authorId"`
	Text      string `json:"text"`
	CreatedAt string `json:"createdAt"`
}

type MessageResult struct {
	Message Message `json:"message"`
	Author  User    `json:"author"`
}

type ReadPosition struct {
	ChannelID         string `json:"channelId"`
	LastReadMessageID string `json:"lastReadMessageId"`
}

type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func fail(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func (a Account) canAccess(channel string) bool {
	for _, c := range a.Channels {
		if c.ID == channel {
			return true
		}
	}

	return false
}
