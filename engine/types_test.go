package engine

import (
	"testing"
	"time"
)

func TestGenerateDMRoomID(t *testing.T) {
	tests := []struct {
		userA    string
		userB    string
		expected string
	}{
		{"alice", "bob", "dm:alice:bob"},
		{"bob", "alice", "dm:alice:bob"}, // Should be consistent
		{"user1", "user2", "dm:user1:user2"},
		{"z", "a", "dm:a:z"}, // Alphabetical order
	}

	for _, tt := range tests {
		got := GenerateDMRoomID(tt.userA, tt.userB)
		if got != tt.expected {
			t.Errorf("GenerateDMRoomID(%q, %q) = %q, want %q", tt.userA, tt.userB, got, tt.expected)
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Prefix != "sr:" {
		t.Errorf("Prefix = %q, want %q", cfg.Prefix, "sr:")
	}
	if cfg.DefaultHistorySize != 1000 {
		t.Errorf("DefaultHistorySize = %d, want 1000", cfg.DefaultHistorySize)
	}
	if cfg.DefaultHistoryTTL != 24*time.Hour {
		t.Errorf("DefaultHistoryTTL = %v, want 24h", cfg.DefaultHistoryTTL)
	}
}

func TestDefaultPublishOptions(t *testing.T) {
	opts := DefaultPublishOptions()

	if !opts.SkipSender {
		t.Error("SkipSender = false, want true")
	}
	if !opts.Persist {
		t.Error("Persist = false, want true")
	}
}

func TestNewRoomConfig(t *testing.T) {
	cfg := NewRoomConfig("test-room",
		WithRoomType(RoomTypeDirect),
		WithRoomMembers("user1", "user2"),
		WithRoomHistorySize(500),
		WithRoomHistoryTTL(2*time.Hour),
	)

	if cfg.ID != "test-room" {
		t.Errorf("ID = %q, want %q", cfg.ID, "test-room")
	}
	if cfg.Type != RoomTypeDirect {
		t.Errorf("Type = %q, want %q", cfg.Type, RoomTypeDirect)
	}
	if len(cfg.Members) != 2 {
		t.Errorf("len(Members) = %d, want 2", len(cfg.Members))
	}
	if cfg.HistorySize != 500 {
		t.Errorf("HistorySize = %d, want 500", cfg.HistorySize)
	}
	if cfg.HistoryTTL != 2*time.Hour {
		t.Errorf("HistoryTTL = %v, want 2h", cfg.HistoryTTL)
	}
}

func TestMessageTypes(t *testing.T) {
	// Just verify the constants are distinct
	types := []MessageType{
		MessageTypeText,
		MessageTypeImage,
		MessageTypeFile,
		MessageTypeSystem,
		MessageTypeJoin,
		MessageTypeLeave,
		MessageTypeTyping,
	}

	seen := make(map[MessageType]bool)
	for _, mt := range types {
		if seen[mt] {
			t.Errorf("duplicate message type: %q", mt)
		}
		seen[mt] = true
	}
}

func TestRoomTypes(t *testing.T) {
	if RoomTypeGroup != "group" {
		t.Errorf("RoomTypeGroup = %q, want %q", RoomTypeGroup, "group")
	}
	if RoomTypeDirect != "direct" {
		t.Errorf("RoomTypeDirect = %q, want %q", RoomTypeDirect, "direct")
	}
}

func TestDeliveryStatus(t *testing.T) {
	// Verify statuses are distinct integers
	statuses := []DeliveryStatus{
		DeliveryPending,
		DeliverySuccess,
		DeliveryFailed,
		DeliveryOffline,
	}

	seen := make(map[DeliveryStatus]bool)
	for _, s := range statuses {
		if seen[s] {
			t.Errorf("duplicate delivery status: %d", s)
		}
		seen[s] = true
	}
}

func TestEventTypes(t *testing.T) {
	types := []EventType{
		EventTypeConnect,
		EventTypeDisconnect,
		EventTypeSubscribe,
		EventTypeUnsubscribe,
		EventTypePublish,
		EventTypeDelivery,
		EventTypeError,
	}

	seen := make(map[EventType]bool)
	for _, et := range types {
		if seen[et] {
			t.Errorf("duplicate event type: %q", et)
		}
		seen[et] = true
	}
}

func TestDeliveryPanicError(t *testing.T) {
	err := &DeliveryPanicError{
		UserID:    "user1",
		Recovered: "something went wrong",
	}

	msg := err.Error()
	if msg != "panic during delivery to user1" {
		t.Errorf("Error() = %q, want %q", msg, "panic during delivery to user1")
	}
}

func TestMessage(t *testing.T) {
	msg := &Message{
		ID:        "1234-0",
		RoomID:    "test-room",
		SenderID:  "user1",
		Type:      MessageTypeText,
		Content:   []byte("hello world"),
		Metadata:  map[string]string{"key": "value"},
		Timestamp: time.Now(),
	}

	if msg.ID != "1234-0" {
		t.Errorf("ID = %q, want %q", msg.ID, "1234-0")
	}
	if string(msg.Content) != "hello world" {
		t.Errorf("Content = %q, want %q", string(msg.Content), "hello world")
	}
	if msg.Metadata["key"] != "value" {
		t.Errorf("Metadata[key] = %q, want %q", msg.Metadata["key"], "value")
	}
}

func TestPresenceInfo(t *testing.T) {
	info := PresenceInfo{
		UserID:      "user1",
		ClientID:    "client123",
		ConnectedAt: time.Now(),
		Data:        map[string]interface{}{"device": "mobile"},
	}

	if info.UserID != "user1" {
		t.Errorf("UserID = %q, want %q", info.UserID, "user1")
	}
	if info.Data["device"] != "mobile" {
		t.Errorf("Data[device] = %v, want %v", info.Data["device"], "mobile")
	}
}
