package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pico/streamrelay/redis"
)

// Relay is the central message broker
type Relay struct {
	client       *redis.Client
	stream       *redis.Stream
	rooms        map[string]*Room
	mu           sync.RWMutex
	eventHandler EventHandler
	ctx          context.Context
	cancel       context.CancelFunc
	running      bool
	startTime    time.Time
	prefix       string

	// Stats
	totalMessages atomic.Int64
}

// Config contains configuration for the Relay
type Config struct {
	// Redis configuration
	Redis redis.Config

	// Prefix for all Redis keys
	Prefix string

	// DefaultHistorySize is the default number of messages to keep per room
	DefaultHistorySize int64

	// DefaultHistoryTTL is the default message retention time
	DefaultHistoryTTL time.Duration

	// EventHandler is called for system events (optional)
	EventHandler EventHandler
}

// DefaultConfig returns a default Relay configuration
func DefaultConfig() Config {
	return Config{
		Redis:              redis.DefaultConfig(),
		Prefix:             "sr:",
		DefaultHistorySize: 1000,
		DefaultHistoryTTL:  24 * time.Hour,
	}
}

// New creates a new Relay instance
func New(cfg Config) (*Relay, error) {
	client, err := redis.NewClient(cfg.Redis)
	if err != nil {
		return nil, fmt.Errorf("failed to create redis client: %w", err)
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to ping redis: %w", err)
	}

	relayCtx, relayCancel := context.WithCancel(context.Background())

	prefix := cfg.Prefix
	if prefix == "" {
		prefix = "sr:"
	}

	return &Relay{
		client:       client,
		stream:       redis.NewStream(client),
		rooms:        make(map[string]*Room),
		eventHandler: cfg.EventHandler,
		ctx:          relayCtx,
		cancel:       relayCancel,
		running:      true,
		startTime:    time.Now(),
		prefix:       prefix,
	}, nil
}

// streamKey generates the Redis key for a room's stream
func (r *Relay) streamKey(roomID string) string {
	return r.prefix + "stream:" + roomID
}

// offsetKey generates the Redis key for a user's offset in a room
func (r *Relay) offsetKey(roomID, userID string) string {
	return r.prefix + "offset:" + roomID + ":" + userID
}

// SetEventHandler sets the event handler for system events
func (r *Relay) SetEventHandler(handler EventHandler) {
	r.eventHandler = handler
}

// CreateRoom creates a new room with the given configuration
func (r *Relay) CreateRoom(config RoomConfig) (*Room, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.rooms[config.ID]; exists {
		return nil, fmt.Errorf("room %s already exists", config.ID)
	}

	room := newRoom(r, config)
	r.rooms[config.ID] = room

	// Start listening to the Redis stream
	room.Start(r.ctx)

	return room, nil
}

// GetRoom returns a room by ID
func (r *Relay) GetRoom(roomID string) (*Room, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	room, exists := r.rooms[roomID]
	return room, exists
}

// GetOrCreateRoom gets an existing room or creates a new one
func (r *Relay) GetOrCreateRoom(config RoomConfig) (*Room, error) {
	if room, exists := r.GetRoom(config.ID); exists {
		return room, nil
	}
	return r.CreateRoom(config)
}

// DeleteRoom removes a room
func (r *Relay) DeleteRoom(roomID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	room, exists := r.rooms[roomID]
	if !exists {
		return fmt.Errorf("room %s not found", roomID)
	}

	room.Close()
	delete(r.rooms, roomID)

	return nil
}

// ListRooms returns all active room IDs
func (r *Relay) ListRooms() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make([]string, 0, len(r.rooms))
	for id := range r.rooms {
		ids = append(ids, id)
	}
	return ids
}

// Subscribe adds a subscriber to a room
func (r *Relay) Subscribe(roomID string, sub Subscriber, presence *PresenceInfo) error {
	room, exists := r.GetRoom(roomID)
	if !exists {
		return fmt.Errorf("room %s not found", roomID)
	}
	return room.Subscribe(sub, presence)
}

// Unsubscribe removes a subscriber from a room
func (r *Relay) Unsubscribe(roomID, userID string) error {
	room, exists := r.GetRoom(roomID)
	if !exists {
		return fmt.Errorf("room %s not found", roomID)
	}
	return room.Unsubscribe(userID)
}

// Publish sends a message to a room
func (r *Relay) Publish(ctx context.Context, roomID string, msg *Message, opts *PublishOptions) ([]DeliveryResult, error) {
	room, exists := r.GetRoom(roomID)
	if !exists {
		return nil, fmt.Errorf("room %s not found", roomID)
	}

	if opts == nil {
		opts = DefaultPublishOptions()
	}

	// Set timestamp if not set
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	msg.RoomID = roomID

	// Persist to Redis Stream if requested
	if opts.Persist {
		messageID, err := r.publishToStream(ctx, roomID, msg)
		if err != nil {
			return nil, fmt.Errorf("failed to persist message: %w", err)
		}
		msg.ID = messageID
	}

	// Broadcast to online subscribers
	results := room.Broadcast(ctx, msg, opts)

	// Emit publish event
	if r.eventHandler != nil {
		r.eventHandler(&Event{
			Type:      EventTypePublish,
			RoomID:    roomID,
			UserID:    msg.SenderID,
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"message_id":     msg.ID,
				"delivery_count": len(results),
			},
		})
	}

	r.totalMessages.Add(1)

	return results, nil
}

// publishToStream writes a message to Redis Stream
func (r *Relay) publishToStream(ctx context.Context, roomID string, msg *Message) (string, error) {
	// Serialize content to JSON string
	contentStr := string(msg.Content)

	// Serialize metadata
	metadataStr := ""
	if len(msg.Metadata) > 0 {
		metaBytes, _ := json.Marshal(msg.Metadata)
		metadataStr = string(metaBytes)
	}

	fields := map[string]string{
		"sender_id": msg.SenderID,
		"type":      string(msg.Type),
		"content":   contentStr,
		"metadata":  metadataStr,
		"timestamp": strconv.FormatInt(msg.Timestamp.UnixMilli(), 10),
	}

	return r.stream.XAdd(ctx, r.streamKey(roomID), "*", fields)
}

// readFromStream reads messages from a room's Redis Stream
func (r *Relay) readFromStream(ctx context.Context, roomID string, lastID string, count int64, block time.Duration) ([]*Message, error) {
	streams := map[string]string{
		r.streamKey(roomID): lastID,
	}

	results, err := r.stream.XRead(ctx, count, block, streams)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}

	messages := make([]*Message, 0)
	for _, result := range results {
		for _, entry := range result.Entries {
			msg := r.parseStreamEntry(roomID, entry)
			messages = append(messages, msg)
		}
	}

	return messages, nil
}

// parseStreamEntry converts a Redis Stream entry to a Message
func (r *Relay) parseStreamEntry(roomID string, entry redis.StreamEntry) *Message {
	msg := &Message{
		ID:     entry.ID,
		RoomID: roomID,
	}

	if v, ok := entry.Fields["sender_id"]; ok {
		msg.SenderID = v
	}
	if v, ok := entry.Fields["type"]; ok {
		msg.Type = MessageType(v)
	}
	if v, ok := entry.Fields["content"]; ok {
		msg.Content = []byte(v)
	}
	if v, ok := entry.Fields["metadata"]; ok && v != "" {
		json.Unmarshal([]byte(v), &msg.Metadata)
	}
	if v, ok := entry.Fields["timestamp"]; ok {
		if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
			msg.Timestamp = time.UnixMilli(ts)
		}
	}

	return msg
}

// PublishDirect sends a direct message between two users
func (r *Relay) PublishDirect(ctx context.Context, senderID, recipientID string, msg *Message, opts *PublishOptions) ([]DeliveryResult, error) {
	// Generate consistent DM room ID
	roomID := GenerateDMRoomID(senderID, recipientID)

	// Get or create the DM room
	_, err := r.GetOrCreateRoom(RoomConfig{
		ID:      roomID,
		Type:    RoomTypeDirect,
		Members: []string{senderID, recipientID},
	})
	if err != nil {
		return nil, err
	}

	msg.RoomID = roomID
	msg.SenderID = senderID

	return r.Publish(ctx, roomID, msg, opts)
}

// GenerateDMRoomID creates a consistent room ID for DMs
func GenerateDMRoomID(userA, userB string) string {
	if userA < userB {
		return "dm:" + userA + ":" + userB
	}
	return "dm:" + userB + ":" + userA
}

// RecoverMessages retrieves missed messages for a user in a room
func (r *Relay) RecoverMessages(ctx context.Context, roomID, userID string, opts *RecoveryOptions) ([]*Message, error) {
	if opts == nil {
		opts = &RecoveryOptions{Limit: 100}
	}

	lastID := opts.FromID
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}

	// If no lastID provided, try to get from stored offset
	if lastID == "" {
		storedID, err := r.GetOffset(ctx, roomID, userID)
		if err == nil && storedID != "" {
			lastID = storedID
		}
	}

	// Use exclusive range: (lastID to get messages after lastID
	startID := lastID
	if startID != "" && startID != "-" && startID != "0" {
		startID = "(" + lastID // Exclusive
	} else {
		startID = "-" // From beginning
	}

	entries, err := r.stream.XRange(ctx, r.streamKey(roomID), startID, "+", limit)
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

// RecoverAndDeliver retrieves missed messages and delivers them to a subscriber
func (r *Relay) RecoverAndDeliver(ctx context.Context, roomID string, sub Subscriber, opts *RecoveryOptions) error {
	messages, err := r.RecoverMessages(ctx, roomID, sub.ID(), opts)
	if err != nil {
		return err
	}

	var lastID string
	for _, msg := range messages {
		if err := sub.Receive(ctx, msg); err != nil {
			return fmt.Errorf("failed to deliver recovered message: %w", err)
		}
		lastID = msg.ID
	}

	// Update offset after successful delivery
	if lastID != "" {
		r.SaveOffset(ctx, roomID, sub.ID(), lastID)
	}

	return nil
}

// SaveOffset saves a user's last read message position
func (r *Relay) SaveOffset(ctx context.Context, roomID, userID, messageID string) error {
	_, err := r.client.Do(ctx, "SET", r.offsetKey(roomID, userID), messageID)
	return err
}

// GetOffset gets a user's last read message position
func (r *Relay) GetOffset(ctx context.Context, roomID, userID string) (string, error) {
	resp, err := r.client.Do(ctx, "GET", r.offsetKey(roomID, userID))
	if err != nil {
		return "", err
	}
	if resp.IsNull {
		return "", nil
	}
	return resp.AsString()
}

// GetPresence returns presence information for a room
func (r *Relay) GetPresence(roomID string) ([]PresenceInfo, error) {
	room, exists := r.GetRoom(roomID)
	if !exists {
		return nil, fmt.Errorf("room %s not found", roomID)
	}
	return room.GetPresence(), nil
}

// Stats returns current relay statistics
func (r *Relay) Stats() Stats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var totalSubs int64
	for _, room := range r.rooms {
		totalSubs += int64(room.SubscriberCount())
	}

	return Stats{
		ActiveRooms:       int64(len(r.rooms)),
		ActiveSubscribers: totalSubs,
		TotalMessages:     r.totalMessages.Load(),
		Uptime:            int64(time.Since(r.startTime).Seconds()),
	}
}

// Client returns the underlying Redis client for advanced operations
func (r *Relay) Client() *redis.Client {
	return r.client
}

// Stream returns the Redis Stream wrapper
func (r *Relay) Stream() *redis.Stream {
	return r.stream
}

// Close shuts down the relay gracefully
func (r *Relay) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.running {
		return nil
	}

	r.running = false
	r.cancel()

	// Close all rooms
	for _, room := range r.rooms {
		room.Close()
	}
	r.rooms = make(map[string]*Room)

	// Close Redis connection
	return r.client.Close()
}
