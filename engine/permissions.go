package engine

import "time"

// Permission defines what actions are allowed
type Permission int

const (
	PermNone      Permission = 0
	PermSubscribe Permission = 1 << iota // Can subscribe to channel
	PermPublish                          // Can publish to channel
	PermPresence                         // Can see presence info
	PermHistory                          // Can access message history
	PermJoinLeave                        // Receives join/leave events
)

// PermAll grants all permissions
const PermAll = PermSubscribe | PermPublish | PermPresence | PermHistory | PermJoinLeave

// PermReadOnly grants subscribe, presence, history (no publish)
const PermReadOnly = PermSubscribe | PermPresence | PermHistory

// PermStandard grants subscribe, publish, presence
const PermStandard = PermSubscribe | PermPublish | PermPresence

// Has checks if a permission is granted
func (p Permission) Has(perm Permission) bool {
	return p&perm == perm
}

// Add adds a permission
func (p Permission) Add(perm Permission) Permission {
	return p | perm
}

// Remove removes a permission
func (p Permission) Remove(perm Permission) Permission {
	return p &^ perm
}

// ChannelRule defines permissions for a channel pattern
type ChannelRule struct {
	// Pattern is a channel name pattern (supports * wildcard)
	// Examples: "chat:*", "user:123:*", "public:*"
	Pattern string

	// DefaultPermissions for all users matching this pattern
	DefaultPermissions Permission

	// RequireAuth requires authentication to access
	RequireAuth bool

	// AllowAnonymous allows unauthenticated access (overrides RequireAuth)
	AllowAnonymous bool

	// MaxSubscribers limits subscribers (0 = unlimited)
	MaxSubscribers int

	// MaxPublishRate limits publishes per second per user (0 = unlimited)
	MaxPublishRate int

	// JoinLeave enables join/leave notifications
	JoinLeave bool

	// Presence enables presence feature
	Presence bool

	// HistorySize overrides namespace history size
	HistorySize int64

	// HistoryTTL overrides namespace history TTL
	HistoryTTL time.Duration
}

// PermissionChecker is the interface for checking permissions
type PermissionChecker interface {
	// CheckConnect checks if user can connect
	CheckConnect(userID string, token string) (bool, error)

	// CheckSubscribe checks if user can subscribe to channel
	CheckSubscribe(userID string, channel string, token string) (Permission, error)

	// CheckPublish checks if user can publish to channel
	CheckPublish(userID string, channel string) (bool, error)

	// GetUserPermissions returns user's permissions for a channel
	GetUserPermissions(userID string, channel string) Permission
}

// DefaultPermissionChecker implements PermissionChecker with configurable rules
type DefaultPermissionChecker struct {
	// Rules is a list of channel rules (checked in order)
	rules []ChannelRule

	// DefaultRule is used when no pattern matches
	defaultRule ChannelRule

	// UserPermissions stores per-user permissions
	// map[userID]map[channel]Permission
	userPermissions map[string]map[string]Permission

	// RolePcermissions stores per-role permissions
	// map[role]Permission
	rolePermissions map[string]Permission

	// UserRoles stores user roles
	// map[userID][]role
	userRoles map[string][]string

	// RequireAuth requires authentication for all operations
	requireAuth bool
}

// NewPermissionChecker creates a new permission checker
func NewPermissionChecker() *DefaultPermissionChecker {
	return &DefaultPermissionChecker{
		rules: []ChannelRule{},
		defaultRule: ChannelRule{
			DefaultPermissions: PermStandard,
			Presence:           true,
		},
		userPermissions: make(map[string]map[string]Permission),
		rolePermissions: make(map[string]Permission),
		userRoles:       make(map[string][]string),
	}
}

// AddRule adds a channel rule
func (c *DefaultPermissionChecker) AddRule(rule ChannelRule) {
	c.rules = append(c.rules, rule)
}

// SetDefaultRule sets the default rule
func (c *DefaultPermissionChecker) SetDefaultRule(rule ChannelRule) {
	c.defaultRule = rule
}

// SetRequireAuth enables/disables global auth requirement
func (c *DefaultPermissionChecker) SetRequireAuth(require bool) {
	c.requireAuth = require
}

// SetUserPermission sets permission for a specific user on a channel
func (c *DefaultPermissionChecker) SetUserPermission(userID, channel string, perm Permission) {
	if c.userPermissions[userID] == nil {
		c.userPermissions[userID] = make(map[string]Permission)
	}
	c.userPermissions[userID][channel] = perm
}

// RemoveUserPermission removes a user's specific permission
func (c *DefaultPermissionChecker) RemoveUserPermission(userID, channel string) {
	if c.userPermissions[userID] != nil {
		delete(c.userPermissions[userID], channel)
	}
}

// SetRolePermission sets permission for a role
func (c *DefaultPermissionChecker) SetRolePermission(role string, perm Permission) {
	c.rolePermissions[role] = perm
}

// AssignRole assigns a role to a user
func (c *DefaultPermissionChecker) AssignRole(userID, role string) {
	c.userRoles[userID] = append(c.userRoles[userID], role)
}

// RemoveRole removes a role from a user
func (c *DefaultPermissionChecker) RemoveRole(userID, role string) {
	roles := c.userRoles[userID]
	newRoles := make([]string, 0, len(roles))
	for _, r := range roles {
		if r != role {
			newRoles = append(newRoles, r)
		}
	}
	c.userRoles[userID] = newRoles
}

// CheckConnect checks if user can connect
func (c *DefaultPermissionChecker) CheckConnect(userID string, token string) (bool, error) {
	// If auth is required and no userID, deny
	if c.requireAuth && userID == "" {
		return false, ErrUnauthorized
	}
	return true, nil
}

// CheckSubscribe checks if user can subscribe to channel
func (c *DefaultPermissionChecker) CheckSubscribe(userID string, channel string, token string) (Permission, error) {
	rule := c.findRule(channel)

	// Check auth requirement
	if rule.RequireAuth && userID == "" && !rule.AllowAnonymous {
		return PermNone, ErrUnauthorized
	}

	// Get base permissions from rule
	perm := rule.DefaultPermissions

	// Apply role permissions
	for _, role := range c.userRoles[userID] {
		if rolePerm, ok := c.rolePermissions[role]; ok {
			perm = perm.Add(rolePerm)
		}
	}

	// Apply user-specific permissions (override)
	if userPerms, ok := c.userPermissions[userID]; ok {
		if channelPerm, ok := userPerms[channel]; ok {
			perm = channelPerm
		}
	}

	if !perm.Has(PermSubscribe) {
		return PermNone, ErrPermissionDenied
	}

	return perm, nil
}

// CheckPublish checks if user can publish to channel
func (c *DefaultPermissionChecker) CheckPublish(userID string, channel string) (bool, error) {
	perm := c.GetUserPermissions(userID, channel)
	if !perm.Has(PermPublish) {
		return false, ErrPermissionDenied
	}
	return true, nil
}

// GetUserPermissions returns user's permissions for a channel
func (c *DefaultPermissionChecker) GetUserPermissions(userID string, channel string) Permission {
	rule := c.findRule(channel)
	perm := rule.DefaultPermissions

	// Apply role permissions
	for _, role := range c.userRoles[userID] {
		if rolePerm, ok := c.rolePermissions[role]; ok {
			perm = perm.Add(rolePerm)
		}
	}

	// Apply user-specific permissions
	if userPerms, ok := c.userPermissions[userID]; ok {
		if channelPerm, ok := userPerms[channel]; ok {
			perm = channelPerm
		}
	}

	return perm
}

// GetChannelRule returns the rule for a channel
func (c *DefaultPermissionChecker) GetChannelRule(channel string) ChannelRule {
	return c.findRule(channel)
}

// findRule finds the first matching rule for a channel
func (c *DefaultPermissionChecker) findRule(channel string) ChannelRule {
	for _, rule := range c.rules {
		if matchPattern(rule.Pattern, channel) {
			return rule
		}
	}
	return c.defaultRule
}

// matchPattern checks if a channel matches a pattern
// Supports * as wildcard
func matchPattern(pattern, channel string) bool {
	if pattern == "*" {
		return true
	}

	pi, ci := 0, 0
	plen, clen := len(pattern), len(channel)

	for pi < plen && ci < clen {
		if pattern[pi] == '*' {
			// If * is at end, match rest
			if pi == plen-1 {
				return true
			}
			// Find next literal in pattern
			pi++
			nextLiteral := ""
			for pi < plen && pattern[pi] != '*' {
				nextLiteral += string(pattern[pi])
				pi++
			}
			// Find nextLiteral in remaining channel
			found := false
			for ci < clen {
				if ci+len(nextLiteral) <= clen && channel[ci:ci+len(nextLiteral)] == nextLiteral {
					ci += len(nextLiteral)
					found = true
					break
				}
				ci++
			}
			if !found {
				return false
			}
		} else if pattern[pi] == channel[ci] {
			pi++
			ci++
		} else {
			return false
		}
	}

	// Handle trailing *
	for pi < plen && pattern[pi] == '*' {
		pi++
	}

	return pi == plen && ci == clen
}
