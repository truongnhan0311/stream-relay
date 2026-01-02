package engine

import (
	"testing"
)

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		pattern string
		channel string
		want    bool
	}{
		// Exact match
		{"chat", "chat", true},
		{"chat", "other", false},

		// Wildcard at end
		{"chat:*", "chat:general", true},
		{"chat:*", "chat:private:123", true},
		{"chat:*", "other:general", false},

		// Wildcard in middle
		{"user:*:messages", "user:123:messages", true},
		{"user:*:messages", "user:abc:messages", true},
		{"user:*:messages", "user:123:other", false},

		// Match everything
		{"*", "anything", true},
		{"*", "chat:room:123", true},

		// Multiple wildcards
		{"chat:*:room:*", "chat:abc:room:123", true},
		{"chat:*:room:*", "chat:abc:room:", true},

		// Edge cases
		{"", "", true},
		{"", "something", false},
		{"chat:", "chat:", true},
		{"chat:", "chat:x", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.channel, func(t *testing.T) {
			got := matchPattern(tt.pattern, tt.channel)
			if got != tt.want {
				t.Errorf("matchPattern(%q, %q) = %v, want %v", tt.pattern, tt.channel, got, tt.want)
			}
		})
	}
}

func TestPermission(t *testing.T) {
	t.Run("Has permission", func(t *testing.T) {
		perm := PermSubscribe | PermPublish

		if !perm.Has(PermSubscribe) {
			t.Error("should have PermSubscribe")
		}
		if !perm.Has(PermPublish) {
			t.Error("should have PermPublish")
		}
		if perm.Has(PermPresence) {
			t.Error("should not have PermPresence")
		}
	})

	t.Run("Add permission", func(t *testing.T) {
		perm := PermSubscribe
		perm = perm.Add(PermPublish)

		if !perm.Has(PermSubscribe) {
			t.Error("should have PermSubscribe")
		}
		if !perm.Has(PermPublish) {
			t.Error("should have PermPublish")
		}
	})

	t.Run("Remove permission", func(t *testing.T) {
		perm := PermAll
		perm = perm.Remove(PermPublish)

		if !perm.Has(PermSubscribe) {
			t.Error("should have PermSubscribe")
		}
		if perm.Has(PermPublish) {
			t.Error("should not have PermPublish")
		}
	})

	t.Run("Permission constants", func(t *testing.T) {
		if !PermAll.Has(PermSubscribe) {
			t.Error("PermAll should have PermSubscribe")
		}
		if !PermAll.Has(PermPublish) {
			t.Error("PermAll should have PermPublish")
		}
		if !PermAll.Has(PermPresence) {
			t.Error("PermAll should have PermPresence")
		}

		if !PermReadOnly.Has(PermSubscribe) {
			t.Error("PermReadOnly should have PermSubscribe")
		}
		if PermReadOnly.Has(PermPublish) {
			t.Error("PermReadOnly should not have PermPublish")
		}

		if !PermStandard.Has(PermSubscribe) {
			t.Error("PermStandard should have PermSubscribe")
		}
		if !PermStandard.Has(PermPublish) {
			t.Error("PermStandard should have PermPublish")
		}
	})
}

func TestPermissionChecker(t *testing.T) {
	checker := NewPermissionChecker()

	// Add rules
	checker.AddRule(ChannelRule{
		Pattern:            "public:*",
		DefaultPermissions: PermReadOnly,
		AllowAnonymous:     true,
	})

	checker.AddRule(ChannelRule{
		Pattern:            "chat:*",
		DefaultPermissions: PermStandard,
		RequireAuth:        true,
	})

	checker.AddRule(ChannelRule{
		Pattern:            "admin:*",
		DefaultPermissions: PermNone,
		RequireAuth:        true,
	})

	// Set role permissions
	checker.SetRolePermission("admin", PermAll)
	checker.AssignRole("user_admin", "admin")

	t.Run("Public channel anonymous access", func(t *testing.T) {
		perm, err := checker.CheckSubscribe("", "public:feed", "")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !perm.Has(PermSubscribe) {
			t.Error("should allow subscribe to public channel")
		}
	})

	t.Run("Chat channel requires auth", func(t *testing.T) {
		_, err := checker.CheckSubscribe("", "chat:general", "")
		if err != ErrUnauthorized {
			t.Errorf("expected ErrUnauthorized, got %v", err)
		}
	})

	t.Run("Chat channel authenticated user", func(t *testing.T) {
		perm, err := checker.CheckSubscribe("user123", "chat:general", "")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !perm.Has(PermSubscribe) || !perm.Has(PermPublish) {
			t.Error("authenticated user should have standard permissions")
		}
	})

	t.Run("Admin channel requires role", func(t *testing.T) {
		// Regular user
		_, err := checker.CheckSubscribe("user123", "admin:dashboard", "")
		if err != ErrPermissionDenied {
			t.Errorf("expected ErrPermissionDenied, got %v", err)
		}

		// Admin user
		perm, err := checker.CheckSubscribe("user_admin", "admin:dashboard", "")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !perm.Has(PermAll) {
			t.Error("admin should have all permissions")
		}
	})

	t.Run("User-specific permissions", func(t *testing.T) {
		checker.SetUserPermission("special_user", "chat:general", PermAll)

		perm := checker.GetUserPermissions("special_user", "chat:general")
		if !perm.Has(PermAll) {
			t.Error("special_user should have all permissions on chat:general")
		}
	})
}

func TestCheckPublish(t *testing.T) {
	checker := NewPermissionChecker()

	checker.AddRule(ChannelRule{
		Pattern:            "readonly:*",
		DefaultPermissions: PermReadOnly,
	})

	checker.AddRule(ChannelRule{
		Pattern:            "chat:*",
		DefaultPermissions: PermStandard,
	})

	t.Run("Cannot publish to readonly", func(t *testing.T) {
		ok, err := checker.CheckPublish("user1", "readonly:channel")
		if ok || err == nil {
			t.Error("should not allow publish to readonly channel")
		}
	})

	t.Run("Can publish to chat", func(t *testing.T) {
		ok, err := checker.CheckPublish("user1", "chat:general")
		if !ok || err != nil {
			t.Errorf("should allow publish to chat channel, got ok=%v err=%v", ok, err)
		}
	})
}
