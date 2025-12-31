package engine

import (
	"context"
	"fmt"
	"time"
)

// ExternalHistoryOptions contains options for loading external history
type ExternalHistoryOptions struct {
	// InjectToRedis if true, saves messages to Redis Stream after loading
	InjectToRedis bool

	// Limit is the maximum number of messages to deliver
	Limit int64
}

// GetHistoryFromRedis gets message history from Redis Stream (within 24h retention).
// Returns messages sorted oldest to newest.
func (r *Relay) GetHistoryFromRedis(ctx context.Context, roomID string, limit int64) ([]*Message, error) {
	if limit <= 0 {
		limit = 100
	}

	entries, err := r.stream.XRange(ctx, r.streamKey(roomID), "-", "+", limit)
	if err != nil {
		return nil, err
	}

	messages := make([]*Message, 0, len(entries))
	for _, entry := range entries {
		msg := r.parseStreamEntry(roomID, entry)
		messages = append(messages, msg)
	}

	return messages, nil
}

// GetHistoryFromRedisReverse gets message history in reverse order (newest first).
func (r *Relay) GetHistoryFromRedisReverse(ctx context.Context, roomID string, limit int64) ([]*Message, error) {
	if limit <= 0 {
		limit = 100
	}

	entries, err := r.stream.XRevRange(ctx, r.streamKey(roomID), "+", "-", limit)
	if err != nil {
		return nil, err
	}

	messages := make([]*Message, 0, len(entries))
	for _, entry := range entries {
		msg := r.parseStreamEntry(roomID, entry)
		messages = append(messages, msg)
	}

	return messages, nil
}

// GetRedisHistoryLength returns the number of messages in Redis Stream for a room.
func (r *Relay) GetRedisHistoryLength(ctx context.Context, roomID string) (int64, error) {
	return r.stream.XLen(ctx, r.streamKey(roomID))
}

// IsRedisHistoryEmpty checks if Redis Stream has no messages for a room.
func (r *Relay) IsRedisHistoryEmpty(ctx context.Context, roomID string) (bool, error) {
	length, err := r.GetRedisHistoryLength(ctx, roomID)
	if err != nil {
		return true, err
	}
	return length == 0, nil
}

// LoadExternalHistory loads messages from external storage (SQLite) into StreamRelay.
// You provide the messages, StreamRelay will:
// 1. Optionally inject them into Redis Stream (for future recovery)
// 2. Deliver them to the subscriber
//
// Usage:
//
//	// Load messages from your SQLite
//	messages := myDB.GetMessages(roomID, limit)
//
//	// Pass to StreamRelay
//	relay.LoadExternalHistory(ctx, roomID, messages, subscriber, &ExternalHistoryOptions{
//	    InjectToRedis: true,  // Save to Redis for other users
//	})
func (r *Relay) LoadExternalHistory(
	ctx context.Context,
	roomID string,
	messages []*Message,
	sub Subscriber,
	opts *ExternalHistoryOptions,
) error {
	if opts == nil {
		opts = &ExternalHistoryOptions{}
	}

	// Apply limit if set
	if opts.Limit > 0 && int64(len(messages)) > opts.Limit {
		messages = messages[:opts.Limit]
	}

	// Inject to Redis Stream if requested
	if opts.InjectToRedis {
		if err := r.InjectHistoryToRedis(ctx, roomID, messages); err != nil {
			return fmt.Errorf("failed to inject to Redis: %w", err)
		}
	}

	// Deliver to subscriber
	for _, msg := range messages {
		msg.RoomID = roomID
		if err := sub.Receive(ctx, msg); err != nil {
			return fmt.Errorf("failed to deliver message: %w", err)
		}
	}

	return nil
}

// LoadHistoryWithFallback loads history from Redis first, falls back to external if empty.
// You provide the messages from SQLite, StreamRelay will:
// 1. Check if Redis has messages → return those
// 2. If Redis is empty → inject your SQLite messages and deliver
//
// Usage:
//
//	// Prepare fallback messages from SQLite (only loaded if Redis is empty)
//	sqliteMessages := myDB.GetRecentMessages(roomID, 100)
//
//	// Load with fallback
//	messages, err := relay.LoadHistoryWithFallback(ctx, roomID, sqliteMessages, subscriber)
func (r *Relay) LoadHistoryWithFallback(
	ctx context.Context,
	roomID string,
	fallbackMessages []*Message,
	sub Subscriber,
	limit int64,
) ([]*Message, error) {
	if limit <= 0 {
		limit = 100
	}

	// 1. Try Redis first
	redisMessages, err := r.GetHistoryFromRedis(ctx, roomID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get Redis history: %w", err)
	}

	// 2. If Redis has messages, deliver and return
	if len(redisMessages) > 0 {
		for _, msg := range redisMessages {
			if sub != nil {
				if err := sub.Receive(ctx, msg); err != nil {
					return redisMessages, fmt.Errorf("failed to deliver Redis message: %w", err)
				}
			}
		}
		return redisMessages, nil
	}

	// 3. Redis is empty → use fallback messages
	if len(fallbackMessages) == 0 {
		return []*Message{}, nil
	}

	// Apply limit
	if int64(len(fallbackMessages)) > limit {
		fallbackMessages = fallbackMessages[:limit]
	}

	// 4. Inject to Redis for future use
	if err := r.InjectHistoryToRedis(ctx, roomID, fallbackMessages); err != nil {
		// Log but don't fail - we can still deliver
		// Just won't be cached in Redis
	}

	// 5. Deliver to subscriber
	for _, msg := range fallbackMessages {
		msg.RoomID = roomID
		if sub != nil {
			if err := sub.Receive(ctx, msg); err != nil {
				return fallbackMessages, fmt.Errorf("failed to deliver fallback message: %w", err)
			}
		}
	}

	return fallbackMessages, nil
}

// InjectHistoryToRedis inserts messages into Redis Stream.
// Use this to "warm up" Redis with messages from SQLite.
func (r *Relay) InjectHistoryToRedis(ctx context.Context, roomID string, messages []*Message) error {
	for _, msg := range messages {
		msg.RoomID = roomID
		if msg.Timestamp.IsZero() {
			msg.Timestamp = time.Now()
		}
		_, err := r.publishToStream(ctx, roomID, msg)
		if err != nil {
			return fmt.Errorf("failed to inject message: %w", err)
		}
	}
	return nil
}

// ClearRedisHistory deletes all messages from Redis Stream for a room.
// Use with caution - messages will be lost unless saved in SQLite.
func (r *Relay) ClearRedisHistory(ctx context.Context, roomID string) error {
	// Delete the entire stream
	_, err := r.client.Do(ctx, "DEL", r.streamKey(roomID))
	return err
}

// TrimRedisHistory keeps only the last N messages in Redis Stream.
func (r *Relay) TrimRedisHistory(ctx context.Context, roomID string, maxLen int64) (int64, error) {
	return r.stream.XTrim(ctx, r.streamKey(roomID), maxLen, true)
}
