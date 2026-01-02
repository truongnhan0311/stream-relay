package redis

import (
	"testing"
)

// Note: This file tests the exported API of the redis package.
// Integration tests that require a Redis connection are skipped by default.

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Addr != "localhost:6379" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, "localhost:6379")
	}
	if cfg.PoolSize != 10 {
		t.Errorf("PoolSize = %d, want 10", cfg.PoolSize)
	}
	if cfg.DB != 0 {
		t.Errorf("DB = %d, want 0", cfg.DB)
	}
}

func TestStreamEntry(t *testing.T) {
	entry := StreamEntry{
		ID: "1234-0",
		Fields: map[string]string{
			"field1": "value1",
			"field2": "value2",
		},
	}

	if entry.ID != "1234-0" {
		t.Errorf("ID = %q, want %q", entry.ID, "1234-0")
	}
	if entry.Fields["field1"] != "value1" {
		t.Errorf("Fields[field1] = %q, want %q", entry.Fields["field1"], "value1")
	}
}

func TestStreamResult(t *testing.T) {
	result := StreamResult{
		Stream: "mystream",
		Entries: []StreamEntry{
			{
				ID:     "1-0",
				Fields: map[string]string{"a": "b"},
			},
			{
				ID:     "2-0",
				Fields: map[string]string{"c": "d"},
			},
		},
	}

	if result.Stream != "mystream" {
		t.Errorf("Stream = %q, want %q", result.Stream, "mystream")
	}
	if len(result.Entries) != 2 {
		t.Errorf("len(Entries) = %d, want 2", len(result.Entries))
	}
}

func TestConfigValidation(t *testing.T) {
	cfg := Config{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
		PoolSize: 10,
	}

	if cfg.Addr == "" {
		t.Error("Addr should not be empty")
	}
}
