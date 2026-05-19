package permmodel

import (
	"errors"
	"sync"
	"time"
)

// Permission is a (resource, action) pair.
type Permission struct {
	Resource string
	Action   string
}

// Role groups permissions under a named identifier.
type Role struct {
	ID          string
	Name        string
	Permissions []Permission
}

// RoleStore is a thread-safe store for roles and user role assignments.
type RoleStore struct {
	mu          sync.RWMutex
	roles       map[string]Role
	assignments map[string][]string // userID -> []roleID
}

// NewRoleStore returns an initialised RoleStore.
func NewRoleStore() *RoleStore {
	return &RoleStore{
		roles:       make(map[string]Role),
		assignments: make(map[string][]string),
	}
}

// AddRole registers a role.
func (rs *RoleStore) AddRole(role Role) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.roles[role.ID] = role
}

// GetRole retrieves a role by ID.
func (rs *RoleStore) GetRole(id string) (*Role, error) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	r, ok := rs.roles[id]
	if !ok {
		return nil, errors.New("permmodel: role not found: " + id)
	}
	cp := r
	return &cp, nil
}

// AssignRole assigns a role to a user.
func (rs *RoleStore) AssignRole(userID, roleID string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	// Avoid duplicates.
	for _, existing := range rs.assignments[userID] {
		if existing == roleID {
			return
		}
	}
	rs.assignments[userID] = append(rs.assignments[userID], roleID)
}

// RolesFor returns all roles assigned to the user.
func (rs *RoleStore) RolesFor(userID string) []Role {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	var out []Role
	for _, rid := range rs.assignments[userID] {
		if r, ok := rs.roles[rid]; ok {
			out = append(out, r)
		}
	}
	return out
}

// ACLStore stores explicit user-resource-action grants.
type ACLStore struct {
	mu     sync.RWMutex
	grants map[string]bool // key: userID+"\x00"+resource+"\x00"+action
}

// NewACLStore returns an initialised ACLStore.
func NewACLStore() *ACLStore {
	return &ACLStore{grants: make(map[string]bool)}
}

func aclKey(userID, resource, action string) string {
	return userID + "\x00" + resource + "\x00" + action
}

// Grant adds an explicit permission for a user.
func (a *ACLStore) Grant(userID, resource, action string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.grants[aclKey(userID, resource, action)] = true
}

// Revoke removes an explicit permission for a user.
func (a *ACLStore) Revoke(userID, resource, action string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.grants, aclKey(userID, resource, action))
}

// Can returns whether the user has an explicit ACL grant.
func (a *ACLStore) Can(userID, resource, action string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.grants[aclKey(userID, resource, action)]
}

// PermissionChecker combines RoleStore and ACLStore for authorization decisions.
type PermissionChecker struct{}

// CanWithRoles checks ACL first, then RBAC role permissions.
func (PermissionChecker) CanWithRoles(store *RoleStore, aclStore *ACLStore, userID, resource, action string) bool {
	// ACL check first.
	if aclStore.Can(userID, resource, action) {
		return true
	}

	// RBAC check.
	roles := store.RolesFor(userID)
	for _, role := range roles {
		for _, perm := range role.Permissions {
			if perm.Resource == resource && perm.Action == action {
				return true
			}
		}
	}
	return false
}

// cacheEntry stores a cached permission result with expiry.
type cacheEntry struct {
	result    bool
	expiresAt time.Time
}

// Cache is a thread-safe permission cache with TTL.
type Cache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
	ttl     time.Duration
}

// NewCache returns a Cache with 30-second TTL.
func NewCache() *Cache {
	return &Cache{
		entries: make(map[string]cacheEntry),
		ttl:     30 * time.Second,
	}
}

func cacheKey(userID, resource, action string) string {
	return userID + "\x00" + resource + "\x00" + action
}

// Set stores a permission result in the cache.
func (c *Cache) Set(userID, resource, action string, result bool, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[cacheKey(userID, resource, action)] = cacheEntry{
		result:    result,
		expiresAt: now.Add(c.ttl),
	}
}

// Get retrieves a cached permission result.
// Returns (result, true) on hit, (false, false) on miss or expiry.
func (c *Cache) Get(userID, resource, action string, now time.Time) (bool, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[cacheKey(userID, resource, action)]
	if !ok || now.After(e.expiresAt) {
		return false, false
	}
	return e.result, true
}
