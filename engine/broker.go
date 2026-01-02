package engine

import (
	"container/list"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pico/streamrelay/redis"
)

// Broker is the interface for inter-node communication in a cluster.
// Implement this to sync messages between multiple StreamRelay servers.
type Broker interface {
	// Publish broadcasts a message to all nodes in the cluster
	Publish(ctx context.Context, channel string, data []byte) error

	// Subscribe starts listening for messages from other nodes
	Subscribe(ctx context.Context, pattern string) (<-chan BrokerMessage, error)

	// Close shuts down the broker
	Close() error
}

// BrokerMessage is a message received from the broker
type BrokerMessage struct {
	Channel   string
	Data      []byte
	NodeID    string // Source node ID
	MessageID string // For deduplication
}

// NodeMessage is the message format sent between nodes
type NodeMessage struct {
	NodeID    string            `json:"node_id"`
	RoomID    string            `json:"room_id"`
	MessageID string            `json:"message_id"`
	SenderID  string            `json:"sender_id"`
	Type      string            `json:"type"`
	Content   []byte            `json:"content"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Timestamp int64             `json:"timestamp"`
}

// --- Deduplication Cache ---

// DedupeCache tracks recently seen message IDs to prevent duplicates
type DedupeCache struct {
	cache    map[string]time.Time
	order    *list.List // LRU order
	elements map[string]*list.Element
	maxSize  int
	ttl      time.Duration
	mu       sync.RWMutex
}

// NewDedupeCache creates a new deduplication cache
func NewDedupeCache(maxSize int, ttl time.Duration) *DedupeCache {
	if maxSize <= 0 {
		maxSize = 10000
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &DedupeCache{
		cache:    make(map[string]time.Time),
		order:    list.New(),
		elements: make(map[string]*list.Element),
		maxSize:  maxSize,
		ttl:      ttl,
	}
}

// IsDuplicate checks if message ID was seen recently, and marks it as seen
func (d *DedupeCache) IsDuplicate(messageID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if exists and not expired
	if ts, exists := d.cache[messageID]; exists {
		if time.Since(ts) < d.ttl {
			return true // Duplicate
		}
		// Expired, remove old entry
		if elem, ok := d.elements[messageID]; ok {
			d.order.Remove(elem)
			delete(d.elements, messageID)
		}
	}

	// Add to cache
	d.cache[messageID] = time.Now()
	elem := d.order.PushBack(messageID)
	d.elements[messageID] = elem

	// Evict old entries if over size
	for d.order.Len() > d.maxSize {
		oldest := d.order.Front()
		if oldest != nil {
			oldID := oldest.Value.(string)
			d.order.Remove(oldest)
			delete(d.elements, oldID)
			delete(d.cache, oldID)
		}
	}

	return false
}

// Cleanup removes expired entries
func (d *DedupeCache) Cleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	for id, ts := range d.cache {
		if now.Sub(ts) > d.ttl {
			if elem, ok := d.elements[id]; ok {
				d.order.Remove(elem)
				delete(d.elements, id)
			}
			delete(d.cache, id)
		}
	}
}

// --- Redis Broker ---

// RedisBroker implements Broker using Redis Pub/Sub for multi-node sync.
// Features:
// - Message deduplication to prevent double delivery
// - Automatic reconnection on failure
// - Configurable retry policy
type RedisBroker struct {
	client     *redis.Client
	nodeID     string
	prefix     string
	subConn    *redis.Client
	handlers   map[string]func(BrokerMessage)
	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
	running    atomic.Bool
	msgChan    chan BrokerMessage
	dedupe     *DedupeCache
	maxRetries int
	retryDelay time.Duration
}

// RedisBrokerConfig contains configuration for RedisBroker
type RedisBrokerConfig struct {
	// Redis client for publishing
	Client *redis.Client

	// NodeID is the unique identifier for this node (auto-generated if empty)
	NodeID string

	// Prefix for pub/sub channels (default: "sr:pub:")
	Prefix string

	// BufferSize for the message channel (default: 1000)
	BufferSize int

	// DedupeSize is max message IDs to track (default: 10000)
	DedupeSize int

	// DedupeTTL is how long to remember message IDs (default: 5 min)
	DedupeTTL time.Duration

	// MaxRetries for publish failures (default: 3)
	MaxRetries int

	// RetryDelay between retries (default: 100ms)
	RetryDelay time.Duration
}

// NewRedisBroker creates a new Redis-based broker for cluster communication
func NewRedisBroker(cfg RedisBrokerConfig) (*RedisBroker, error) {
	if cfg.Client == nil {
		return nil, fmt.Errorf("redis client is required")
	}

	if cfg.NodeID == "" {
		cfg.NodeID = generateNodeID()
	}

	if cfg.Prefix == "" {
		cfg.Prefix = "sr:pub:"
	}

	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 1000
	}

	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}

	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = 100 * time.Millisecond
	}

	ctx, cancel := context.WithCancel(context.Background())

	broker := &RedisBroker{
		client:     cfg.Client,
		nodeID:     cfg.NodeID,
		prefix:     cfg.Prefix,
		handlers:   make(map[string]func(BrokerMessage)),
		ctx:        ctx,
		cancel:     cancel,
		msgChan:    make(chan BrokerMessage, cfg.BufferSize),
		dedupe:     NewDedupeCache(cfg.DedupeSize, cfg.DedupeTTL),
		maxRetries: cfg.MaxRetries,
		retryDelay: cfg.RetryDelay,
	}

	// Start cleanup goroutine
	go broker.cleanupLoop()

	return broker, nil
}

// cleanupLoop periodically cleans expired dedup entries
func (b *RedisBroker) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-b.ctx.Done():
			return
		case <-ticker.C:
			b.dedupe.Cleanup()
		}
	}
}

// NodeID returns this broker's node ID
func (b *RedisBroker) NodeID() string {
	return b.nodeID
}

// Publish sends a message to all nodes in the cluster via Redis Pub/Sub
// Includes retry logic for reliability
func (b *RedisBroker) Publish(ctx context.Context, channel string, data []byte) error {
	pubChannel := b.prefix + channel

	// Wrap with node ID to prevent echo
	wrapped := struct {
		NodeID string `json:"n"`
		Data   []byte `json:"d"`
	}{
		NodeID: b.nodeID,
		Data:   data,
	}

	payload, err := json.Marshal(wrapped)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Retry logic
	var lastErr error
	for attempt := 0; attempt < b.maxRetries; attempt++ {
		_, err = b.client.Do(ctx, "PUBLISH", pubChannel, string(payload))
		if err == nil {
			return nil // Success
		}
		lastErr = err

		// Check if context is cancelled
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Wait before retry
		if attempt < b.maxRetries-1 {
			time.Sleep(b.retryDelay * time.Duration(attempt+1))
		}
	}

	return fmt.Errorf("publish failed after %d retries: %w", b.maxRetries, lastErr)
}

// PublishMessage publishes a Message to the cluster with deduplication ID
func (b *RedisBroker) PublishMessage(ctx context.Context, msg *Message) error {
	nodeMsg := NodeMessage{
		NodeID:    b.nodeID,
		RoomID:    msg.RoomID,
		MessageID: msg.ID, // This is the dedup key
		SenderID:  msg.SenderID,
		Type:      string(msg.Type),
		Content:   msg.Content,
		Metadata:  msg.Metadata,
		Timestamp: msg.Timestamp.UnixMilli(),
	}

	data, err := json.Marshal(nodeMsg)
	if err != nil {
		return err
	}

	return b.Publish(ctx, msg.RoomID, data)
}

// Subscribe starts listening for messages on a pattern
func (b *RedisBroker) Subscribe(ctx context.Context, pattern string) (<-chan BrokerMessage, error) {
	if b.running.Load() {
		return b.msgChan, nil
	}

	// Create dedicated connection for subscription
	subClient, err := redis.NewClient(b.client.Config())
	if err != nil {
		return nil, fmt.Errorf("failed to create subscription client: %w", err)
	}
	b.subConn = subClient

	b.running.Store(true)

	// Start subscription loop
	go b.subscriptionLoop(pattern)

	return b.msgChan, nil
}

// subscriptionLoop handles the Redis PSUBSCRIBE with auto-reconnect
func (b *RedisBroker) subscriptionLoop(pattern string) {
	defer b.running.Store(false)

	fullPattern := b.prefix + pattern
	consecutiveErrors := 0
	maxConsecutiveErrors := 10

	for {
		select {
		case <-b.ctx.Done():
			return
		default:
		}

		// Subscribe to pattern
		_, err := b.subConn.Do(b.ctx, "PSUBSCRIBE", fullPattern)
		if err != nil {
			consecutiveErrors++
			backoff := time.Duration(consecutiveErrors) * time.Second
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			time.Sleep(backoff)

			// Try to reconnect
			if consecutiveErrors >= maxConsecutiveErrors {
				// Create new connection
				newConn, err := redis.NewClient(b.client.Config())
				if err == nil {
					b.subConn.Close()
					b.subConn = newConn
					consecutiveErrors = 0
				}
			}
			continue
		}

		consecutiveErrors = 0

		// Read messages loop
		for {
			select {
			case <-b.ctx.Done():
				return
			default:
			}

			// Read next message (blocking)
			msgResp, err := b.subConn.ReadPubSubMessage(b.ctx)
			if err != nil {
				break // Reconnect
			}

			// Parse the message
			if msgResp.Type != "pmessage" {
				continue
			}

			// Unwrap the message
			var wrapped struct {
				NodeID string `json:"n"`
				Data   []byte `json:"d"`
			}
			if err := json.Unmarshal([]byte(msgResp.Data), &wrapped); err != nil {
				continue
			}

			// Skip messages from ourselves
			if wrapped.NodeID == b.nodeID {
				continue
			}

			// Parse inner message to get ID for deduplication
			var nodeMsg NodeMessage
			if err := json.Unmarshal(wrapped.Data, &nodeMsg); err != nil {
				continue
			}

			// Check for duplicate
			if b.dedupe.IsDuplicate(nodeMsg.MessageID) {
				continue // Already processed
			}

			// Send to channel
			select {
			case b.msgChan <- BrokerMessage{
				Channel:   msgResp.Channel,
				Data:      wrapped.Data,
				NodeID:    wrapped.NodeID,
				MessageID: nodeMsg.MessageID,
			}:
			default:
				// Channel full, drop message (it's in Redis Stream anyway)
			}
		}
	}
}

// Close shuts down the broker
func (b *RedisBroker) Close() error {
	b.cancel()

	if b.subConn != nil {
		b.subConn.Close()
	}

	close(b.msgChan)
	return nil
}

// --- Cluster-aware Relay ---

// ClusterRelay extends Relay with multi-node support via Redis Pub/Sub
// Features:
// - Automatic message sync between nodes
// - Deduplication prevents double delivery
// - Graceful handling of pub/sub failures
type ClusterRelay struct {
	*Relay
	broker *RedisBroker
	ctx    context.Context
	cancel context.CancelFunc
}

// ClusterConfig contains configuration for ClusterRelay
type ClusterConfig struct {
	// Base relay configuration
	Config

	// NodeID for this server (auto-generated if empty)
	NodeID string

	// BrokerPrefix for pub/sub channels
	BrokerPrefix string

	// DedupeSize is max message IDs to track (default: 10000)
	DedupeSize int

	// DedupeTTL is how long to remember message IDs (default: 5 min)
	DedupeTTL time.Duration
}

// NewClusterRelay creates a new cluster-aware relay
func NewClusterRelay(cfg ClusterConfig) (*ClusterRelay, error) {
	// Create base relay
	relay, err := New(cfg.Config)
	if err != nil {
		return nil, err
	}

	// Create broker with deduplication
	broker, err := NewRedisBroker(RedisBrokerConfig{
		Client:     relay.Client(),
		NodeID:     cfg.NodeID,
		Prefix:     cfg.BrokerPrefix,
		DedupeSize: cfg.DedupeSize,
		DedupeTTL:  cfg.DedupeTTL,
	})
	if err != nil {
		relay.Close()
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	cr := &ClusterRelay{
		Relay:  relay,
		broker: broker,
		ctx:    ctx,
		cancel: cancel,
	}

	// Start listening for messages from other nodes
	go cr.listenForBrokerMessages()

	return cr, nil
}

// listenForBrokerMessages handles messages from other nodes
func (cr *ClusterRelay) listenForBrokerMessages() {
	msgChan, err := cr.broker.Subscribe(cr.ctx, "*")
	if err != nil {
		return
	}

	for msg := range msgChan {
		cr.handleBrokerMessage(msg)
	}
}

// handleBrokerMessage processes a message from another node
func (cr *ClusterRelay) handleBrokerMessage(bMsg BrokerMessage) {
	var nodeMsg NodeMessage
	if err := json.Unmarshal(bMsg.Data, &nodeMsg); err != nil {
		return
	}

	// Convert to Message
	msg := &Message{
		ID:        nodeMsg.MessageID,
		RoomID:    nodeMsg.RoomID,
		SenderID:  nodeMsg.SenderID,
		Type:      MessageType(nodeMsg.Type),
		Content:   nodeMsg.Content,
		Metadata:  nodeMsg.Metadata,
		Timestamp: time.UnixMilli(nodeMsg.Timestamp),
	}

	// Get the room and broadcast locally (don't persist again, already in Redis Stream)
	room, exists := cr.GetRoom(msg.RoomID)
	if !exists {
		return
	}

	// Broadcast to local subscribers only
	room.Broadcast(cr.ctx, msg, &PublishOptions{
		SkipSender: true,
		Persist:    false, // Already persisted by origin node
	})
}

// Publish sends a message to a room and syncs to cluster
// If pub/sub fails, message is still in Redis Stream for recovery
func (cr *ClusterRelay) Publish(ctx context.Context, roomID string, msg *Message, opts *PublishOptions) ([]DeliveryResult, error) {
	// First, publish locally (this persists to Redis Stream)
	// This is the source of truth - if this succeeds, message is safe
	results, err := cr.Relay.Publish(ctx, roomID, msg, opts)
	if err != nil {
		return nil, err
	}

	// Then, broadcast to other nodes via Pub/Sub
	// If this fails, other nodes will still get the message from Redis Stream
	// when they read from the stream (eventual consistency)
	if pubErr := cr.broker.PublishMessage(ctx, msg); pubErr != nil {
		// Log but don't fail - message is already in Redis Stream
		// Other nodes will get it via stream recovery
		if cr.Relay.eventHandler != nil {
			cr.Relay.eventHandler(&Event{
				Type:      EventTypeError,
				RoomID:    roomID,
				Timestamp: time.Now(),
				Data: map[string]interface{}{
					"error":      "pubsub_publish_failed",
					"message_id": msg.ID,
					"details":    pubErr.Error(),
				},
			})
		}
	}

	return results, nil
}

// NodeID returns this node's unique identifier
func (cr *ClusterRelay) NodeID() string {
	return cr.broker.NodeID()
}

// Broker returns the underlying broker
func (cr *ClusterRelay) Broker() *RedisBroker {
	return cr.broker
}

// Close shuts down the cluster relay
func (cr *ClusterRelay) Close() error {
	cr.cancel()
	cr.broker.Close()
	return cr.Relay.Close()
}

// --- Helper functions ---

// generateNodeID creates a unique node identifier
func generateNodeID() string {
	return fmt.Sprintf("node-%d-%d", time.Now().UnixNano(), time.Now().Nanosecond())
}
