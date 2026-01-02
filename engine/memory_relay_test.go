package engine

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestMemoryRelayCreate(t *testing.T) {
	relay := NewMemoryRelay(DefaultMemoryConfig())
	defer relay.Close()

	if relay == nil {
		t.Fatal("NewMemoryRelay returned nil")
	}
}

func TestMemoryRelayCreateRoom(t *testing.T) {
	relay := NewMemoryRelay(DefaultMemoryConfig())
	defer relay.Close()

	room, err := relay.CreateRoom(RoomConfig{
		ID:   "test-room",
		Type: RoomTypeGroup,
	})

	if err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}
	if room.ID() != "test-room" {
		t.Errorf("room.ID() = %q, want %q", room.ID(), "test-room")
	}
}

func TestMemoryRelayDuplicateRoom(t *testing.T) {
	relay := NewMemoryRelay(DefaultMemoryConfig())
	defer relay.Close()

	relay.CreateRoom(RoomConfig{ID: "test"})
	_, err := relay.CreateRoom(RoomConfig{ID: "test"})

	if err == nil {
		t.Error("expected error for duplicate room")
	}
}

func TestMemoryRelayPublishAndBroadcast(t *testing.T) {
	relay := NewMemoryRelay(DefaultMemoryConfig())
	defer relay.Close()

	room, _ := relay.CreateRoom(RoomConfig{
		ID:   "test-room",
		Type: RoomTypeGroup,
	})

	sub1 := NewMockSubscriber("user1")
	sub2 := NewMockSubscriber("user2")

	room.Subscribe(sub1, nil)
	room.Subscribe(sub2, nil)

	ctx := context.Background()
	msg := &Message{
		SenderID: "user1",
		Type:     MessageTypeText,
		Content:  []byte("hello"),
	}

	results, err := relay.Publish(ctx, "test-room", msg, &PublishOptions{SkipSender: true})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	// Wait for delivery
	time.Sleep(100 * time.Millisecond)

	// user1 is sender, should be skipped
	if sub1.ReceivedCount() != 0 {
		t.Errorf("sender received %d messages, want 0", sub1.ReceivedCount())
	}

	// user2 should receive
	if sub2.ReceivedCount() != 1 {
		t.Errorf("user2 received %d messages, want 1", sub2.ReceivedCount())
	}

	// Check results
	if len(results) < 1 {
		t.Error("expected at least 1 delivery result")
	}
}

func TestMemoryRelayHistory(t *testing.T) {
	relay := NewMemoryRelay(MemoryConfig{DefaultHistorySize: 10})
	defer relay.Close()

	relay.CreateRoom(RoomConfig{ID: "test"})

	ctx := context.Background()

	// Publish 5 messages
	for i := 0; i < 5; i++ {
		relay.Publish(ctx, "test", &Message{
			SenderID: "user1",
			Content:  []byte("msg"),
		}, nil)
	}

	// Recover messages
	messages, err := relay.RecoverMessages(ctx, "test", "user1", nil)
	if err != nil {
		t.Fatalf("RecoverMessages() error = %v", err)
	}

	if len(messages) != 5 {
		t.Errorf("recovered %d messages, want 5", len(messages))
	}
}

func TestMemoryRelayHistoryLimit(t *testing.T) {
	relay := NewMemoryRelay(MemoryConfig{DefaultHistorySize: 5})
	defer relay.Close()

	relay.CreateRoom(RoomConfig{ID: "test"})

	ctx := context.Background()

	// Publish 10 messages (exceeds buffer size of 5)
	for i := 0; i < 10; i++ {
		relay.Publish(ctx, "test", &Message{
			SenderID: "user1",
			Content:  []byte("msg"),
		}, nil)
	}

	messages, _ := relay.RecoverMessages(ctx, "test", "user1", nil)

	// Should only have last 5
	if len(messages) != 5 {
		t.Errorf("recovered %d messages, want 5 (buffer limit)", len(messages))
	}
}

func TestMemoryRelayStats(t *testing.T) {
	relay := NewMemoryRelay(DefaultMemoryConfig())
	defer relay.Close()

	room, _ := relay.CreateRoom(RoomConfig{ID: "test"})
	sub := NewMockSubscriber("user1")
	room.Subscribe(sub, nil)

	ctx := context.Background()
	relay.Publish(ctx, "test", &Message{Content: []byte("hi")}, nil)
	relay.Publish(ctx, "test", &Message{Content: []byte("hello")}, nil)

	time.Sleep(50 * time.Millisecond)

	stats := relay.Stats()

	if stats.ActiveRooms != 1 {
		t.Errorf("ActiveRooms = %d, want 1", stats.ActiveRooms)
	}
	if stats.ActiveSubscribers != 1 {
		t.Errorf("ActiveSubscribers = %d, want 1", stats.ActiveSubscribers)
	}
	if stats.TotalMessages != 2 {
		t.Errorf("TotalMessages = %d, want 2", stats.TotalMessages)
	}
}

func TestMemoryRelayDirectMessage(t *testing.T) {
	relay := NewMemoryRelay(DefaultMemoryConfig())
	defer relay.Close()

	ctx := context.Background()
	msg := &Message{
		Type:    MessageTypeText,
		Content: []byte("hey"),
	}

	_, err := relay.PublishDirect(ctx, "alice", "bob", msg, nil)
	if err != nil {
		t.Fatalf("PublishDirect() error = %v", err)
	}

	// Room should be created
	roomID := GenerateDMRoomID("alice", "bob")
	_, exists := relay.GetRoom(roomID)
	if !exists {
		t.Error("DM room should be created")
	}
}

func TestRingBuffer(t *testing.T) {
	rb := NewRingBuffer(3)

	rb.Add(&Message{ID: "1"})
	rb.Add(&Message{ID: "2"})
	rb.Add(&Message{ID: "3"})

	all := rb.GetAll()
	if len(all) != 3 {
		t.Errorf("len(GetAll()) = %d, want 3", len(all))
	}

	// Add one more (should overwrite oldest)
	rb.Add(&Message{ID: "4"})

	all = rb.GetAll()
	if len(all) != 3 {
		t.Errorf("after overflow, len(GetAll()) = %d, want 3", len(all))
	}

	// First should now be "2"
	if all[0].ID != "2" {
		t.Errorf("all[0].ID = %q, want %q", all[0].ID, "2")
	}
}

func TestRingBufferGetSince(t *testing.T) {
	rb := NewRingBuffer(10)

	rb.Add(&Message{ID: "1"})
	rb.Add(&Message{ID: "2"})
	rb.Add(&Message{ID: "3"})
	rb.Add(&Message{ID: "4"})

	msgs := rb.GetSince("2")
	if len(msgs) != 2 {
		t.Errorf("GetSince('2') = %d messages, want 2", len(msgs))
	}
	if msgs[0].ID != "3" {
		t.Errorf("first message after '2' should be '3', got %q", msgs[0].ID)
	}
}

func TestRingBufferGetLast(t *testing.T) {
	rb := NewRingBuffer(10)

	for i := 1; i <= 5; i++ {
		rb.Add(&Message{ID: string(rune('0' + i))})
	}

	last2 := rb.GetLast(2)
	if len(last2) != 2 {
		t.Errorf("GetLast(2) = %d messages, want 2", len(last2))
	}
}

func TestMemoryRelayConcurrent(t *testing.T) {
	relay := NewMemoryRelay(DefaultMemoryConfig())
	defer relay.Close()

	relay.CreateRoom(RoomConfig{ID: "test"})

	// Add some subscribers
	for i := 0; i < 10; i++ {
		sub := NewMockSubscriber("user" + string(rune('0'+i)))
		relay.Subscribe("test", sub, nil)
	}

	ctx := context.Background()
	var wg sync.WaitGroup

	// Concurrent publishes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			relay.Publish(ctx, "test", &Message{
				SenderID: "sender",
				Content:  []byte("message"),
			}, nil)
		}(i)
	}

	wg.Wait()

	stats := relay.Stats()
	if stats.TotalMessages != 100 {
		t.Errorf("TotalMessages = %d, want 100", stats.TotalMessages)
	}
}

func TestParseMessageID(t *testing.T) {
	ts, seq, err := ParseMessageID("1704153600000-42")
	if err != nil {
		t.Fatalf("ParseMessageID() error = %v", err)
	}

	if seq != 42 {
		t.Errorf("seq = %d, want 42", seq)
	}

	if ts.UnixMilli() != 1704153600000 {
		t.Errorf("ts = %d, want 1704153600000", ts.UnixMilli())
	}
}

func TestCompareMessageIDs(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1000-0", "2000-0", -1},
		{"2000-0", "1000-0", 1},
		{"1000-0", "1000-0", 0},
		{"1000-1", "1000-2", -1},
		{"1000-2", "1000-1", 1},
	}

	for _, tt := range tests {
		got := CompareMessageIDs(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("CompareMessageIDs(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestMemoryRelayRecoverAndDeliver(t *testing.T) {
	relay := NewMemoryRelay(DefaultMemoryConfig())
	defer relay.Close()

	relay.CreateRoom(RoomConfig{ID: "test"})

	ctx := context.Background()

	// Publish some messages
	for i := 0; i < 3; i++ {
		relay.Publish(ctx, "test", &Message{
			SenderID: "user1",
			Content:  []byte("msg"),
		}, nil)
	}

	// Create a new subscriber and recover
	sub := NewMockSubscriber("newuser")
	err := relay.RecoverAndDeliver(ctx, "test", sub, nil)
	if err != nil {
		t.Fatalf("RecoverAndDeliver() error = %v", err)
	}

	if sub.ReceivedCount() != 3 {
		t.Errorf("received %d messages, want 3", sub.ReceivedCount())
	}
}

func TestMemoryRelayGetHistoryWithFallback(t *testing.T) {
	relay := NewMemoryRelay(DefaultMemoryConfig())
	defer relay.Close()

	relay.CreateRoom(RoomConfig{ID: "test"})

	ctx := context.Background()

	// Add 2 messages to memory
	relay.Publish(ctx, "test", &Message{ID: "mem-1", Content: []byte("1")}, nil)
	relay.Publish(ctx, "test", &Message{ID: "mem-2", Content: []byte("2")}, nil)

	// Fallback loader returns 3 more
	fallback := func(roomID string, limit int) ([]*Message, error) {
		return []*Message{
			{ID: "ext-1", Content: []byte("old1")},
			{ID: "ext-2", Content: []byte("old2")},
			{ID: "ext-3", Content: []byte("old3")},
		}, nil
	}

	messages, err := relay.GetHistoryWithFallback(ctx, "test", 5, fallback)
	if err != nil {
		t.Fatalf("GetHistoryWithFallback() error = %v", err)
	}

	// Should have external + memory messages
	if len(messages) != 5 {
		t.Errorf("got %d messages, want 5", len(messages))
	}
}
