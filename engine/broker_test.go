package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestGenerateNodeID(t *testing.T) {
	id1 := generateNodeID()
	id2 := generateNodeID()

	if id1 == "" {
		t.Error("generateNodeID() returned empty string")
	}

	// Should be unique
	if id1 == id2 {
		t.Error("generateNodeID() should return unique IDs")
	}
}

func TestNodeMessage(t *testing.T) {
	msg := NodeMessage{
		NodeID:    "node-1",
		RoomID:    "chat:general",
		MessageID: "1234-0",
		SenderID:  "user1",
		Type:      "text",
		Content:   []byte("hello"),
		Metadata:  map[string]string{"key": "value"},
		Timestamp: time.Now().UnixMilli(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded NodeMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.NodeID != msg.NodeID {
		t.Errorf("NodeID = %q, want %q", decoded.NodeID, msg.NodeID)
	}
	if decoded.RoomID != msg.RoomID {
		t.Errorf("RoomID = %q, want %q", decoded.RoomID, msg.RoomID)
	}
}

func TestBrokerMessage(t *testing.T) {
	msg := BrokerMessage{
		Channel:   "sr:pub:chat:general",
		Data:      []byte(`{"text":"hello"}`),
		NodeID:    "node-1",
		MessageID: "1234-0",
	}

	if msg.Channel != "sr:pub:chat:general" {
		t.Errorf("Channel = %q, want %q", msg.Channel, "sr:pub:chat:general")
	}
	if msg.NodeID != "node-1" {
		t.Errorf("NodeID = %q, want %q", msg.NodeID, "node-1")
	}
	if msg.MessageID != "1234-0" {
		t.Errorf("MessageID = %q, want %q", msg.MessageID, "1234-0")
	}
}

func TestDedupeCache(t *testing.T) {
	cache := NewDedupeCache(100, time.Minute)

	t.Run("first message is not duplicate", func(t *testing.T) {
		if cache.IsDuplicate("msg-1") {
			t.Error("first message should not be duplicate")
		}
	})

	t.Run("second same message is duplicate", func(t *testing.T) {
		if !cache.IsDuplicate("msg-1") {
			t.Error("second same message should be duplicate")
		}
	})

	t.Run("different message is not duplicate", func(t *testing.T) {
		if cache.IsDuplicate("msg-2") {
			t.Error("different message should not be duplicate")
		}
	})
}

func TestDedupeCacheExpiry(t *testing.T) {
	cache := NewDedupeCache(100, 50*time.Millisecond)

	// Add message
	cache.IsDuplicate("msg-1")

	// Should be duplicate immediately
	if !cache.IsDuplicate("msg-1") {
		t.Error("should be duplicate before expiry")
	}

	// Wait for expiry
	time.Sleep(100 * time.Millisecond)

	// Should NOT be duplicate after expiry
	if cache.IsDuplicate("msg-1") {
		t.Error("should not be duplicate after expiry")
	}
}

func TestDedupeCacheMaxSize(t *testing.T) {
	cache := NewDedupeCache(5, time.Minute)

	// Add 10 messages
	for i := 0; i < 10; i++ {
		cache.IsDuplicate("msg-" + string(rune('0'+i)))
	}

	// First messages should be evicted
	if cache.IsDuplicate("msg-0") {
		t.Error("msg-0 should have been evicted")
	}
	if cache.IsDuplicate("msg-1") {
		t.Error("msg-1 should have been evicted")
	}

	// Last messages should still be tracked
	if !cache.IsDuplicate("msg-9") {
		t.Error("msg-9 should still be tracked")
	}
}

func TestDedupeCacheCleanup(t *testing.T) {
	cache := NewDedupeCache(100, 10*time.Millisecond)

	// Add entries
	cache.IsDuplicate("msg-1")
	cache.IsDuplicate("msg-2")

	// Wait for expiry
	time.Sleep(50 * time.Millisecond)

	// Cleanup
	cache.Cleanup()

	// Should not be duplicates after cleanup
	if cache.IsDuplicate("msg-1") {
		t.Error("msg-1 should not be duplicate after cleanup")
	}
}

func TestRedisBrokerConfigDefaults(t *testing.T) {
	// Test with nil client - should error
	_, err := NewRedisBroker(RedisBrokerConfig{})
	if err == nil {
		t.Error("expected error with nil client")
	}
}

func TestClusterConfigDefaults(t *testing.T) {
	cfg := ClusterConfig{
		Config:       DefaultConfig(),
		NodeID:       "",
		BrokerPrefix: "",
	}

	if cfg.Config.Prefix != "sr:" {
		t.Errorf("Prefix = %q, want %q", cfg.Config.Prefix, "sr:")
	}
}

func TestNodeMessageConversion(t *testing.T) {
	now := time.Now()
	nodeMsg := NodeMessage{
		NodeID:    "node-1",
		RoomID:    "chat",
		MessageID: "123-0",
		SenderID:  "user1",
		Type:      "text",
		Content:   []byte("hello"),
		Timestamp: now.UnixMilli(),
	}

	// Simulate conversion as done in handleBrokerMessage
	msg := &Message{
		ID:        nodeMsg.MessageID,
		RoomID:    nodeMsg.RoomID,
		SenderID:  nodeMsg.SenderID,
		Type:      MessageType(nodeMsg.Type),
		Content:   nodeMsg.Content,
		Metadata:  nodeMsg.Metadata,
		Timestamp: time.UnixMilli(nodeMsg.Timestamp),
	}

	if msg.ID != "123-0" {
		t.Errorf("ID = %q, want %q", msg.ID, "123-0")
	}
	if msg.Type != MessageTypeText {
		t.Errorf("Type = %q, want %q", msg.Type, MessageTypeText)
	}
}

func TestBrokerInterface(t *testing.T) {
	// Verify RedisBroker implements Broker interface
	var _ Broker = (*RedisBroker)(nil)
}

func TestBrokerPublishFormat(t *testing.T) {
	// Test the wrapped message format
	wrapped := struct {
		NodeID string `json:"n"`
		Data   []byte `json:"d"`
	}{
		NodeID: "node-1",
		Data:   []byte(`{"test":"data"}`),
	}

	payload, err := json.Marshal(wrapped)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Verify it can be unmarshaled
	var decoded struct {
		NodeID string `json:"n"`
		Data   []byte `json:"d"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.NodeID != "node-1" {
		t.Errorf("NodeID = %q, want %q", decoded.NodeID, "node-1")
	}
}

// MockBroker for testing without Redis
type MockBroker struct {
	published []BrokerMessage
	msgChan   chan BrokerMessage
	dedupe    *DedupeCache
}

func NewMockBroker() *MockBroker {
	return &MockBroker{
		published: make([]BrokerMessage, 0),
		msgChan:   make(chan BrokerMessage, 100),
		dedupe:    NewDedupeCache(1000, 5*time.Minute),
	}
}

func (b *MockBroker) Publish(ctx context.Context, channel string, data []byte) error {
	b.published = append(b.published, BrokerMessage{
		Channel: channel,
		Data:    data,
		NodeID:  "mock",
	})
	return nil
}

func (b *MockBroker) Subscribe(ctx context.Context, pattern string) (<-chan BrokerMessage, error) {
	return b.msgChan, nil
}

func (b *MockBroker) Close() error {
	close(b.msgChan)
	return nil
}

func (b *MockBroker) PublishedCount() int {
	return len(b.published)
}

func (b *MockBroker) SimulateMessage(msg BrokerMessage) {
	// Check dedup
	if b.dedupe.IsDuplicate(msg.MessageID) {
		return
	}
	b.msgChan <- msg
}

func TestMockBroker(t *testing.T) {
	broker := NewMockBroker()
	defer broker.Close()

	// Test publish
	ctx := context.Background()
	broker.Publish(ctx, "test", []byte("data"))

	if broker.PublishedCount() != 1 {
		t.Errorf("PublishedCount() = %d, want 1", broker.PublishedCount())
	}

	// Verify Broker interface
	var _ Broker = broker
}

func TestMockBrokerDedup(t *testing.T) {
	broker := NewMockBroker()
	defer broker.Close()

	// Subscribe
	msgChan, _ := broker.Subscribe(context.Background(), "*")

	// Send same message twice
	msg := BrokerMessage{
		Channel:   "test",
		Data:      []byte("data"),
		NodeID:    "node-1",
		MessageID: "msg-1",
	}

	go func() {
		broker.SimulateMessage(msg)
		broker.SimulateMessage(msg) // Duplicate!
	}()

	// Should only receive one
	received := 0
	timeout := time.After(100 * time.Millisecond)

	for {
		select {
		case <-msgChan:
			received++
		case <-timeout:
			if received != 1 {
				t.Errorf("received %d messages, want 1 (dedup should work)", received)
			}
			return
		}
	}
}
