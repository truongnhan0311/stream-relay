package engine

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// MemoryRelay is a Redis-free relay that uses only in-memory storage.
// Perfect for single-server deployments with <10K users.
// Use with SQLite for permanent message storage.
type MemoryRelay struct {
	rooms        map[string]*MemoryRoom
	mu           sync.RWMutex
	eventHandler EventHandler
	ctx          context.Context
	cancel       context.CancelFunc
	running      bool
	startTime    time.Time

	// Message ID generator
	messageSeq atomic.Int64

	// Stats
	totalMessages atomic.Int64

	// Config
	defaultHistorySize int
	defaultHistoryTTL  time.Duration
}

// MemoryRoom is an in-memory room without Redis
type MemoryRoom struct {
	config      RoomConfig
	subscribers map[string]Subscriber
	presence    map[string]*PresenceInfo
	history     *RingBuffer // Circular buffer for message history
	mu          sync.RWMutex
	relay       *MemoryRelay
	lastActive  time.Time
}

// RingBuffer is a circular buffer for storing message history
type RingBuffer struct {
	messages []*Message
	size     int
	head     int
	count    int
	mu       sync.RWMutex
}

// NewRingBuffer creates a new ring buffer with the given size
func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = 100
	}
	return &RingBuffer{
		messages: make([]*Message, size),
		size:     size,
	}
}

// Add adds a message to the buffer
func (rb *RingBuffer) Add(msg *Message) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.messages[rb.head] = msg
	rb.head = (rb.head + 1) % rb.size
	if rb.count < rb.size {
		rb.count++
	}
}

// GetAll returns all messages in order (oldest to newest)
func (rb *RingBuffer) GetAll() []*Message {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	result := make([]*Message, 0, rb.count)
	start := (rb.head - rb.count + rb.size) % rb.size

	for i := 0; i < rb.count; i++ {
		idx := (start + i) % rb.size
		if rb.messages[idx] != nil {
			result = append(result, rb.messages[idx])
		}
	}
	return result
}

// GetSince returns messages after the given ID
func (rb *RingBuffer) GetSince(afterID string) []*Message {
	all := rb.GetAll()
	result := make([]*Message, 0)

	found := afterID == "" || afterID == "0" || afterID == "-"
	for _, msg := range all {
		if found {
			result = append(result, msg)
		} else if msg.ID == afterID {
			found = true
		}
	}
	return result
}

// GetLast returns the last N messages
func (rb *RingBuffer) GetLast(n int) []*Message {
	all := rb.GetAll()
	if len(all) <= n {
		return all
	}
	return all[len(all)-n:]
}

// Len returns the number of messages in the buffer
func (rb *RingBuffer) Len() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.count
}

// MemoryConfig contains configuration for MemoryRelay
type MemoryConfig struct {
	// DefaultHistorySize is messages to keep per room
	DefaultHistorySize int

	// DefaultHistoryTTL is unused in memory mode (no expiry)
	DefaultHistoryTTL time.Duration

	// EventHandler for system events
	EventHandler EventHandler
}

// DefaultMemoryConfig returns default configuration
func DefaultMemoryConfig() MemoryConfig {
	return MemoryConfig{
		DefaultHistorySize: 100,
		DefaultHistoryTTL:  24 * time.Hour,
	}
}

// NewMemoryRelay creates a new in-memory relay (no Redis required)
func NewMemoryRelay(cfg MemoryConfig) *MemoryRelay {
	ctx, cancel := context.WithCancel(context.Background())

	if cfg.DefaultHistorySize <= 0 {
		cfg.DefaultHistorySize = 100
	}

	return &MemoryRelay{
		rooms:              make(map[string]*MemoryRoom),
		eventHandler:       cfg.EventHandler,
		ctx:                ctx,
		cancel:             cancel,
		running:            true,
		startTime:          time.Now(),
		defaultHistorySize: cfg.DefaultHistorySize,
		defaultHistoryTTL:  cfg.DefaultHistoryTTL,
	}
}

// generateMessageID generates a unique message ID (similar to Redis Stream format)
func (r *MemoryRelay) generateMessageID() string {
	ts := time.Now().UnixMilli()
	seq := r.messageSeq.Add(1)
	return fmt.Sprintf("%d-%d", ts, seq)
}

// SetEventHandler sets the event handler
func (r *MemoryRelay) SetEventHandler(handler EventHandler) {
	r.eventHandler = handler
}

// CreateRoom creates a new room
func (r *MemoryRelay) CreateRoom(config RoomConfig) (*MemoryRoom, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.rooms[config.ID]; exists {
		return nil, fmt.Errorf("room %s already exists", config.ID)
	}

	historySize := int(config.HistorySize)
	if historySize <= 0 {
		historySize = r.defaultHistorySize
	}

	room := &MemoryRoom{
		config:      config,
		subscribers: make(map[string]Subscriber),
		presence:    make(map[string]*PresenceInfo),
		history:     NewRingBuffer(historySize),
		relay:       r,
		lastActive:  time.Now(),
	}
	r.rooms[config.ID] = room

	return room, nil
}

// GetRoom returns a room by ID
func (r *MemoryRelay) GetRoom(roomID string) (*MemoryRoom, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	room, exists := r.rooms[roomID]
	return room, exists
}

// GetOrCreateRoom gets or creates a room
func (r *MemoryRelay) GetOrCreateRoom(config RoomConfig) (*MemoryRoom, error) {
	if room, exists := r.GetRoom(config.ID); exists {
		return room, nil
	}
	return r.CreateRoom(config)
}

// DeleteRoom removes a room
func (r *MemoryRelay) DeleteRoom(roomID string) error {
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

// ListRooms returns all room IDs
func (r *MemoryRelay) ListRooms() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make([]string, 0, len(r.rooms))
	for id := range r.rooms {
		ids = append(ids, id)
	}
	return ids
}

// Subscribe adds a subscriber to a room
func (r *MemoryRelay) Subscribe(roomID string, sub Subscriber, presence *PresenceInfo) error {
	room, exists := r.GetRoom(roomID)
	if !exists {
		return fmt.Errorf("room %s not found", roomID)
	}
	return room.Subscribe(sub, presence)
}

// Unsubscribe removes a subscriber from a room
func (r *MemoryRelay) Unsubscribe(roomID, userID string) error {
	room, exists := r.GetRoom(roomID)
	if !exists {
		return fmt.Errorf("room %s not found", roomID)
	}
	return room.Unsubscribe(userID)
}

// Publish sends a message to a room
func (r *MemoryRelay) Publish(ctx context.Context, roomID string, msg *Message, opts *PublishOptions) ([]DeliveryResult, error) {
	room, exists := r.GetRoom(roomID)
	if !exists {
		return nil, fmt.Errorf("room %s not found", roomID)
	}

	if opts == nil {
		opts = DefaultPublishOptions()
	}

	// Set message properties
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	msg.RoomID = roomID
	msg.ID = r.generateMessageID()

	// Store in history
	if opts.Persist {
		room.history.Add(msg)
	}

	// Broadcast to subscribers
	results := room.Broadcast(ctx, msg, opts)

	// Emit event
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

// PublishDirect sends a DM
func (r *MemoryRelay) PublishDirect(ctx context.Context, senderID, recipientID string, msg *Message, opts *PublishOptions) ([]DeliveryResult, error) {
	roomID := GenerateDMRoomID(senderID, recipientID)

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

// RecoverMessages gets messages from history
func (r *MemoryRelay) RecoverMessages(ctx context.Context, roomID, userID string, opts *RecoveryOptions) ([]*Message, error) {
	room, exists := r.GetRoom(roomID)
	if !exists {
		return nil, fmt.Errorf("room %s not found", roomID)
	}

	if opts == nil {
		opts = &RecoveryOptions{Limit: 100}
	}

	var messages []*Message
	if opts.FromID != "" {
		messages = room.history.GetSince(opts.FromID)
	} else {
		messages = room.history.GetAll()
	}

	// Apply limit
	if opts.Limit > 0 && int64(len(messages)) > opts.Limit {
		messages = messages[len(messages)-int(opts.Limit):]
	}

	return messages, nil
}

// RecoverAndDeliver gets history and delivers to subscriber
func (r *MemoryRelay) RecoverAndDeliver(ctx context.Context, roomID string, sub Subscriber, opts *RecoveryOptions) error {
	messages, err := r.RecoverMessages(ctx, roomID, sub.ID(), opts)
	if err != nil {
		return err
	}

	for _, msg := range messages {
		if err := sub.Receive(ctx, msg); err != nil {
			return err
		}
	}
	return nil
}

// GetPresence returns presence for a room
func (r *MemoryRelay) GetPresence(roomID string) ([]PresenceInfo, error) {
	room, exists := r.GetRoom(roomID)
	if !exists {
		return nil, fmt.Errorf("room %s not found", roomID)
	}
	return room.GetPresence(), nil
}

// Stats returns relay statistics
func (r *MemoryRelay) Stats() Stats {
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

// Close shuts down the relay
func (r *MemoryRelay) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.running {
		return nil
	}

	r.running = false
	r.cancel()

	for _, room := range r.rooms {
		room.Close()
	}
	r.rooms = make(map[string]*MemoryRoom)

	return nil
}

// --- MemoryRoom methods ---

// ID returns the room ID
func (r *MemoryRoom) ID() string {
	return r.config.ID
}

// Type returns the room type
func (r *MemoryRoom) Type() RoomType {
	return r.config.Type
}

// Subscribe adds a subscriber
func (r *MemoryRoom) Subscribe(sub Subscriber, info *PresenceInfo) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	userID := sub.ID()

	// Close existing
	if existing, ok := r.subscribers[userID]; ok {
		existing.Close()
	}

	r.subscribers[userID] = sub
	r.lastActive = time.Now()

	if info == nil {
		info = &PresenceInfo{
			UserID:      userID,
			ConnectedAt: time.Now(),
		}
	}
	r.presence[userID] = info

	if r.relay != nil && r.relay.eventHandler != nil {
		r.relay.eventHandler(&Event{
			Type:      EventTypeSubscribe,
			RoomID:    r.config.ID,
			UserID:    userID,
			Timestamp: time.Now(),
		})
	}

	return nil
}

// Unsubscribe removes a subscriber
func (r *MemoryRoom) Unsubscribe(userID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if sub, exists := r.subscribers[userID]; exists {
		sub.Close()
		delete(r.subscribers, userID)
		delete(r.presence, userID)

		if r.relay != nil && r.relay.eventHandler != nil {
			r.relay.eventHandler(&Event{
				Type:      EventTypeUnsubscribe,
				RoomID:    r.config.ID,
				UserID:    userID,
				Timestamp: time.Now(),
			})
		}
	}

	return nil
}

// Broadcast sends a message to all subscribers
func (r *MemoryRoom) Broadcast(ctx context.Context, msg *Message, opts *PublishOptions) []DeliveryResult {
	r.mu.RLock()
	subscribers := make(map[string]Subscriber, len(r.subscribers))
	for k, v := range r.subscribers {
		subscribers[k] = v
	}
	members := make([]string, len(r.config.Members))
	copy(members, r.config.Members)
	r.mu.RUnlock()

	results := make([]DeliveryResult, 0, len(subscribers))
	var resultsMu sync.Mutex
	var wg sync.WaitGroup

	for userID, sub := range subscribers {
		if opts != nil && opts.SkipSender && userID == msg.SenderID {
			continue
		}

		wg.Add(1)
		go func(uid string, s Subscriber) {
			defer wg.Done()
			result := r.safeDeliver(ctx, uid, s, msg)
			resultsMu.Lock()
			results = append(results, result)
			resultsMu.Unlock()
		}(userID, sub)
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
	case <-time.After(10 * time.Second):
	}

	// Mark offline members
	for _, memberID := range members {
		if opts != nil && opts.SkipSender && memberID == msg.SenderID {
			continue
		}
		if _, online := subscribers[memberID]; !online {
			resultsMu.Lock()
			results = append(results, DeliveryResult{
				UserID: memberID,
				Status: DeliveryOffline,
			})
			resultsMu.Unlock()
		}
	}

	return results
}

func (r *MemoryRoom) safeDeliver(ctx context.Context, userID string, sub Subscriber, msg *Message) (result DeliveryResult) {
	result = DeliveryResult{UserID: userID}

	defer func() {
		if recovered := recover(); recovered != nil {
			result.Status = DeliveryFailed
			result.Error = &DeliveryPanicError{
				UserID:    userID,
				Recovered: recovered,
			}
			go r.Unsubscribe(userID)
		}
	}()

	// Retry up to 3 times
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if err := sub.Receive(ctx, msg); err != nil {
			lastErr = err
			if attempt < 2 {
				time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
			}
			continue
		}
		result.Status = DeliverySuccess
		return result
	}

	result.Status = DeliveryFailed
	result.Error = lastErr
	return result
}

// GetPresence returns presence info
func (r *MemoryRoom) GetPresence() []PresenceInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]PresenceInfo, 0, len(r.presence))
	for _, info := range r.presence {
		result = append(result, *info)
	}
	return result
}

// SubscriberCount returns subscriber count
func (r *MemoryRoom) SubscriberCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.subscribers)
}

// IsSubscribed checks if user is subscribed
func (r *MemoryRoom) IsSubscribed(userID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.subscribers[userID]
	return exists
}

// GetHistory returns message history
func (r *MemoryRoom) GetHistory(limit int) []*Message {
	if limit <= 0 {
		return r.history.GetAll()
	}
	return r.history.GetLast(limit)
}

// Close closes the room
func (r *MemoryRoom) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, sub := range r.subscribers {
		sub.Close()
	}
	r.subscribers = make(map[string]Subscriber)
	r.presence = make(map[string]*PresenceInfo)

	return nil
}

// --- Helper for combining with external storage ---

// LoadExternalMessages loads messages from external storage (e.g., SQLite) into memory
func (r *MemoryRoom) LoadExternalMessages(messages []*Message) {
	for _, msg := range messages {
		r.history.Add(msg)
	}
}

// GetHistoryWithFallback gets history from memory, with callback for external fallback
func (r *MemoryRelay) GetHistoryWithFallback(
	ctx context.Context,
	roomID string,
	limit int,
	fallbackLoader func(roomID string, limit int) ([]*Message, error),
) ([]*Message, error) {
	room, exists := r.GetRoom(roomID)
	if !exists {
		// Room doesn't exist, try to load from external
		if fallbackLoader != nil {
			return fallbackLoader(roomID, limit)
		}
		return nil, fmt.Errorf("room %s not found", roomID)
	}

	// Get from memory
	messages := room.GetHistory(limit)

	// If not enough messages and fallback available
	if len(messages) < limit && fallbackLoader != nil {
		externalMsgs, err := fallbackLoader(roomID, limit-len(messages))
		if err == nil && len(externalMsgs) > 0 {
			// Prepend external (older) messages
			messages = append(externalMsgs, messages...)
		}
	}

	return messages, nil
}

// ParseMessageID extracts timestamp from message ID
func ParseMessageID(id string) (time.Time, int64, error) {
	var ts, seq int64
	_, err := fmt.Sscanf(id, "%d-%d", &ts, &seq)
	if err != nil {
		return time.Time{}, 0, err
	}
	return time.UnixMilli(ts), seq, nil
}

// CompareMessageIDs compares two message IDs
// Returns -1 if a < b, 0 if a == b, 1 if a > b
func CompareMessageIDs(a, b string) int {
	tsA, seqA, errA := ParseMessageID(a)
	tsB, seqB, errB := ParseMessageID(b)

	if errA != nil || errB != nil {
		// Fallback to string comparison
		if a < b {
			return -1
		} else if a > b {
			return 1
		}
		return 0
	}

	if tsA.Before(tsB) {
		return -1
	} else if tsA.After(tsB) {
		return 1
	}

	// Same timestamp, compare sequence
	if seqA < seqB {
		return -1
	} else if seqA > seqB {
		return 1
	}
	return 0
}

// GenerateMessageIDFromTime creates a message ID from a specific time
func GenerateMessageIDFromTime(t time.Time, seq int64) string {
	return strconv.FormatInt(t.UnixMilli(), 10) + "-" + strconv.FormatInt(seq, 10)
}
