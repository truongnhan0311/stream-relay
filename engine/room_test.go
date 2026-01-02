package engine

import (
	"context"
	"sync"
	"testing"
	"time"
)

// MockSubscriber implements Subscriber for testing
type MockSubscriber struct {
	id        string
	messages  []*Message
	mu        sync.Mutex
	closed    bool
	onReceive func(msg *Message) error
}

func NewMockSubscriber(id string) *MockSubscriber {
	return &MockSubscriber{
		id:       id,
		messages: make([]*Message, 0),
	}
}

func (s *MockSubscriber) ID() string {
	return s.id
}

func (s *MockSubscriber) Receive(ctx context.Context, msg *Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.onReceive != nil {
		return s.onReceive(msg)
	}

	s.messages = append(s.messages, msg)
	return nil
}

func (s *MockSubscriber) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *MockSubscriber) ReceivedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages)
}

func (s *MockSubscriber) GetMessages() []*Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]*Message, len(s.messages))
	copy(result, s.messages)
	return result
}

func (s *MockSubscriber) IsClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func TestRoomSubscribe(t *testing.T) {
	room := newRoom(nil, RoomConfig{
		ID:   "test-room",
		Type: RoomTypeGroup,
	})

	sub := NewMockSubscriber("user1")
	err := room.Subscribe(sub, nil)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	if room.SubscriberCount() != 1 {
		t.Errorf("SubscriberCount() = %d, want 1", room.SubscriberCount())
	}

	if !room.IsSubscribed("user1") {
		t.Error("IsSubscribed() = false, want true")
	}
}

func TestRoomUnsubscribe(t *testing.T) {
	room := newRoom(nil, RoomConfig{
		ID:   "test-room",
		Type: RoomTypeGroup,
	})

	sub := NewMockSubscriber("user1")
	room.Subscribe(sub, nil)

	err := room.Unsubscribe("user1")
	if err != nil {
		t.Fatalf("Unsubscribe() error = %v", err)
	}

	if room.SubscriberCount() != 0 {
		t.Errorf("SubscriberCount() = %d, want 0", room.SubscriberCount())
	}

	if room.IsSubscribed("user1") {
		t.Error("IsSubscribed() = true, want false")
	}

	if !sub.IsClosed() {
		t.Error("subscriber should be closed")
	}
}

func TestRoomBroadcast(t *testing.T) {
	room := newRoom(nil, RoomConfig{
		ID:   "test-room",
		Type: RoomTypeGroup,
	})

	sub1 := NewMockSubscriber("user1")
	sub2 := NewMockSubscriber("user2")
	sub3 := NewMockSubscriber("user3")

	room.Subscribe(sub1, nil)
	room.Subscribe(sub2, nil)
	room.Subscribe(sub3, nil)

	ctx := context.Background()
	msg := &Message{
		ID:        "1",
		SenderID:  "user1",
		Content:   []byte("hello"),
		Timestamp: time.Now(),
	}

	// Broadcast with SkipSender
	results := room.Broadcast(ctx, msg, &PublishOptions{SkipSender: true})

	// Wait a bit for goroutines
	time.Sleep(100 * time.Millisecond)

	// user1 is sender, should be skipped
	if sub1.ReceivedCount() != 0 {
		t.Errorf("sender received %d messages, want 0", sub1.ReceivedCount())
	}

	// user2 and user3 should receive
	if sub2.ReceivedCount() != 1 {
		t.Errorf("user2 received %d messages, want 1", sub2.ReceivedCount())
	}
	if sub3.ReceivedCount() != 1 {
		t.Errorf("user3 received %d messages, want 1", sub3.ReceivedCount())
	}

	// Check delivery results
	successCount := 0
	for _, r := range results {
		if r.Status == DeliverySuccess {
			successCount++
		}
	}
	if successCount != 2 {
		t.Errorf("successful deliveries = %d, want 2", successCount)
	}
}

func TestRoomBroadcastToAll(t *testing.T) {
	room := newRoom(nil, RoomConfig{
		ID:   "test-room",
		Type: RoomTypeGroup,
	})

	sub1 := NewMockSubscriber("user1")
	sub2 := NewMockSubscriber("user2")

	room.Subscribe(sub1, nil)
	room.Subscribe(sub2, nil)

	ctx := context.Background()
	msg := &Message{
		ID:        "1",
		SenderID:  "user1",
		Content:   []byte("hello"),
		Timestamp: time.Now(),
	}

	// Broadcast without SkipSender
	room.Broadcast(ctx, msg, &PublishOptions{SkipSender: false})

	// Wait for goroutines
	time.Sleep(100 * time.Millisecond)

	// All should receive including sender
	if sub1.ReceivedCount() != 1 {
		t.Errorf("user1 received %d messages, want 1", sub1.ReceivedCount())
	}
	if sub2.ReceivedCount() != 1 {
		t.Errorf("user2 received %d messages, want 1", sub2.ReceivedCount())
	}
}

func TestRoomGetPresence(t *testing.T) {
	room := newRoom(nil, RoomConfig{
		ID:   "test-room",
		Type: RoomTypeGroup,
	})

	sub1 := NewMockSubscriber("user1")
	sub2 := NewMockSubscriber("user2")

	room.Subscribe(sub1, &PresenceInfo{
		UserID:      "user1",
		ClientID:    "client1",
		ConnectedAt: time.Now(),
	})
	room.Subscribe(sub2, &PresenceInfo{
		UserID:      "user2",
		ClientID:    "client2",
		ConnectedAt: time.Now(),
	})

	presence := room.GetPresence()
	if len(presence) != 2 {
		t.Errorf("len(GetPresence()) = %d, want 2", len(presence))
	}
}

func TestRoomMembers(t *testing.T) {
	room := newRoom(nil, RoomConfig{
		ID:      "test-room",
		Type:    RoomTypeGroup,
		Members: []string{"user1", "user2"},
	})

	if !room.IsMember("user1") {
		t.Error("IsMember(user1) = false, want true")
	}
	if !room.IsMember("user2") {
		t.Error("IsMember(user2) = false, want true")
	}
	if room.IsMember("user3") {
		t.Error("IsMember(user3) = true, want false")
	}

	// Add member
	room.AddMember("user3")
	if !room.IsMember("user3") {
		t.Error("after AddMember, IsMember(user3) = false, want true")
	}

	// Remove member
	room.RemoveMember("user2")
	if room.IsMember("user2") {
		t.Error("after RemoveMember, IsMember(user2) = true, want false")
	}
}

func TestRoomClose(t *testing.T) {
	room := newRoom(nil, RoomConfig{
		ID:   "test-room",
		Type: RoomTypeGroup,
	})

	sub1 := NewMockSubscriber("user1")
	sub2 := NewMockSubscriber("user2")

	room.Subscribe(sub1, nil)
	room.Subscribe(sub2, nil)

	room.Close()

	if room.SubscriberCount() != 0 {
		t.Errorf("after Close, SubscriberCount() = %d, want 0", room.SubscriberCount())
	}

	if !sub1.IsClosed() {
		t.Error("sub1 should be closed")
	}
	if !sub2.IsClosed() {
		t.Error("sub2 should be closed")
	}
}

func TestRoomReconnection(t *testing.T) {
	room := newRoom(nil, RoomConfig{
		ID:   "test-room",
		Type: RoomTypeGroup,
	})

	sub1 := NewMockSubscriber("user1")
	room.Subscribe(sub1, nil)

	// Simulate reconnection with new subscriber
	sub2 := NewMockSubscriber("user1")
	room.Subscribe(sub2, nil)

	// Old subscriber should be closed
	if !sub1.IsClosed() {
		t.Error("old subscriber should be closed on reconnection")
	}

	// Only one subscriber should exist
	if room.SubscriberCount() != 1 {
		t.Errorf("SubscriberCount() = %d, want 1", room.SubscriberCount())
	}
}

func TestRoomOfflineMembers(t *testing.T) {
	room := newRoom(nil, RoomConfig{
		ID:      "test-room",
		Type:    RoomTypeGroup,
		Members: []string{"user1", "user2", "user3"},
	})

	// Only user1 is online
	sub1 := NewMockSubscriber("user1")
	room.Subscribe(sub1, nil)

	ctx := context.Background()
	msg := &Message{
		ID:        "1",
		SenderID:  "external",
		Content:   []byte("hello"),
		Timestamp: time.Now(),
	}

	results := room.Broadcast(ctx, msg, nil)

	time.Sleep(100 * time.Millisecond)

	offlineCount := 0
	successCount := 0
	for _, r := range results {
		switch r.Status {
		case DeliveryOffline:
			offlineCount++
		case DeliverySuccess:
			successCount++
		}
	}

	if successCount != 1 {
		t.Errorf("successful deliveries = %d, want 1", successCount)
	}
	if offlineCount != 2 {
		t.Errorf("offline deliveries = %d, want 2", offlineCount)
	}
}
