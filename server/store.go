package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const retention = 7 * 24 * time.Hour

type Store struct {
	db *redis.Client
}

// Encode untrusted identifiers so separators cannot cause key collisions.
func key(parts ...string) string {
	result := "orgchat"
	for _, part := range parts {
		result += ":" + hex.EncodeToString([]byte(part))
	}

	return result
}

func sessionKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return key("session", hex.EncodeToString(sum[:]))
}

func (s *Store) account(ctx context.Context, id string) (Account, error) {
	var account Account

	raw, err := s.db.Get(ctx, key("user", id)).Bytes()
	if err != nil {
		return account, err
	}

	err = json.Unmarshal(raw, &account)
	return account, err
}

func (s *Store) saveAccount(ctx context.Context, account Account, token string) error {
	raw, err := json.Marshal(account)
	if err != nil {
		return err
	}

	_, err = s.db.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, key("user", account.User.ID), raw, 0)
		pipe.Set(ctx, sessionKey(token), account.User.ID, 24*time.Hour)

		for _, channel := range account.Channels {
			pipe.SAdd(ctx, key("channels"), channel.ID)
		}

		return nil
	})

	return err
}

func decodeMessage(channel string, entry redis.XMessage) Message {
	return Message{
		ID:        entry.ID,
		ChannelID: channel,
		AuthorID:  fmt.Sprint(entry.Values["authorId"]),
		Text:      fmt.Sprint(entry.Values["text"]),
		CreatedAt: fmt.Sprint(entry.Values["createdAt"]),
	}
}

func cutoffID() string {
	return strconv.FormatInt(time.Now().Add(-retention).UnixMilli(), 10) + "-0"
}

func (s *Store) history(ctx context.Context, channel, before string, limit int) ([]Message, *string, error) {
	stream := key("messages", channel)
	end := "+"

	if before != "" {
		found, err := s.db.XRangeN(ctx, stream, before, before, 1).Result()
		if err != nil {
			return nil, nil, err
		}
		if len(found) == 0 {
			return nil, nil, errCursor
		}

		end = "(" + before
	}

	entries, err := s.db.XRevRangeN(ctx, stream, end, cutoffID(), int64(limit+1)).Result()
	if err != nil {
		return nil, nil, err
	}

	var next *string
	if len(entries) > limit {
		entries = entries[:limit]
		id := entries[len(entries)-1].ID
		next = &id
	}

	messages := make([]Message, len(entries))
	for i, entry := range entries {
		messages[len(entries)-1-i] = decodeMessage(channel, entry)
	}

	return messages, next, nil
}

// Creation and idempotency are atomic, including across concurrent requests.
var sendScript = redis.NewScript(`
local previous = redis.call('GET', KEYS[2])
if previous then
    local message = cjson.decode(previous)
    if message.text ~= ARGV[2] then return {'conflict'} end
    return {'existing', previous}
end

-- One tiny global stream gives all channels globally unique, ordered IDs.
local id = redis.call('XADD', KEYS[3], 'MAXLEN', '1', '*', 'v', '1')
redis.call('XADD', KEYS[1], id, 'authorId', ARGV[1], 'text', ARGV[2], 'createdAt', ARGV[3])
local message = cjson.encode({id=id, channelId=ARGV[4], authorId=ARGV[1], text=ARGV[2], createdAt=ARGV[3]})
redis.call('SET', KEYS[2], message, 'EX', ARGV[5])
return {'created', message}
`)

func (s *Store) send(ctx context.Context, user, channel, text, clientID string) (Message, bool, error) {
	var message Message

	keys := []string{
		key("messages", channel),
		key("send", user, channel, clientID),
		key("message-clock"),
	}
	result, err := sendScript.Run(ctx, s.db, keys,
		user, text, time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), channel, int(retention.Seconds()),
	).StringSlice()
	if err != nil {
		return message, false, err
	}
	if result[0] == "conflict" {
		return message, false, errConflict
	}

	err = json.Unmarshal([]byte(result[1]), &message)
	return message, result[0] == "created", err
}

var readScript = redis.NewScript(`
if #redis.call('XRANGE', KEYS[1], ARGV[1], ARGV[1], 'COUNT', 1) == 0 then return '' end

local previous = redis.call('GET', KEYS[2])
local function parts(id)
    local a,b = string.match(id, '^(%d+)%-(%d+)$')
    return tonumber(a), tonumber(b)
end

if previous then
    local a,b = parts(previous)
    local c,d = parts(ARGV[1])
    if a > c or (a == c and b >= d) then return previous end
end

redis.call('SET', KEYS[2], ARGV[1])
return ARGV[1]
`)

func (s *Store) markRead(ctx context.Context, user, channel, id string) (string, error) {
	keys := []string{key("messages", channel), key("read", user, channel)}
	return readScript.Run(ctx, s.db, keys, id).Text()
}

func (s *Store) readState(ctx context.Context, user, channel string) (*string, bool, error) {
	position, err := s.db.Get(ctx, key("read", user, channel)).Result()
	if err != nil && err != redis.Nil {
		return nil, false, err
	}

	var last *string
	start := cutoffID()

	if position != "" {
		last = &position
		if streamMillis(position) >= streamMillis(start) {
			start = "(" + position
		}
	}

	for {
		entries, err := s.db.XRangeN(ctx, key("messages", channel), start, "+", 256).Result()
		if err != nil {
			return nil, false, err
		}

		for _, entry := range entries {
			if fmt.Sprint(entry.Values["authorId"]) != user {
				return last, true, nil
			}
		}
		if len(entries) < 256 {
			return last, false, nil
		}

		start = "(" + entries[len(entries)-1].ID
	}
}

func streamMillis(id string) int64 {
	millis, _, _ := strings.Cut(id, "-")
	value, _ := strconv.ParseInt(millis, 10, 64)
	return value
}

func (s *Store) cleanup(ctx context.Context) error {
	// SSCAN avoids blocking Redis while discovering channels.
	var cursor uint64
	cutoff := cutoffID()

	for {
		channels, next, err := s.db.SScan(ctx, key("channels"), cursor, "*", 100).Result()
		if err != nil {
			return err
		}

		for _, channel := range channels {
			if err := s.db.XTrimMinID(ctx, key("messages", channel), cutoff).Err(); err != nil {
				return err
			}
		}

		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}
