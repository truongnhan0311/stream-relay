package engine

import (
	"testing"
	"time"
)

func TestNamespaceManager(t *testing.T) {
	mgr := NewNamespaceManager()

	t.Run("Register and get namespace", func(t *testing.T) {
		ns := &Namespace{
			Name:          "chat",
			ChannelPrefix: "chat:",
			Options:       DefaultNamespaceOptions(),
		}
		mgr.Register(ns)

		got, ok := mgr.Get("chat")
		if !ok {
			t.Fatal("namespace not found")
		}
		if got.Name != "chat" {
			t.Errorf("Name = %q, want %q", got.Name, "chat")
		}
	})

	t.Run("Get by channel", func(t *testing.T) {
		ns := &Namespace{
			Name:          "notifications",
			ChannelPrefix: "notify:",
			Options:       DefaultNamespaceOptions(),
		}
		mgr.Register(ns)

		got := mgr.GetByChannel("notify:user123")
		if got.Name != "notifications" {
			t.Errorf("GetByChannel() = %q, want %q", got.Name, "notifications")
		}

		// Unknown channel returns default
		got = mgr.GetByChannel("unknown:channel")
		if got.Name != "default" {
			t.Errorf("GetByChannel() = %q, want %q", got.Name, "default")
		}
	})

	t.Run("Default namespace", func(t *testing.T) {
		def := mgr.Default()
		if def == nil {
			t.Fatal("default namespace is nil")
		}
		if def.Name != "default" {
			t.Errorf("default name = %q, want %q", def.Name, "default")
		}
	})

	t.Run("List namespaces", func(t *testing.T) {
		list := mgr.List()
		if len(list) < 1 {
			t.Error("list should have at least default namespace")
		}
	})

	t.Run("Remove namespace", func(t *testing.T) {
		mgr.Remove("chat")
		_, ok := mgr.Get("chat")
		if ok {
			t.Error("namespace should be removed")
		}
	})
}

func TestNamespaceChannelName(t *testing.T) {
	ns := &Namespace{
		Name:          "chat",
		ChannelPrefix: "chat:",
		Options:       DefaultNamespaceOptions(),
	}

	t.Run("ChannelName adds prefix", func(t *testing.T) {
		got := ns.ChannelName("general")
		if got != "chat:general" {
			t.Errorf("ChannelName() = %q, want %q", got, "chat:general")
		}
	})

	t.Run("StripPrefix removes prefix", func(t *testing.T) {
		got := ns.StripPrefix("chat:general")
		if got != "general" {
			t.Errorf("StripPrefix() = %q, want %q", got, "general")
		}
	})

	t.Run("StripPrefix no-op if no prefix", func(t *testing.T) {
		got := ns.StripPrefix("other:general")
		if got != "other:general" {
			t.Errorf("StripPrefix() = %q, want %q", got, "other:general")
		}
	})
}

func TestNamespaceValidateChannel(t *testing.T) {
	ns := &Namespace{
		Name:          "test",
		ChannelPrefix: "test:",
		Options: NamespaceOptions{
			MaxChannelLength: 10,
		},
	}

	t.Run("Empty channel is invalid", func(t *testing.T) {
		err := ns.ValidateChannel("")
		if err == nil {
			t.Error("empty channel should be invalid")
		}
	})

	t.Run("Valid channel", func(t *testing.T) {
		err := ns.ValidateChannel("general")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("Too long channel", func(t *testing.T) {
		err := ns.ValidateChannel("verylongname")
		if err == nil {
			t.Error("too long channel should be invalid")
		}
	})
}

func TestPresetNamespaces(t *testing.T) {
	t.Run("ChatNamespace", func(t *testing.T) {
		ns := ChatNamespace("chat:")
		if ns.Name != "chat" {
			t.Errorf("Name = %q, want %q", ns.Name, "chat")
		}
		if ns.ChannelPrefix != "chat:" {
			t.Errorf("ChannelPrefix = %q, want %q", ns.ChannelPrefix, "chat:")
		}
		if !ns.Options.Presence {
			t.Error("chat should have presence enabled")
		}
		if !ns.Options.JoinLeave {
			t.Error("chat should have join/leave enabled")
		}
	})

	t.Run("NotificationNamespace", func(t *testing.T) {
		ns := NotificationNamespace("notify:")
		if ns.Name != "notifications" {
			t.Errorf("Name = %q, want %q", ns.Name, "notifications")
		}
		if ns.Options.Publish {
			t.Error("notifications should not allow client publish")
		}
	})

	t.Run("PublicNamespace", func(t *testing.T) {
		ns := PublicNamespace("public:")
		if ns.Name != "public" {
			t.Errorf("Name = %q, want %q", ns.Name, "public")
		}
		if !ns.Options.Anonymous {
			t.Error("public should allow anonymous")
		}
	})

	t.Run("PersonalNamespace", func(t *testing.T) {
		ns := PersonalNamespace("user:")
		if ns.Name != "personal" {
			t.Errorf("Name = %q, want %q", ns.Name, "personal")
		}
		if !ns.Options.Protected {
			t.Error("personal should be protected")
		}
	})
}

func TestDefaultNamespaceOptions(t *testing.T) {
	opts := DefaultNamespaceOptions()

	if opts.HistorySize != 100 {
		t.Errorf("HistorySize = %d, want 100", opts.HistorySize)
	}
	if opts.HistoryTTL != 24*time.Hour {
		t.Errorf("HistoryTTL = %v, want 24h", opts.HistoryTTL)
	}
	if !opts.HistoryRecover {
		t.Error("HistoryRecover should be true")
	}
	if !opts.Presence {
		t.Error("Presence should be true")
	}
	if !opts.Publish {
		t.Error("Publish should be true")
	}
}
