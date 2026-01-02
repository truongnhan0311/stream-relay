package engine

import (
	"encoding/json"
	"testing"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		cmdType CommandType
		channel string
		wantErr bool
	}{
		{
			name:    "connect command",
			input:   `{"id":1,"type":"connect"}`,
			cmdType: CmdConnect,
			channel: "",
			wantErr: false,
		},
		{
			name:    "subscribe command",
			input:   `{"id":2,"type":"subscribe","channel":"chat:general"}`,
			cmdType: CmdSubscribe,
			channel: "chat:general",
			wantErr: false,
		},
		{
			name:    "publish command",
			input:   `{"id":3,"type":"publish","channel":"chat:general","data":{"data":"hello"}}`,
			cmdType: CmdPublish,
			channel: "chat:general",
			wantErr: false,
		},
		{
			name:    "ping command",
			input:   `{"type":"ping"}`,
			cmdType: CmdPing,
			channel: "",
			wantErr: false,
		},
		{
			name:    "invalid json",
			input:   `{invalid}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := ParseCommand([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cmd.Type != tt.cmdType {
				t.Errorf("Type = %q, want %q", cmd.Type, tt.cmdType)
			}
			if cmd.Channel != tt.channel {
				t.Errorf("Channel = %q, want %q", cmd.Channel, tt.channel)
			}
		})
	}
}

func TestEncodeReply(t *testing.T) {
	t.Run("success reply", func(t *testing.T) {
		reply, err := SuccessReply(1, ConnectResult{
			Client:  "client123",
			Version: "1.0",
		})
		if err != nil {
			t.Fatalf("SuccessReply() error = %v", err)
		}

		data, err := EncodeReply(reply)
		if err != nil {
			t.Fatalf("EncodeReply() error = %v", err)
		}

		// Verify it's valid JSON
		var decoded Reply
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("failed to decode reply: %v", err)
		}

		if decoded.ID != 1 {
			t.Errorf("ID = %d, want 1", decoded.ID)
		}
		if decoded.Error != nil {
			t.Errorf("Error = %v, want nil", decoded.Error)
		}
	})

	t.Run("error reply", func(t *testing.T) {
		reply := ErrorReply(2, ErrCodeUnauthorized, "not authorized")

		data, err := EncodeReply(reply)
		if err != nil {
			t.Fatalf("EncodeReply() error = %v", err)
		}

		var decoded Reply
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("failed to decode reply: %v", err)
		}

		if decoded.ID != 2 {
			t.Errorf("ID = %d, want 2", decoded.ID)
		}
		if decoded.Error == nil {
			t.Fatal("Error = nil, want error")
		}
		if decoded.Error.Code != ErrCodeUnauthorized {
			t.Errorf("Error.Code = %d, want %d", decoded.Error.Code, ErrCodeUnauthorized)
		}
	})
}

func TestEncodePush(t *testing.T) {
	t.Run("message push", func(t *testing.T) {
		pub := &Publication{
			Offset: "1234-0",
			Data:   json.RawMessage(`{"text":"hello"}`),
		}
		push, err := NewPublicationPush("chat:general", pub)
		if err != nil {
			t.Fatalf("NewPublicationPush() error = %v", err)
		}

		data, err := EncodePush(push)
		if err != nil {
			t.Fatalf("EncodePush() error = %v", err)
		}

		var decoded Push
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("failed to decode push: %v", err)
		}

		if decoded.Type != PushMessage {
			t.Errorf("Type = %q, want %q", decoded.Type, PushMessage)
		}
		if decoded.Channel != "chat:general" {
			t.Errorf("Channel = %q, want %q", decoded.Channel, "chat:general")
		}
	})

	t.Run("join push", func(t *testing.T) {
		info := &ClientInfo{
			User:   "user123",
			Client: "client456",
		}
		push, err := NewJoinPush("chat:general", info)
		if err != nil {
			t.Fatalf("NewJoinPush() error = %v", err)
		}

		if push.Type != PushJoin {
			t.Errorf("Type = %q, want %q", push.Type, PushJoin)
		}
	})

	t.Run("leave push", func(t *testing.T) {
		info := &ClientInfo{
			User:   "user123",
			Client: "client456",
		}
		push, err := NewLeavePush("chat:general", info)
		if err != nil {
			t.Fatalf("NewLeavePush() error = %v", err)
		}

		if push.Type != PushLeave {
			t.Errorf("Type = %q, want %q", push.Type, PushLeave)
		}
	})

	t.Run("pong push", func(t *testing.T) {
		push := NewPongPush()
		if push.Type != PushPong {
			t.Errorf("Type = %q, want %q", push.Type, PushPong)
		}
	})

	t.Run("disconnect push", func(t *testing.T) {
		push, err := NewDisconnectPush(1000, "server shutdown", true)
		if err != nil {
			t.Fatalf("NewDisconnectPush() error = %v", err)
		}

		if push.Type != PushDisconnect {
			t.Errorf("Type = %q, want %q", push.Type, PushDisconnect)
		}

		var disc DisconnectPush
		if err := json.Unmarshal(push.Data, &disc); err != nil {
			t.Fatalf("failed to decode disconnect: %v", err)
		}
		if disc.Code != 1000 {
			t.Errorf("Code = %d, want 1000", disc.Code)
		}
		if !disc.Reconnect {
			t.Error("Reconnect = false, want true")
		}
	})
}

func TestConnectRequest(t *testing.T) {
	input := `{"id":1,"type":"connect","data":{"token":"mytoken","name":"web"}}`
	cmd, err := ParseCommand([]byte(input))
	if err != nil {
		t.Fatalf("ParseCommand() error = %v", err)
	}

	var req ConnectRequest
	if err := json.Unmarshal(cmd.Data, &req); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if req.Token != "mytoken" {
		t.Errorf("Token = %q, want %q", req.Token, "mytoken")
	}
	if req.Name != "web" {
		t.Errorf("Name = %q, want %q", req.Name, "web")
	}
}

func TestSubscribeRequest(t *testing.T) {
	input := `{"id":2,"type":"subscribe","channel":"chat:general","data":{"recover":true,"offset":"1234-0"}}`
	cmd, err := ParseCommand([]byte(input))
	if err != nil {
		t.Fatalf("ParseCommand() error = %v", err)
	}

	var req SubscribeRequest
	if err := json.Unmarshal(cmd.Data, &req); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if !req.Recover {
		t.Error("Recover = false, want true")
	}
	if req.Offset != "1234-0" {
		t.Errorf("Offset = %q, want %q", req.Offset, "1234-0")
	}
}

func TestPublishRequest(t *testing.T) {
	input := `{"id":3,"type":"publish","channel":"chat:general","data":{"data":{"text":"hello"}}}`
	cmd, err := ParseCommand([]byte(input))
	if err != nil {
		t.Fatalf("ParseCommand() error = %v", err)
	}

	var req PublishRequest
	if err := json.Unmarshal(cmd.Data, &req); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if string(req.Data) != `{"text":"hello"}` {
		t.Errorf("Data = %s, want %s", string(req.Data), `{"text":"hello"}`)
	}
}

func TestHistoryRequest(t *testing.T) {
	input := `{"id":4,"type":"history","channel":"chat:general","data":{"limit":50,"since":"1000-0"}}`
	cmd, err := ParseCommand([]byte(input))
	if err != nil {
		t.Fatalf("ParseCommand() error = %v", err)
	}

	var req HistoryRequest
	if err := json.Unmarshal(cmd.Data, &req); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if req.Limit != 50 {
		t.Errorf("Limit = %d, want 50", req.Limit)
	}
	if req.Since != "1000-0" {
		t.Errorf("Since = %q, want %q", req.Since, "1000-0")
	}
}

func TestErrorCodes(t *testing.T) {
	codes := []int{
		ErrCodeBadRequest,
		ErrCodeUnauthorized,
		ErrCodePermissionDenied,
		ErrCodeNotFound,
		ErrCodeAlreadyExists,
		ErrCodeInternal,
		ErrCodeTimeout,
	}

	// Just verify they are distinct
	seen := make(map[int]bool)
	for _, code := range codes {
		if seen[code] {
			t.Errorf("duplicate error code: %d", code)
		}
		seen[code] = true
	}
}

func TestTimestamp(t *testing.T) {
	ts := Timestamp()
	if ts <= 0 {
		t.Errorf("Timestamp() = %d, want positive", ts)
	}
}
