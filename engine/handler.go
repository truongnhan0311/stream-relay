package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ClientHandler handles protocol messages for a single client connection.
// It implements the Subscriber interface and processes all protocol commands.
type ClientHandler struct {
	// Client identity
	clientID string
	userID   string

	// Connection state
	connected    bool
	connectedAt  time.Time
	lastActivity time.Time

	// Transport interface for sending data
	transport Transport

	// Reference to relay
	relay *Relay

	// Subscriptions: channel -> permission
	subscriptions map[string]Permission
	subMu         sync.RWMutex

	// Custom data
	connInfo json.RawMessage
	chanInfo map[string]json.RawMessage

	// Permission checker
	permChecker PermissionChecker

	// Namespace manager
	nsManager *NamespaceManager

	// Ping/pong
	pingInterval time.Duration
	pongTimeout  time.Duration
	lastPing     time.Time
	pingTimer    *time.Timer

	// Close handling
	closed   atomic.Bool
	closeMu  sync.Mutex
	closeCh  chan struct{}
	closeErr error

	mu sync.RWMutex
}

// Transport is the interface for sending data to the client.
// Implement this for WebSocket, SSE, or any transport.
type Transport interface {
	// Write sends data to the client
	Write(data []byte) error

	// Close closes the transport
	Close() error
}

// ClientHandlerConfig contains configuration for a client handler
type ClientHandlerConfig struct {
	// Transport for sending data
	Transport Transport

	// Relay reference
	Relay *Relay

	// PermissionChecker for access control
	PermissionChecker PermissionChecker

	// NamespaceManager for channel config
	NamespaceManager *NamespaceManager

	// PingInterval for keepalive (0 = disabled)
	PingInterval time.Duration

	// PongTimeout for ping response
	PongTimeout time.Duration
}

// NewClientHandler creates a new client handler
func NewClientHandler(cfg ClientHandlerConfig) *ClientHandler {
	clientID := generateClientID()

	h := &ClientHandler{
		clientID:      clientID,
		transport:     cfg.Transport,
		relay:         cfg.Relay,
		subscriptions: make(map[string]Permission),
		chanInfo:      make(map[string]json.RawMessage),
		permChecker:   cfg.PermissionChecker,
		nsManager:     cfg.NamespaceManager,
		pingInterval:  cfg.PingInterval,
		pongTimeout:   cfg.PongTimeout,
		closeCh:       make(chan struct{}),
	}

	if h.pingInterval == 0 {
		h.pingInterval = 25 * time.Second
	}
	if h.pongTimeout == 0 {
		h.pongTimeout = 10 * time.Second
	}
	if h.permChecker == nil {
		h.permChecker = NewPermissionChecker()
	}
	if h.nsManager == nil {
		h.nsManager = NewNamespaceManager()
	}

	return h
}

// generateClientID generates a unique client ID
func generateClientID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().UnixNano()%1000000)
}

// ID returns the client ID (implements Subscriber interface)
func (h *ClientHandler) ID() string {
	return h.userID
}

// ClientID returns the unique client identifier
func (h *ClientHandler) ClientID() string {
	return h.clientID
}

// UserID returns the authenticated user ID
func (h *ClientHandler) UserID() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.userID
}

// IsConnected returns whether the client is connected
func (h *ClientHandler) IsConnected() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.connected
}

// Receive delivers a message to the client (implements Subscriber interface)
func (h *ClientHandler) Receive(ctx context.Context, msg *Message) error {
	if h.closed.Load() {
		return ErrClosed
	}

	// Convert to Publication
	pub := &Publication{
		Offset: msg.ID,
		Data:   msg.Content,
	}

	if msg.SenderID != "" {
		pub.Info = &ClientInfo{
			User: msg.SenderID,
		}
	}

	// Create push
	push, err := NewPublicationPush(msg.RoomID, pub)
	if err != nil {
		return err
	}

	return h.sendPush(push)
}

// Close closes the client handler (implements Subscriber interface)
func (h *ClientHandler) Close() error {
	if h.closed.Swap(true) {
		return nil // Already closed
	}

	h.closeMu.Lock()
	defer h.closeMu.Unlock()

	close(h.closeCh)

	// Stop ping timer
	if h.pingTimer != nil {
		h.pingTimer.Stop()
	}

	// Unsubscribe from all channels
	h.subMu.Lock()
	channels := make([]string, 0, len(h.subscriptions))
	for ch := range h.subscriptions {
		channels = append(channels, ch)
	}
	h.subMu.Unlock()

	for _, ch := range channels {
		h.relay.Unsubscribe(ch, h.userID)
	}

	return h.transport.Close()
}

// HandleMessage processes an incoming message from the client
func (h *ClientHandler) HandleMessage(data []byte) error {
	h.mu.Lock()
	h.lastActivity = time.Now()
	h.mu.Unlock()

	cmd, err := ParseCommand(data)
	if err != nil {
		return h.sendError(0, ErrCodeBadRequest, "invalid command format")
	}

	return h.handleCommand(cmd)
}

// handleCommand routes a command to the appropriate handler
func (h *ClientHandler) handleCommand(cmd *Command) error {
	switch cmd.Type {
	case CmdConnect:
		return h.handleConnect(cmd)
	case CmdSubscribe:
		return h.handleSubscribe(cmd)
	case CmdUnsubscribe:
		return h.handleUnsubscribe(cmd)
	case CmdPublish:
		return h.handlePublish(cmd)
	case CmdPresence:
		return h.handlePresence(cmd)
	case CmdHistory:
		return h.handleHistory(cmd)
	case CmdPing:
		return h.handlePing(cmd)
	default:
		return h.sendError(cmd.ID, ErrCodeBadRequest, "unknown command type")
	}
}

// handleConnect processes a connect command
func (h *ClientHandler) handleConnect(cmd *Command) error {
	var req ConnectRequest
	if cmd.Data != nil {
		if err := json.Unmarshal(cmd.Data, &req); err != nil {
			return h.sendError(cmd.ID, ErrCodeBadRequest, "invalid connect request")
		}
	}

	// Check connection permission
	userID := h.extractUserFromToken(req.Token)
	ok, err := h.permChecker.CheckConnect(userID, req.Token)
	if err != nil || !ok {
		return h.sendError(cmd.ID, ErrCodeUnauthorized, "connection denied")
	}

	h.mu.Lock()
	h.userID = userID
	h.connected = true
	h.connectedAt = time.Now()
	h.connInfo = req.Data
	h.mu.Unlock()

	// Start ping timer
	h.startPingTimer()

	// Send connect result
	result := ConnectResult{
		Client:  h.clientID,
		Version: ProtocolVersion,
		Ping:    int64(h.pingInterval.Seconds()),
		Pong:    true,
	}

	return h.sendResult(cmd.ID, result)
}

// handleSubscribe processes a subscribe command
func (h *ClientHandler) handleSubscribe(cmd *Command) error {
	if !h.IsConnected() {
		return h.sendError(cmd.ID, ErrCodeUnauthorized, "not connected")
	}

	channel := cmd.Channel
	if channel == "" {
		return h.sendError(cmd.ID, ErrCodeBadRequest, "channel required")
	}

	var req SubscribeRequest
	if cmd.Data != nil {
		json.Unmarshal(cmd.Data, &req)
	}

	// Get namespace for channel
	ns := h.nsManager.GetByChannel(channel)
	if err := ns.ValidateChannel(channel); err != nil {
		return h.sendError(cmd.ID, ErrCodeBadRequest, "invalid channel name")
	}

	// Check permission
	perm, err := h.permChecker.CheckSubscribe(h.userID, channel, req.Token)
	if err != nil {
		code := ErrCodePermissionDenied
		if err == ErrUnauthorized {
			code = ErrCodeUnauthorized
		}
		return h.sendError(cmd.ID, code, err.Error())
	}

	// Get or create room
	room, err := h.relay.GetOrCreateRoom(RoomConfig{
		ID:          channel,
		Type:        RoomTypeGroup,
		HistorySize: ns.Options.HistorySize,
		HistoryTTL:  ns.Options.HistoryTTL,
	})
	if err != nil {
		return h.sendError(cmd.ID, ErrCodeInternal, "failed to access channel")
	}

	// Check max subscribers
	if ns.Options.MaxSubscribers > 0 && room.SubscriberCount() >= ns.Options.MaxSubscribers {
		return h.sendError(cmd.ID, ErrCodePermissionDenied, "channel is full")
	}

	// Create presence info
	presenceInfo := &PresenceInfo{
		UserID:      h.userID,
		ClientID:    h.clientID,
		ConnectedAt: time.Now(),
	}

	// Subscribe
	if err := room.Subscribe(h, presenceInfo); err != nil {
		return h.sendError(cmd.ID, ErrCodeInternal, "failed to subscribe")
	}

	// Store subscription
	h.subMu.Lock()
	h.subscriptions[channel] = perm
	h.chanInfo[channel] = req.Data
	h.subMu.Unlock()

	// Prepare result
	result := SubscribeResult{
		Recoverable: ns.Options.HistoryRecover,
	}

	// Handle recovery
	if req.Recover && ns.Options.HistoryRecover && req.Offset != "" {
		ctx := context.Background()
		messages, err := h.relay.RecoverMessages(ctx, channel, h.userID, &RecoveryOptions{
			FromID: req.Offset,
			Limit:  100,
		})
		if err == nil && len(messages) > 0 {
			result.Recovered = true
			result.WasRecovering = true
			result.Publications = make([]Publication, len(messages))
			for i, msg := range messages {
				result.Publications[i] = Publication{
					Offset: msg.ID,
					Data:   msg.Content,
				}
				if msg.SenderID != "" {
					result.Publications[i].Info = &ClientInfo{User: msg.SenderID}
				}
			}
			if len(messages) > 0 {
				result.Offset = messages[len(messages)-1].ID
			}
		}
	}

	// Send join notification if enabled
	if ns.Options.JoinLeave && perm.Has(PermJoinLeave) {
		h.broadcastJoin(channel)
	}

	return h.sendResult(cmd.ID, result)
}

// handleUnsubscribe processes an unsubscribe command
func (h *ClientHandler) handleUnsubscribe(cmd *Command) error {
	if !h.IsConnected() {
		return h.sendError(cmd.ID, ErrCodeUnauthorized, "not connected")
	}

	channel := cmd.Channel
	if channel == "" {
		return h.sendError(cmd.ID, ErrCodeBadRequest, "channel required")
	}

	h.subMu.Lock()
	perm, subscribed := h.subscriptions[channel]
	if subscribed {
		delete(h.subscriptions, channel)
		delete(h.chanInfo, channel)
	}
	h.subMu.Unlock()

	if !subscribed {
		return h.sendError(cmd.ID, ErrCodeNotFound, "not subscribed")
	}

	// Unsubscribe from room
	h.relay.Unsubscribe(channel, h.userID)

	// Send leave notification
	ns := h.nsManager.GetByChannel(channel)
	if ns.Options.JoinLeave && perm.Has(PermJoinLeave) {
		h.broadcastLeave(channel)
	}

	return h.sendResult(cmd.ID, UnsubscribeResult{})
}

// handlePublish processes a publish command
func (h *ClientHandler) handlePublish(cmd *Command) error {
	if !h.IsConnected() {
		return h.sendError(cmd.ID, ErrCodeUnauthorized, "not connected")
	}

	channel := cmd.Channel
	if channel == "" {
		return h.sendError(cmd.ID, ErrCodeBadRequest, "channel required")
	}

	var req PublishRequest
	if err := json.Unmarshal(cmd.Data, &req); err != nil {
		return h.sendError(cmd.ID, ErrCodeBadRequest, "invalid publish request")
	}

	// Get namespace
	ns := h.nsManager.GetByChannel(channel)
	if !ns.Options.Publish {
		return h.sendError(cmd.ID, ErrCodePermissionDenied, "publishing not allowed")
	}

	// Check if subscribed (if required)
	h.subMu.RLock()
	perm, subscribed := h.subscriptions[channel]
	h.subMu.RUnlock()

	if ns.Options.SubscribeToPublish && !subscribed {
		return h.sendError(cmd.ID, ErrCodePermissionDenied, "must subscribe to publish")
	}

	// Check publish permission
	if !perm.Has(PermPublish) {
		ok, err := h.permChecker.CheckPublish(h.userID, channel)
		if !ok || err != nil {
			return h.sendError(cmd.ID, ErrCodePermissionDenied, "publish not allowed")
		}
	}

	// Publish message
	ctx := context.Background()
	msg := &Message{
		SenderID:  h.userID,
		RoomID:    channel,
		Type:      MessageTypeText,
		Content:   req.Data,
		Timestamp: time.Now(),
	}

	_, err := h.relay.Publish(ctx, channel, msg, &PublishOptions{
		SkipSender: true,
		Persist:    ns.Options.HistorySize > 0,
	})
	if err != nil {
		return h.sendError(cmd.ID, ErrCodeInternal, "failed to publish")
	}

	result := PublishResult{
		Offset: msg.ID,
	}

	return h.sendResult(cmd.ID, result)
}

// handlePresence processes a presence command
func (h *ClientHandler) handlePresence(cmd *Command) error {
	if !h.IsConnected() {
		return h.sendError(cmd.ID, ErrCodeUnauthorized, "not connected")
	}

	channel := cmd.Channel
	if channel == "" {
		return h.sendError(cmd.ID, ErrCodeBadRequest, "channel required")
	}

	// Check subscription and permission
	h.subMu.RLock()
	perm, subscribed := h.subscriptions[channel]
	h.subMu.RUnlock()

	if !subscribed {
		return h.sendError(cmd.ID, ErrCodePermissionDenied, "not subscribed")
	}
	if !perm.Has(PermPresence) {
		return h.sendError(cmd.ID, ErrCodePermissionDenied, "presence not allowed")
	}

	// Get presence
	presenceList, err := h.relay.GetPresence(channel)
	if err != nil {
		return h.sendError(cmd.ID, ErrCodeInternal, "failed to get presence")
	}

	presence := make(map[string]ClientInfo)
	for _, p := range presenceList {
		presence[p.ClientID] = ClientInfo{
			User:   p.UserID,
			Client: p.ClientID,
		}
	}

	return h.sendResult(cmd.ID, PresenceResult{Presence: presence})
}

// handleHistory processes a history command
func (h *ClientHandler) handleHistory(cmd *Command) error {
	if !h.IsConnected() {
		return h.sendError(cmd.ID, ErrCodeUnauthorized, "not connected")
	}

	channel := cmd.Channel
	if channel == "" {
		return h.sendError(cmd.ID, ErrCodeBadRequest, "channel required")
	}

	// Check subscription and permission
	h.subMu.RLock()
	perm, subscribed := h.subscriptions[channel]
	h.subMu.RUnlock()

	if !subscribed {
		return h.sendError(cmd.ID, ErrCodePermissionDenied, "not subscribed")
	}
	if !perm.Has(PermHistory) {
		return h.sendError(cmd.ID, ErrCodePermissionDenied, "history not allowed")
	}

	var req HistoryRequest
	if cmd.Data != nil {
		json.Unmarshal(cmd.Data, &req)
	}

	limit := req.Limit
	if limit <= 0 || limit > 100 {
		limit = 100
	}

	ctx := context.Background()
	messages, err := h.relay.RecoverMessages(ctx, channel, h.userID, &RecoveryOptions{
		FromID: req.Since,
		Limit:  limit,
	})
	if err != nil {
		return h.sendError(cmd.ID, ErrCodeInternal, "failed to get history")
	}

	pubs := make([]Publication, len(messages))
	for i, msg := range messages {
		pubs[i] = Publication{
			Offset: msg.ID,
			Data:   msg.Content,
		}
		if msg.SenderID != "" {
			pubs[i].Info = &ClientInfo{User: msg.SenderID}
		}
	}

	result := HistoryResult{
		Publications: pubs,
	}
	if len(messages) > 0 {
		result.Offset = messages[len(messages)-1].ID
	}

	return h.sendResult(cmd.ID, result)
}

// handlePing processes a ping command
func (h *ClientHandler) handlePing(cmd *Command) error {
	h.mu.Lock()
	h.lastActivity = time.Now()
	h.mu.Unlock()

	// Send pong
	push := NewPongPush()
	return h.sendPush(push)
}

// --- Helper methods ---

func (h *ClientHandler) sendResult(id uint64, result interface{}) error {
	reply, err := SuccessReply(id, result)
	if err != nil {
		return err
	}
	return h.sendReply(reply)
}

func (h *ClientHandler) sendError(id uint64, code int, message string) error {
	reply := ErrorReply(id, code, message)
	return h.sendReply(reply)
}

func (h *ClientHandler) sendReply(reply *Reply) error {
	data, err := EncodeReply(reply)
	if err != nil {
		return err
	}
	return h.transport.Write(data)
}

func (h *ClientHandler) sendPush(push *Push) error {
	data, err := EncodePush(push)
	if err != nil {
		return err
	}
	return h.transport.Write(data)
}

func (h *ClientHandler) startPingTimer() {
	if h.pingInterval <= 0 {
		return
	}

	h.pingTimer = time.AfterFunc(h.pingInterval, func() {
		h.mu.RLock()
		lastActivity := h.lastActivity
		h.mu.RUnlock()

		if time.Since(lastActivity) > h.pingInterval+h.pongTimeout {
			// Client timed out
			h.Close()
			return
		}

		// Reschedule
		h.pingTimer.Reset(h.pingInterval)
	})
}

func (h *ClientHandler) broadcastJoin(channel string) {
	room, exists := h.relay.GetRoom(channel)
	if !exists {
		return
	}

	info := &ClientInfo{
		User:   h.userID,
		Client: h.clientID,
	}

	push, err := NewJoinPush(channel, info)
	if err != nil {
		return
	}

	data, err := EncodePush(push)
	if err != nil {
		return
	}

	for _, sub := range room.GetSubscribers() {
		if sub.ID() != h.userID {
			if handler, ok := sub.(*ClientHandler); ok {
				handler.transport.Write(data)
			}
		}
	}
}

func (h *ClientHandler) broadcastLeave(channel string) {
	room, exists := h.relay.GetRoom(channel)
	if !exists {
		return
	}

	info := &ClientInfo{
		User:   h.userID,
		Client: h.clientID,
	}

	push, err := NewLeavePush(channel, info)
	if err != nil {
		return
	}

	data, err := EncodePush(push)
	if err != nil {
		return
	}

	for _, sub := range room.GetSubscribers() {
		if handler, ok := sub.(*ClientHandler); ok {
			handler.transport.Write(data)
		}
	}
}

func (h *ClientHandler) extractUserFromToken(token string) string {
	// TODO: Implement JWT parsing or custom token validation
	// For now, just return the token as user ID for testing
	if token == "" {
		return "anonymous"
	}
	return token
}

// ErrClosed indicates the handler is closed
var ErrClosed = fmt.Errorf("client handler closed")
