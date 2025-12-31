package engine

import (
	"encoding/json"
	"errors"
	"time"
)

// Protocol version
const ProtocolVersion = "1.0"

// Command types from client to server
type CommandType string

const (
	CmdConnect     CommandType = "connect"
	CmdSubscribe   CommandType = "subscribe"
	CmdUnsubscribe CommandType = "unsubscribe"
	CmdPublish     CommandType = "publish"
	CmdPresence    CommandType = "presence"
	CmdHistory     CommandType = "history"
	CmdPing        CommandType = "ping"
	CmdRPC         CommandType = "rpc"
)

// Push types from server to client
type PushType string

const (
	PushMessage     PushType = "message"
	PushJoin        PushType = "join"
	PushLeave       PushType = "leave"
	PushSubscribe   PushType = "subscribe"
	PushUnsubscribe PushType = "unsubscribe"
	PushConnect     PushType = "connect"
	PushDisconnect  PushType = "disconnect"
	PushPong        PushType = "pong"
	PushError       PushType = "error"
)

// Error codes
const (
	ErrCodeBadRequest       = 100
	ErrCodeUnauthorized     = 101
	ErrCodePermissionDenied = 102
	ErrCodeNotFound         = 103
	ErrCodeAlreadyExists    = 104
	ErrCodeInternal         = 105
	ErrCodeTimeout          = 106
)

var (
	ErrBadRequest       = errors.New("bad request")
	ErrUnauthorized     = errors.New("unauthorized")
	ErrPermissionDenied = errors.New("permission denied")
	ErrNotFound         = errors.New("not found")
	ErrAlreadyExists    = errors.New("already exists")
	ErrInternal         = errors.New("internal error")
)

// Command is a message from client to server
type Command struct {
	// ID is a unique command identifier for request-response matching
	ID uint64 `json:"id,omitempty"`

	// Type is the command type
	Type CommandType `json:"type"`

	// Channel for channel-specific commands
	Channel string `json:"channel,omitempty"`

	// Data contains command-specific payload
	Data json.RawMessage `json:"data,omitempty"`
}

// Reply is the server's response to a command
type Reply struct {
	// ID matches the command ID
	ID uint64 `json:"id,omitempty"`

	// Error if the command failed
	Error *Error `json:"error,omitempty"`

	// Result contains the successful response data
	Result json.RawMessage `json:"result,omitempty"`
}

// Push is an unsolicited message from server to client
type Push struct {
	// Type is the push type
	Type PushType `json:"type"`

	// Channel for channel-specific pushes
	Channel string `json:"channel,omitempty"`

	// Data contains push-specific payload
	Data json.RawMessage `json:"data,omitempty"`
}

// Error represents a protocol error
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	return e.Message
}

// NewError creates a new protocol error
func NewError(code int, message string) *Error {
	return &Error{Code: code, Message: message}
}

// --- Command Data Structures ---

// ConnectRequest is sent to establish a connection
type ConnectRequest struct {
	// Token is the authentication token (JWT or custom)
	Token string `json:"token,omitempty"`

	// Data is custom connection data
	Data json.RawMessage `json:"data,omitempty"`

	// Name is the client name/type (e.g., "web", "ios", "android")
	Name string `json:"name,omitempty"`

	// Version is the client version
	Version string `json:"version,omitempty"`
}

// ConnectResult is returned on successful connect
type ConnectResult struct {
	// Client is the unique client ID assigned by server
	Client string `json:"client"`

	// Version is the protocol version
	Version string `json:"version"`

	// Expires is when the connection token expires (0 = never)
	Expires int64 `json:"expires,omitempty"`

	// TTL is the time-to-live in seconds for refreshing
	TTL int64 `json:"ttl,omitempty"`

	// Data is custom data from server
	Data json.RawMessage `json:"data,omitempty"`

	// Ping is the server's ping interval in seconds
	Ping int64 `json:"ping,omitempty"`

	// Pong indicates if client should respond to pings
	Pong bool `json:"pong,omitempty"`
}

// SubscribeRequest is sent to subscribe to a channel
type SubscribeRequest struct {
	// Token is an optional channel subscription token
	Token string `json:"token,omitempty"`

	// Recover indicates if client wants to recover missed messages
	Recover bool `json:"recover,omitempty"`

	// Offset is the last known stream offset for recovery
	Offset string `json:"offset,omitempty"`

	// Epoch is the stream epoch for recovery validation
	Epoch string `json:"epoch,omitempty"`

	// Data is custom subscription data
	Data json.RawMessage `json:"data,omitempty"`
}

// SubscribeResult is returned on successful subscribe
type SubscribeResult struct {
	// Expires is when the subscription expires (0 = never)
	Expires int64 `json:"expires,omitempty"`

	// TTL is the time-to-live for refreshing
	TTL int64 `json:"ttl,omitempty"`

	// Recoverable indicates if the channel supports recovery
	Recoverable bool `json:"recoverable,omitempty"`

	// Offset is the current stream offset
	Offset string `json:"offset,omitempty"`

	// Epoch is the current stream epoch
	Epoch string `json:"epoch,omitempty"`

	// Publications are recovered messages
	Publications []Publication `json:"publications,omitempty"`

	// Recovered indicates if recovery was successful
	Recovered bool `json:"recovered,omitempty"`

	// Data is custom data from server
	Data json.RawMessage `json:"data,omitempty"`

	// WasRecovering indicates if there was a recovery attempt
	WasRecovering bool `json:"was_recovering,omitempty"`
}

// UnsubscribeRequest is sent to unsubscribe from a channel
type UnsubscribeRequest struct{}

// UnsubscribeResult is returned on successful unsubscribe
type UnsubscribeResult struct{}

// PublishRequest is sent to publish a message to a channel
type PublishRequest struct {
	// Data is the message payload
	Data json.RawMessage `json:"data"`
}

// PublishResult is returned on successful publish
type PublishResult struct {
	// Offset is the published message offset
	Offset string `json:"offset,omitempty"`
}

// PresenceRequest is sent to get channel presence
type PresenceRequest struct{}

// PresenceResult contains channel presence information
type PresenceResult struct {
	Presence map[string]ClientInfo `json:"presence"`
}

// HistoryRequest is sent to get channel history
type HistoryRequest struct {
	// Limit is the maximum number of messages to return
	Limit int64 `json:"limit,omitempty"`

	// Since is the offset to start from
	Since string `json:"since,omitempty"`

	// Reverse returns messages in reverse order
	Reverse bool `json:"reverse,omitempty"`
}

// HistoryResult contains channel history
type HistoryResult struct {
	Publications []Publication `json:"publications"`
	Offset       string        `json:"offset,omitempty"`
	Epoch        string        `json:"epoch,omitempty"`
}

// PingRequest is a keepalive ping
type PingRequest struct{}

// PongResult is the pong response
type PongResult struct{}

// --- Push Data Structures ---

// Publication is a message published to a channel
type Publication struct {
	// Offset is the message position in the stream
	Offset string `json:"offset,omitempty"`

	// Data is the message payload
	Data json.RawMessage `json:"data"`

	// Info is the publisher's client info (if available)
	Info *ClientInfo `json:"info,omitempty"`

	// Tags are optional message tags
	Tags map[string]string `json:"tags,omitempty"`
}

// ClientInfo contains information about a connected client
type ClientInfo struct {
	// User is the user ID
	User string `json:"user"`

	// Client is the unique client ID
	Client string `json:"client"`

	// ConnInfo is custom connection data
	ConnInfo json.RawMessage `json:"conn_info,omitempty"`

	// ChanInfo is custom channel data
	ChanInfo json.RawMessage `json:"chan_info,omitempty"`
}

// JoinPush is sent when a user joins a channel
type JoinPush struct {
	Info ClientInfo `json:"info"`
}

// LeavePush is sent when a user leaves a channel
type LeavePush struct {
	Info ClientInfo `json:"info"`
}

// DisconnectPush is sent when the client is being disconnected
type DisconnectPush struct {
	Code      int    `json:"code"`
	Reason    string `json:"reason"`
	Reconnect bool   `json:"reconnect"`
}

// --- Helper Functions ---

// ParseCommand parses a JSON command
func ParseCommand(data []byte) (*Command, error) {
	var cmd Command
	if err := json.Unmarshal(data, &cmd); err != nil {
		return nil, err
	}
	return &cmd, nil
}

// EncodeReply encodes a reply to JSON
func EncodeReply(reply *Reply) ([]byte, error) {
	return json.Marshal(reply)
}

// EncodePush encodes a push to JSON
func EncodePush(push *Push) ([]byte, error) {
	return json.Marshal(push)
}

// SuccessReply creates a successful reply
func SuccessReply(id uint64, result interface{}) (*Reply, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return &Reply{
		ID:     id,
		Result: data,
	}, nil
}

// ErrorReply creates an error reply
func ErrorReply(id uint64, code int, message string) *Reply {
	return &Reply{
		ID:    id,
		Error: NewError(code, message),
	}
}

// NewPublicationPush creates a message push
func NewPublicationPush(channel string, pub *Publication) (*Push, error) {
	data, err := json.Marshal(pub)
	if err != nil {
		return nil, err
	}
	return &Push{
		Type:    PushMessage,
		Channel: channel,
		Data:    data,
	}, nil
}

// NewJoinPush creates a join push
func NewJoinPush(channel string, info *ClientInfo) (*Push, error) {
	data, err := json.Marshal(JoinPush{Info: *info})
	if err != nil {
		return nil, err
	}
	return &Push{
		Type:    PushJoin,
		Channel: channel,
		Data:    data,
	}, nil
}

// NewLeavePush creates a leave push
func NewLeavePush(channel string, info *ClientInfo) (*Push, error) {
	data, err := json.Marshal(LeavePush{Info: *info})
	if err != nil {
		return nil, err
	}
	return &Push{
		Type:    PushLeave,
		Channel: channel,
		Data:    data,
	}, nil
}

// NewPongPush creates a pong push
func NewPongPush() *Push {
	return &Push{
		Type: PushPong,
	}
}

// NewDisconnectPush creates a disconnect push
func NewDisconnectPush(code int, reason string, reconnect bool) (*Push, error) {
	data, err := json.Marshal(DisconnectPush{
		Code:      code,
		Reason:    reason,
		Reconnect: reconnect,
	})
	if err != nil {
		return nil, err
	}
	return &Push{
		Type: PushDisconnect,
		Data: data,
	}, nil
}

// MessageEnvelope wraps either a Reply or Push for wire format
type MessageEnvelope struct {
	// Reply is set for request-response
	Reply *Reply `json:"reply,omitempty"`

	// Push is set for server-initiated messages
	Push *Push `json:"push,omitempty"`
}

// Timestamp returns current timestamp in milliseconds
func Timestamp() int64 {
	return time.Now().UnixMilli()
}
