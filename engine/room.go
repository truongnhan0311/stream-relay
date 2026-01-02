package engine

import (
	"context"
	"sync"
	"time"
)

// Room represents a chat room with its subscribers
type Room struct {
	config      RoomConfig
	subscribers map[string]Subscriber // Key: UserID
	presence    map[string]*PresenceInfo
	mu          sync.RWMutex
	relay       *Relay
	stopChan    chan struct{}
	running     bool
	lastActive  time.Time
}

// newRoom creates a new room
func newRoom(relay *Relay, config RoomConfig) *Room {
	return &Room{
		config:      config,
		subscribers: make(map[string]Subscriber),
		presence:    make(map[string]*PresenceInfo),
		relay:       relay,
		stopChan:    make(chan struct{}),
		running:     false,
		lastActive:  time.Now(),
	}
}

// ID returns the room identifier
func (r *Room) ID() string {
	return r.config.ID
}

// Type returns the room type
func (r *Room) Type() RoomType {
	return r.config.Type
}

// Config returns the room configuration
func (r *Room) Config() RoomConfig {
	return r.config
}

// Subscribe adds a subscriber to this room
func (r *Room) Subscribe(sub Subscriber, info *PresenceInfo) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	userID := sub.ID()

	// Close existing connection if any (handle reconnection)
	if existing, ok := r.subscribers[userID]; ok {
		existing.Close()
	}

	r.subscribers[userID] = sub
	r.lastActive = time.Now()

	// Set presence info
	if info == nil {
		info = &PresenceInfo{
			UserID:      userID,
			ConnectedAt: time.Now(),
		}
	}
	r.presence[userID] = info

	// Emit subscribe event
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

// Unsubscribe removes a subscriber from this room
func (r *Room) Unsubscribe(userID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if sub, exists := r.subscribers[userID]; exists {
		sub.Close()
		delete(r.subscribers, userID)
		delete(r.presence, userID)
		r.lastActive = time.Now()

		// Emit unsubscribe event
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

// GetSubscriber returns a subscriber by user ID
func (r *Room) GetSubscriber(userID string) (Subscriber, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sub, exists := r.subscribers[userID]
	return sub, exists
}

// GetSubscribers returns all active subscribers
func (r *Room) GetSubscribers() []Subscriber {
	r.mu.RLock()
	defer r.mu.RUnlock()

	subs := make([]Subscriber, 0, len(r.subscribers))
	for _, sub := range r.subscribers {
		subs = append(subs, sub)
	}
	return subs
}

// GetSubscriberIDs returns all subscriber user IDs
func (r *Room) GetSubscriberIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make([]string, 0, len(r.subscribers))
	for id := range r.subscribers {
		ids = append(ids, id)
	}
	return ids
}

// SubscriberCount returns the number of active subscribers
func (r *Room) SubscriberCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.subscribers)
}

// IsSubscribed checks if a user is subscribed
func (r *Room) IsSubscribed(userID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.subscribers[userID]
	return exists
}

// IsMember checks if a user is a member of this room
func (r *Room) IsMember(userID string) bool {
	for _, member := range r.config.Members {
		if member == userID {
			return true
		}
	}
	return false
}

// AddMember adds a member to the room
func (r *Room) AddMember(userID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.config.Members {
		if m == userID {
			return // Already a member
		}
	}
	r.config.Members = append(r.config.Members, userID)
}

// RemoveMember removes a member from the room
func (r *Room) RemoveMember(userID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	members := make([]string, 0, len(r.config.Members))
	for _, m := range r.config.Members {
		if m != userID {
			members = append(members, m)
		}
	}
	r.config.Members = members

	// Also unsubscribe if subscribed
	if sub, exists := r.subscribers[userID]; exists {
		sub.Close()
		delete(r.subscribers, userID)
		delete(r.presence, userID)
	}
}

// GetPresence returns presence information for all online users
func (r *Room) GetPresence() []PresenceInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]PresenceInfo, 0, len(r.presence))
	for _, info := range r.presence {
		result = append(result, *info)
	}
	return result
}

// Broadcast sends a message to all active subscribers with panic recovery
func (r *Room) Broadcast(ctx context.Context, msg *Message, opts *PublishOptions) []DeliveryResult {
	r.mu.RLock()
	subscribers := make(map[string]Subscriber, len(r.subscribers))
	for k, v := range r.subscribers {
		subscribers[k] = v
	}
	members := make([]string, len(r.config.Members))
	copy(members, r.config.Members)
	r.mu.RUnlock()

	results := make([]DeliveryResult, 0, len(members))
	var resultsMu sync.Mutex

	// Use WaitGroup to ensure all deliveries complete
	var wg sync.WaitGroup

	// Deliver to online subscribers (fan-out with panic recovery)
	for userID, sub := range subscribers {
		// Skip sender if requested
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

	// Wait for all deliveries to complete (or timeout)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All deliveries completed
	case <-ctx.Done():
		// Context canceled, some deliveries may be incomplete
	case <-time.After(10 * time.Second):
		// Timeout safety net
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

// safeDeliver delivers a message to a single subscriber with panic recovery
func (r *Room) safeDeliver(ctx context.Context, userID string, sub Subscriber, msg *Message) (result DeliveryResult) {
	result = DeliveryResult{UserID: userID}

	// Recover from any panic in the subscriber's Receive method
	defer func() {
		if recovered := recover(); recovered != nil {
			result.Status = DeliveryFailed
			result.Error = &DeliveryPanicError{
				UserID:    userID,
				Recovered: recovered,
			}

			// Emit error event
			if r.relay != nil && r.relay.eventHandler != nil {
				r.relay.eventHandler(&Event{
					Type:      EventTypeError,
					RoomID:    r.config.ID,
					UserID:    userID,
					Timestamp: time.Now(),
					Data: map[string]interface{}{
						"error":      "delivery_panic",
						"recovered":  recovered,
						"message_id": msg.ID,
					},
				})
			}

			// Remove the problematic subscriber to prevent future panics
			go r.Unsubscribe(userID)
		}
	}()

	// Attempt delivery with retry
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if err := sub.Receive(ctx, msg); err != nil {
			lastErr = err
			// Brief backoff before retry
			if attempt < 2 {
				time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
			}
			continue
		}
		// Success
		result.Status = DeliverySuccess
		return result
	}

	// All retries failed
	result.Status = DeliveryFailed
	result.Error = lastErr
	return result
}

// Start begins listening to the Redis stream for this room
func (r *Room) Start(ctx context.Context) {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.stopChan = make(chan struct{})
	r.mu.Unlock()

	go r.listenLoop(ctx)
}

// Stop stops listening to the Redis stream
func (r *Room) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.running {
		close(r.stopChan)
		r.running = false
	}
}

// IsRunning returns whether the room listener is active
func (r *Room) IsRunning() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.running
}

// LastActive returns when the room was last active
func (r *Room) LastActive() time.Time {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastActive
}

// listenLoop is the internal goroutine that listens to Redis Stream with panic recovery
func (r *Room) listenLoop(ctx context.Context) {
	// Recover from any panic and restart the loop
	defer func() {
		if recovered := recover(); recovered != nil {
			// Emit error event
			if r.relay != nil && r.relay.eventHandler != nil {
				r.relay.eventHandler(&Event{
					Type:      EventTypeError,
					RoomID:    r.config.ID,
					Timestamp: time.Now(),
					Data: map[string]interface{}{
						"error":     "listen_loop_panic",
						"recovered": recovered,
					},
				})
			}

			// Restart the loop after a brief delay
			r.mu.RLock()
			running := r.running
			r.mu.RUnlock()

			if running {
				time.Sleep(time.Second)
				go r.listenLoop(ctx) // Restart
			}
		}
	}()

	lastID := "$" // Start with only new messages
	consecutiveErrors := 0
	maxConsecutiveErrors := 10

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stopChan:
			return
		default:
			// Read from Redis Stream with blocking
			messages, err := r.relay.readFromStream(ctx, r.config.ID, lastID, 100, 5*time.Second)
			if err != nil {
				consecutiveErrors++

				// Exponential backoff with cap
				backoff := time.Duration(consecutiveErrors) * time.Second
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}

				// Emit error event after too many consecutive errors
				if consecutiveErrors >= maxConsecutiveErrors {
					if r.relay != nil && r.relay.eventHandler != nil {
						r.relay.eventHandler(&Event{
							Type:      EventTypeError,
							RoomID:    r.config.ID,
							Timestamp: time.Now(),
							Data: map[string]interface{}{
								"error":              "redis_stream_errors",
								"consecutive_errors": consecutiveErrors,
								"last_error":         err.Error(),
							},
						})
					}
				}

				time.Sleep(backoff)
				continue
			}

			// Reset error counter on success
			consecutiveErrors = 0

			for _, msg := range messages {
				// Safe broadcast (already has panic recovery)
				r.Broadcast(ctx, msg, &PublishOptions{SkipSender: true})
				lastID = msg.ID
				r.mu.Lock()
				r.lastActive = time.Now()
				r.mu.Unlock()
			}
		}
	}
}

// Close closes the room and disconnects all subscribers
func (r *Room) Close() error {
	r.Stop()

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, sub := range r.subscribers {
		sub.Close()
	}
	r.subscribers = make(map[string]Subscriber)
	r.presence = make(map[string]*PresenceInfo)

	return nil
}
