// Package engine provides the real-time messaging engine for StreamRelay.
// This implements Centrifugo-like logic for pub/sub, rooms, and message delivery.
package engine

import (
	"context"
	"time"
)

// Subscriber is the interface that any connection handler must implement.
// This keeps the engine transport-agnostic (WebSocket, SSE, gRPC, etc.)
type Subscriber interface {
	// ID returns the unique identifier for this subscriber (e.g., UserID)
	ID() string

	// Receive is called when a message is ready for delivery.
	// The implementation should handle sending over the actual transport.
	Receive(ctx context.Context, msg *Message) error

	// Close is called when the subscriber should be disconnected
	Close() error
}

// Message represents a chat message in the system
type Message struct {
	// ID is the unique message identifier (from Redis Stream)
	ID string `json:"id"`

	// RoomID is the target room/channel
	RoomID string `json:"room_id"`

	// SenderID is the user who sent the message
	SenderID string `json:"sender_id"`

	// Type indicates the message type
	Type MessageType `json:"type"`

	// Content is the actual message payload
	Content []byte `json:"content"`

	// Metadata contains additional properties
	Metadata map[string]string `json:"metadata,omitempty"`

	// Timestamp when the message was created
	Timestamp time.Time `json:"timestamp"`
}

// MessageType defines the type of message
type MessageType string

const (
	MessageTypeText   MessageType = "text"
	MessageTypeImage  MessageType = "image"
	MessageTypeFile   MessageType = "file"
	MessageTypeSystem MessageType = "system"
	MessageTypeJoin   MessageType = "join"
	MessageTypeLeave  MessageType = "leave"
	MessageTypeTyping MessageType = "typing"
)

// DeliveryStatus represents the status of message delivery
type DeliveryStatus int

const (
	DeliveryPending DeliveryStatus = iota
	DeliverySuccess
	DeliveryFailed
	DeliveryOffline // User was offline, message buffered
)

// DeliveryResult contains the result of delivering a message
type DeliveryResult struct {
	UserID string
	Status DeliveryStatus
	Error  error
}

// DeliveryPanicError represents a panic that occurred during message delivery
type DeliveryPanicError struct {
	UserID    string
	Recovered interface{}
}

func (e *DeliveryPanicError) Error() string {
	return "panic during delivery to " + e.UserID
}

// RoomType defines the type of room
type RoomType string

const (
	RoomTypeGroup  RoomType = "group"  // Group chat (1-to-many)
	RoomTypeDirect RoomType = "direct" // Direct message (1-to-1)
)

// RoomConfig contains configuration for a room
type RoomConfig struct {
	// ID is the unique room identifier
	ID string

	// Type is either group or direct
	Type RoomType

	// HistorySize is max messages to keep in Redis Stream
	HistorySize int64

	// HistoryTTL is how long to keep messages
	HistoryTTL time.Duration

	// Members is the list of user IDs in this room
	Members []string
}

// PublishOptions contains options for publishing a message
type PublishOptions struct {
	// SkipSender if true, won't deliver back to sender
	SkipSender bool

	// Priority for message delivery (higher = more important)
	Priority int

	// TTL for the message (0 = use room default)
	TTL time.Duration

	// Persist if false, won't save to Redis Stream (ephemeral)
	Persist bool
}

// DefaultPublishOptions returns default publish options
func DefaultPublishOptions() *PublishOptions {
	return &PublishOptions{
		SkipSender: true,
		Persist:    true,
	}
}

// RecoveryOptions contains options for recovering missed messages
type RecoveryOptions struct {
	// FromID is the last message ID the subscriber received
	FromID string

	// Limit is the maximum number of messages to recover
	Limit int64

	// Since only recover messages from this time onwards
	Since time.Time
}

// Event represents a system event
type Event struct {
	Type      EventType
	RoomID    string
	UserID    string
	Timestamp time.Time
	Data      map[string]interface{}
}

// EventType defines the type of system event
type EventType string

const (
	EventTypeConnect     EventType = "connect"
	EventTypeDisconnect  EventType = "disconnect"
	EventTypeSubscribe   EventType = "subscribe"
	EventTypeUnsubscribe EventType = "unsubscribe"
	EventTypePublish     EventType = "publish"
	EventTypeDelivery    EventType = "delivery"
	EventTypeError       EventType = "error"
)

// EventHandler is a callback for handling events
type EventHandler func(event *Event)

// Stats contains runtime statistics
type Stats struct {
	ActiveRooms       int64   `json:"active_rooms"`
	ActiveSubscribers int64   `json:"active_subscribers"`
	TotalMessages     int64   `json:"total_messages"`
	MessagesPerSecond float64 `json:"messages_per_second"`
	PendingDeliveries int64   `json:"pending_deliveries"`
	Uptime            int64   `json:"uptime_seconds"`
}

// PresenceInfo contains information about a user's presence
type PresenceInfo struct {
	UserID      string                 `json:"user_id"`
	ClientID    string                 `json:"client_id"`
	ConnectedAt time.Time              `json:"connected_at"`
	Data        map[string]interface{} `json:"data,omitempty"`
}
