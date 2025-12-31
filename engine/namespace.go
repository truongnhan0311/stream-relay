package engine

import "time"

// Namespace groups channels with shared configuration
type Namespace struct {
	// Name is the namespace identifier (e.g., "chat", "notifications", "system")
	Name string

	// ChannelPrefix is prepended to channels in this namespace
	// e.g., prefix "chat:" means channel "general" becomes "chat:general"
	ChannelPrefix string

	// Options contains namespace-specific settings
	Options NamespaceOptions
}

// NamespaceOptions contains configuration for a namespace
type NamespaceOptions struct {
	// --- History & Recovery ---

	// HistorySize is the number of messages to keep in history
	HistorySize int64

	// HistoryTTL is how long to keep messages
	HistoryTTL time.Duration

	// HistoryRecover allows clients to recover missed messages
	HistoryRecover bool

	// --- Presence ---

	// Presence enables presence feature (who's online)
	Presence bool

	// JoinLeave enables join/leave notifications
	JoinLeave bool

	// ForcePushJoinLeave forces join/leave even if client didn't request
	ForcePushJoinLeave bool

	// PresenceDisableForClient hides presence from clients but tracks internally
	PresenceDisableForClient bool

	// --- Publishing ---

	// Publish allows clients to publish to channels in this namespace
	Publish bool

	// SubscribeToPublish requires subscription before publishing
	SubscribeToPublish bool

	// --- Permissions ---

	// Anonymous allows unauthenticated access
	Anonymous bool

	// Protected requires valid subscription token
	Protected bool

	// --- Position & Recovery ---

	// ForcePositioning requires clients to track position
	ForcePositioning bool

	// ForceRecovery forces recovery on reconnect
	ForceRecovery bool

	// --- Limits ---

	// MaxSubscribers limits subscribers per channel (0 = unlimited)
	MaxSubscribers int

	// MaxChannelLength limits channel name length
	MaxChannelLength int

	// --- Behavior ---

	// SingleConnection allows only one connection per user per channel
	SingleConnection bool

	// ProxySubscribe enables subscription proxy (custom handler)
	ProxySubscribe bool

	// ProxyPublish enables publish proxy (custom handler)
	ProxyPublish bool
}

// DefaultNamespaceOptions returns default namespace options
func DefaultNamespaceOptions() NamespaceOptions {
	return NamespaceOptions{
		HistorySize:      100,
		HistoryTTL:       24 * time.Hour,
		HistoryRecover:   true,
		Presence:         true,
		JoinLeave:        false,
		Publish:          true,
		Anonymous:        false,
		Protected:        false,
		MaxChannelLength: 255,
	}
}

// NamespaceManager manages namespaces
type NamespaceManager struct {
	namespaces map[string]*Namespace
	defaultNS  *Namespace
}

// NewNamespaceManager creates a new namespace manager
func NewNamespaceManager() *NamespaceManager {
	mgr := &NamespaceManager{
		namespaces: make(map[string]*Namespace),
	}

	// Create default namespace
	mgr.defaultNS = &Namespace{
		Name:          "default",
		ChannelPrefix: "",
		Options:       DefaultNamespaceOptions(),
	}

	return mgr
}

// Register adds a namespace
func (m *NamespaceManager) Register(ns *Namespace) {
	m.namespaces[ns.Name] = ns
}

// Get returns a namespace by name
func (m *NamespaceManager) Get(name string) (*Namespace, bool) {
	ns, ok := m.namespaces[name]
	return ns, ok
}

// GetByChannel finds namespace for a channel based on prefix
func (m *NamespaceManager) GetByChannel(channel string) *Namespace {
	// Check for namespace prefix in channel name
	for _, ns := range m.namespaces {
		if ns.ChannelPrefix != "" && len(channel) > len(ns.ChannelPrefix) {
			if channel[:len(ns.ChannelPrefix)] == ns.ChannelPrefix {
				return ns
			}
		}
	}
	return m.defaultNS
}

// SetDefault sets the default namespace
func (m *NamespaceManager) SetDefault(ns *Namespace) {
	m.defaultNS = ns
}

// Default returns the default namespace
func (m *NamespaceManager) Default() *Namespace {
	return m.defaultNS
}

// List returns all registered namespaces
func (m *NamespaceManager) List() []*Namespace {
	result := make([]*Namespace, 0, len(m.namespaces)+1)
	result = append(result, m.defaultNS)
	for _, ns := range m.namespaces {
		result = append(result, ns)
	}
	return result
}

// Remove removes a namespace
func (m *NamespaceManager) Remove(name string) {
	delete(m.namespaces, name)
}

// ChannelName returns the full channel name with namespace prefix
func (ns *Namespace) ChannelName(channel string) string {
	if ns.ChannelPrefix == "" {
		return channel
	}
	return ns.ChannelPrefix + channel
}

// StripPrefix removes the namespace prefix from a channel name
func (ns *Namespace) StripPrefix(channel string) string {
	if ns.ChannelPrefix == "" {
		return channel
	}
	if len(channel) > len(ns.ChannelPrefix) && channel[:len(ns.ChannelPrefix)] == ns.ChannelPrefix {
		return channel[len(ns.ChannelPrefix):]
	}
	return channel
}

// ValidateChannel checks if a channel name is valid for this namespace
func (ns *Namespace) ValidateChannel(channel string) error {
	if len(channel) == 0 {
		return ErrBadRequest
	}
	if ns.Options.MaxChannelLength > 0 && len(channel) > ns.Options.MaxChannelLength {
		return ErrBadRequest
	}
	return nil
}

// --- Preset Namespace Configurations ---

// ChatNamespace creates a namespace configured for chat
func ChatNamespace(prefix string) *Namespace {
	return &Namespace{
		Name:          "chat",
		ChannelPrefix: prefix,
		Options: NamespaceOptions{
			HistorySize:    1000,
			HistoryTTL:     7 * 24 * time.Hour, // 1 week
			HistoryRecover: true,
			Presence:       true,
			JoinLeave:      true,
			Publish:        true,
			Anonymous:      false,
			Protected:      false,
		},
	}
}

// NotificationNamespace creates a namespace for system notifications
func NotificationNamespace(prefix string) *Namespace {
	return &Namespace{
		Name:          "notifications",
		ChannelPrefix: prefix,
		Options: NamespaceOptions{
			HistorySize:      100,
			HistoryTTL:       24 * time.Hour,
			HistoryRecover:   true,
			Presence:         false,
			JoinLeave:        false,
			Publish:          false, // Server-only publish
			Anonymous:        false,
			ForceRecovery:    true,
			SingleConnection: true,
		},
	}
}

// PresenceNamespace creates a namespace for presence-only channels
func PresenceNamespace(prefix string) *Namespace {
	return &Namespace{
		Name:          "presence",
		ChannelPrefix: prefix,
		Options: NamespaceOptions{
			HistorySize:        0, // No history
			HistoryRecover:     false,
			Presence:           true,
			JoinLeave:          true,
			ForcePushJoinLeave: true,
			Publish:            false,
			Anonymous:          false,
			ForcePositioning:   false,
		},
	}
}

// PublicNamespace creates a namespace for public/anonymous access
func PublicNamespace(prefix string) *Namespace {
	return &Namespace{
		Name:          "public",
		ChannelPrefix: prefix,
		Options: NamespaceOptions{
			HistorySize:    100,
			HistoryTTL:     time.Hour,
			HistoryRecover: false,
			Presence:       false,
			JoinLeave:      false,
			Publish:        false, // Read-only for clients
			Anonymous:      true,
		},
	}
}

// PersonalNamespace creates a namespace for user-specific channels
func PersonalNamespace(prefix string) *Namespace {
	return &Namespace{
		Name:          "personal",
		ChannelPrefix: prefix,
		Options: NamespaceOptions{
			HistorySize:      500,
			HistoryTTL:       30 * 24 * time.Hour, // 30 days
			HistoryRecover:   true,
			Presence:         false,
			JoinLeave:        false,
			Publish:          false, // Server-only
			Protected:        true,  // Requires token
			ForceRecovery:    true,
			SingleConnection: true,
		},
	}
}
