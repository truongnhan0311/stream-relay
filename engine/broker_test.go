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
		Channel: "sr:pub:chat:general",
		Data:    []byte(`{"text":"hello"}`),
		NodeID:  "node-1",
	}

	if msg.Channel != "sr:pub:chat:general" {
		t.Errorf("Channel = %q, want %q", msg.Channel, "sr:pub:chat:general")
	}
	if msg.NodeID != "node-1" {
		t.Errorf("NodeID = %q, want %q", msg.NodeID, "node-1")
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

// Note: Full integration tests for RedisBroker and ClusterRelay require
// a running Redis instance. These are tested in integration tests.

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
}

func NewMockBroker() *MockBroker {
	return &MockBroker{
		published: make([]BrokerMessage, 0),
		msgChan:   make(chan BrokerMessage, 100),
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
