// Package streamrelay provides a high-performance, real-time message relay system
// inspired by Centrifugo, built entirely from scratch with zero external dependencies.
//
// StreamRelay combines a custom Redis Streams implementation with a Centrifugo-like
// messaging engine for pub/sub, rooms, and message delivery.
//
// Features:
//   - Custom RESP protocol for Redis communication (zero dependencies)
//   - Redis Streams for message persistence and recovery
//   - Centrifugo-like client protocol (connect, subscribe, publish, ping/pong)
//   - Channel namespaces with shared configuration
//   - Permission system with role-based access control
//   - Presence tracking and join/leave notifications
//
// Architecture:
//   - redis: Custom RESP protocol implementation and Redis Streams commands
//   - engine: Real-time messaging engine (rooms, subscribers, delivery)
//
// Basic usage:
//
//	relay, err := streamrelay.NewWithOptions(
//	    streamrelay.WithRedisAddr("localhost:6379"),
//	    streamrelay.WithPrefix("chat:"),
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer relay.Close()
//
//	// Create a room
//	room, _ := relay.CreateRoom(streamrelay.RoomConfig{
//	    ID:   "general",
//	    Type: streamrelay.RoomTypeGroup,
//	})
//
//	// Create client handler for WebSocket connection
//	handler := streamrelay.NewClientHandler(streamrelay.ClientHandlerConfig{
//	    Transport: myWebSocketTransport,
//	    Relay:     relay,
//	})
//
//	// Process incoming messages
//	handler.HandleMessage(data)
package streamrelay

import (
	"github.com/pico/streamrelay/engine"
	"github.com/pico/streamrelay/redis"
)

// Re-export main types from engine package
type (
	// Relay is the central message broker
	Relay = engine.Relay

	// Room represents a chat room
	Room = engine.Room

	// Message represents a chat message
	Message = engine.Message

	// Subscriber is the interface for receiving messages
	Subscriber = engine.Subscriber

	// RoomConfig contains room configuration
	RoomConfig = engine.RoomConfig

	// Config contains relay configuration
	Config = engine.Config

	// PublishOptions contains publish options
	PublishOptions = engine.PublishOptions

	// RecoveryOptions contains recovery options
	RecoveryOptions = engine.RecoveryOptions

	// DeliveryResult contains delivery result
	DeliveryResult = engine.DeliveryResult

	// Event represents a system event
	Event = engine.Event

	// PresenceInfo contains presence information
	PresenceInfo = engine.PresenceInfo

	// Stats contains runtime statistics
	Stats = engine.Stats

	// --- Protocol Types ---

	// Command is a client-to-server message
	Command = engine.Command

	// Reply is a server response to a command
	Reply = engine.Reply

	// Push is a server-initiated message
	Push = engine.Push

	// Publication is a message in a channel
	Publication = engine.Publication

	// ClientInfo contains client information
	ClientInfo = engine.ClientInfo

	// ConnectRequest is sent to establish connection
	ConnectRequest = engine.ConnectRequest

	// ConnectResult is returned on successful connect
	ConnectResult = engine.ConnectResult

	// SubscribeRequest is sent to subscribe to a channel
	SubscribeRequest = engine.SubscribeRequest

	// SubscribeResult is returned on successful subscribe
	SubscribeResult = engine.SubscribeResult

	// PublishRequest is sent to publish a message
	PublishRequest = engine.PublishRequest

	// PublishResult is returned on successful publish
	PublishResult = engine.PublishResult

	// PresenceRequest is sent to get presence
	PresenceRequest = engine.PresenceRequest

	// PresenceResult contains presence data
	PresenceResult = engine.PresenceResult

	// HistoryRequest is sent to get history
	HistoryRequest = engine.HistoryRequest

	// HistoryResult contains history data
	HistoryResult = engine.HistoryResult

	// --- Permission Types ---

	// Permission defines allowed actions
	Permission = engine.Permission

	// ChannelRule defines permissions for a channel pattern
	ChannelRule = engine.ChannelRule

	// PermissionChecker is the interface for checking permissions
	PermissionChecker = engine.PermissionChecker

	// DefaultPermissionChecker implements PermissionChecker
	DefaultPermissionChecker = engine.DefaultPermissionChecker

	// --- Namespace Types ---

	// Namespace groups channels with shared config
	Namespace = engine.Namespace

	// NamespaceOptions contains namespace settings
	NamespaceOptions = engine.NamespaceOptions

	// NamespaceManager manages namespaces
	NamespaceManager = engine.NamespaceManager

	// --- Client Handler ---

	// ClientHandler processes protocol messages
	ClientHandler = engine.ClientHandler

	// ClientHandlerConfig contains handler configuration
	ClientHandlerConfig = engine.ClientHandlerConfig

	// Transport is the interface for sending data
	Transport = engine.Transport
)

// Re-export constants and types
type (
	MessageType    = engine.MessageType
	RoomType       = engine.RoomType
	DeliveryStatus = engine.DeliveryStatus
	EventType      = engine.EventType
	EventHandler   = engine.EventHandler
)

// Message types
const (
	MessageTypeText   = engine.MessageTypeText
	MessageTypeImage  = engine.MessageTypeImage
	MessageTypeFile   = engine.MessageTypeFile
	MessageTypeSystem = engine.MessageTypeSystem
	MessageTypeJoin   = engine.MessageTypeJoin
	MessageTypeLeave  = engine.MessageTypeLeave
	MessageTypeTyping = engine.MessageTypeTyping
)

// Room types
const (
	RoomTypeGroup  = engine.RoomTypeGroup
	RoomTypeDirect = engine.RoomTypeDirect
)

// Delivery statuses
const (
	DeliveryPending = engine.DeliveryPending
	DeliverySuccess = engine.DeliverySuccess
	DeliveryFailed  = engine.DeliveryFailed
	DeliveryOffline = engine.DeliveryOffline
)

// Event types
const (
	EventTypeConnect     = engine.EventTypeConnect
	EventTypeDisconnect  = engine.EventTypeDisconnect
	EventTypeSubscribe   = engine.EventTypeSubscribe
	EventTypeUnsubscribe = engine.EventTypeUnsubscribe
	EventTypePublish     = engine.EventTypePublish
	EventTypeDelivery    = engine.EventTypeDelivery
	EventTypeError       = engine.EventTypeError
)

// Re-export functions
var (
	// New creates a new Relay with the given configuration
	New = engine.New

	// NewWithOptions creates a new Relay with functional options
	NewWithOptions = engine.NewWithOptions

	// DefaultConfig returns a default configuration
	DefaultConfig = engine.DefaultConfig

	// DefaultPublishOptions returns default publish options
	DefaultPublishOptions = engine.DefaultPublishOptions

	// NewRoomConfig creates a RoomConfig with options
	NewRoomConfig = engine.NewRoomConfig

	// GenerateDMRoomID creates a consistent DM room ID
	GenerateDMRoomID = engine.GenerateDMRoomID
)

// Re-export option functions
var (
	WithRedisAddr     = engine.WithRedisAddr
	WithRedisPassword = engine.WithRedisPassword
	WithRedisDB       = engine.WithRedisDB
	WithPrefix        = engine.WithPrefix
	WithPoolSize      = engine.WithPoolSize
	WithDialTimeout   = engine.WithDialTimeout
	WithReadTimeout   = engine.WithReadTimeout
	WithWriteTimeout  = engine.WithWriteTimeout
	WithHistorySize   = engine.WithHistorySize
	WithHistoryTTL    = engine.WithHistoryTTL
	WithEventHandler  = engine.WithEventHandler
	WithRedisConfig   = engine.WithRedisConfig

	WithRoomType        = engine.WithRoomType
	WithRoomMembers     = engine.WithRoomMembers
	WithRoomHistorySize = engine.WithRoomHistorySize
	WithRoomHistoryTTL  = engine.WithRoomHistoryTTL
)

// Redis package re-exports for advanced usage
type (
	// RedisClient is the low-level Redis client
	RedisClient = redis.Client

	// RedisConfig is the Redis configuration
	RedisConfig = redis.Config

	// RedisStream provides Redis Streams operations
	RedisStream = redis.Stream

	// StreamEntry is a Redis Stream entry
	StreamEntry = redis.StreamEntry

	// StreamResult is the result of reading streams
	StreamResult = redis.StreamResult
)

// Redis functions
var (
	// NewRedisClient creates a new Redis client
	NewRedisClient = redis.NewClient

	// DefaultRedisConfig returns default Redis config
	DefaultRedisConfig = redis.DefaultConfig

	// NewRedisStream creates a new Stream wrapper
	NewRedisStream = redis.NewStream
)

// --- Protocol exports ---

type (
	CommandType = engine.CommandType
	PushType    = engine.PushType
)

// Command types
const (
	CmdConnect     = engine.CmdConnect
	CmdSubscribe   = engine.CmdSubscribe
	CmdUnsubscribe = engine.CmdUnsubscribe
	CmdPublish     = engine.CmdPublish
	CmdPresence    = engine.CmdPresence
	CmdHistory     = engine.CmdHistory
	CmdPing        = engine.CmdPing
)

// Push types
const (
	PushMessage     = engine.PushMessage
	PushJoin        = engine.PushJoin
	PushLeave       = engine.PushLeave
	PushSubscribe   = engine.PushSubscribe
	PushUnsubscribe = engine.PushUnsubscribe
	PushConnect     = engine.PushConnect
	PushDisconnect  = engine.PushDisconnect
	PushPong        = engine.PushPong
	PushError       = engine.PushError
)

// Protocol error codes
const (
	ErrCodeBadRequest       = engine.ErrCodeBadRequest
	ErrCodeUnauthorized     = engine.ErrCodeUnauthorized
	ErrCodePermissionDenied = engine.ErrCodePermissionDenied
	ErrCodeNotFound         = engine.ErrCodeNotFound
	ErrCodeAlreadyExists    = engine.ErrCodeAlreadyExists
	ErrCodeInternal         = engine.ErrCodeInternal
	ErrCodeTimeout          = engine.ErrCodeTimeout
)

// Protocol functions
var (
	ParseCommand       = engine.ParseCommand
	EncodeReply        = engine.EncodeReply
	EncodePush         = engine.EncodePush
	SuccessReply       = engine.SuccessReply
	ErrorReply         = engine.ErrorReply
	NewPublicationPush = engine.NewPublicationPush
	NewJoinPush        = engine.NewJoinPush
	NewLeavePush       = engine.NewLeavePush
	NewPongPush        = engine.NewPongPush
	NewDisconnectPush  = engine.NewDisconnectPush
)

// --- Permission exports ---

// Permission constants
const (
	PermNone      = engine.PermNone
	PermSubscribe = engine.PermSubscribe
	PermPublish   = engine.PermPublish
	PermPresence  = engine.PermPresence
	PermHistory   = engine.PermHistory
	PermJoinLeave = engine.PermJoinLeave
	PermAll       = engine.PermAll
	PermReadOnly  = engine.PermReadOnly
	PermStandard  = engine.PermStandard
)

// Permission functions
var (
	NewPermissionChecker = engine.NewPermissionChecker
)

// --- Namespace exports ---

// Namespace functions
var (
	NewNamespaceManager     = engine.NewNamespaceManager
	DefaultNamespaceOptions = engine.DefaultNamespaceOptions

	// Preset namespace factories
	ChatNamespace         = engine.ChatNamespace
	NotificationNamespace = engine.NotificationNamespace
	PresenceNamespace     = engine.PresenceNamespace
	PublicNamespace       = engine.PublicNamespace
	PersonalNamespace     = engine.PersonalNamespace
)

// --- Client Handler exports ---

var (
	NewClientHandler = engine.NewClientHandler
)
