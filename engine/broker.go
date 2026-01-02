package engine

import (
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
	Channel string
	Data    []byte
	NodeID  string // Source node ID
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

// RedisBroker implements Broker using Redis Pub/Sub for multi-node sync.
// This enables horizontal scaling across multiple StreamRelay servers.
type RedisBroker struct {
	client   *redis.Client
	nodeID   string
	prefix   string
	subConn  *redis.Client // Dedicated connection for subscriptions
	handlers map[string]func(BrokerMessage)
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
	running  atomic.Bool
	msgChan  chan BrokerMessage
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

	ctx, cancel := context.WithCancel(context.Background())

	return &RedisBroker{
		client:   cfg.Client,
		nodeID:   cfg.NodeID,
		prefix:   cfg.Prefix,
		handlers: make(map[string]func(BrokerMessage)),
		ctx:      ctx,
		cancel:   cancel,
		msgChan:  make(chan BrokerMessage, cfg.BufferSize),
	}, nil
}

// NodeID returns this broker's node ID
func (b *RedisBroker) NodeID() string {
	return b.nodeID
}

// Publish sends a message to all nodes in the cluster via Redis Pub/Sub
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

	_, err = b.client.Do(ctx, "PUBLISH", pubChannel, string(payload))
	return err
}

// PublishMessage publishes a Message to the cluster
func (b *RedisBroker) PublishMessage(ctx context.Context, msg *Message) error {
	nodeMsg := NodeMessage{
		NodeID:    b.nodeID,
		RoomID:    msg.RoomID,
		MessageID: msg.ID,
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

// subscriptionLoop handles the Redis PSUBSCRIBE
func (b *RedisBroker) subscriptionLoop(pattern string) {
	defer b.running.Store(false)

	fullPattern := b.prefix + pattern

	for {
		select {
		case <-b.ctx.Done():
			return
		default:
		}

		// Subscribe to pattern
		_, err := b.subConn.Do(b.ctx, "PSUBSCRIBE", fullPattern)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}

		// Read messages
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

			// Send to channel
			select {
			case b.msgChan <- BrokerMessage{
				Channel: msgResp.Channel,
				Data:    wrapped.Data,
				NodeID:  wrapped.NodeID,
			}:
			default:
				// Channel full, drop message
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
}

// NewClusterRelay creates a new cluster-aware relay
func NewClusterRelay(cfg ClusterConfig) (*ClusterRelay, error) {
	// Create base relay
	relay, err := New(cfg.Config)
	if err != nil {
		return nil, err
	}

	// Create broker
	broker, err := NewRedisBroker(RedisBrokerConfig{
		Client: relay.Client(),
		NodeID: cfg.NodeID,
		Prefix: cfg.BrokerPrefix,
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
func (cr *ClusterRelay) Publish(ctx context.Context, roomID string, msg *Message, opts *PublishOptions) ([]DeliveryResult, error) {
	// First, publish locally (this persists to Redis Stream)
	results, err := cr.Relay.Publish(ctx, roomID, msg, opts)
	if err != nil {
		return nil, err
	}

	// Then, broadcast to other nodes via Pub/Sub
	if err := cr.broker.PublishMessage(ctx, msg); err != nil {
		// Log but don't fail - local publish succeeded
		// Other nodes will eventually get the message from Redis Stream
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
