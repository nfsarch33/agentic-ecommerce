package permmodel

import (
	"testing"
	"time"
)

func TestRoleStore_AssignAndCheck(t *testing.T) {
	t.Parallel()

	rs := NewRoleStore()
	rs.AddRole(Role{
		ID:          "admin",
		Name:        "Administrator",
		Permissions: []Permission{{Resource: "orders", Action: "delete"}},
	})
	rs.AssignRole("alice", "admin")

	roles := rs.RolesFor("alice")
	if len(roles) != 1 {
		t.Fatalf("RolesFor alice = %d, want 1", len(roles))
	}
	if roles[0].ID != "admin" {
		t.Errorf("role ID = %q, want admin", roles[0].ID)
	}
}

func TestRoleStore_GetRole(t *testing.T) {
	t.Parallel()

	rs := NewRoleStore()
	rs.AddRole(Role{ID: "viewer", Name: "Viewer"})

	r, err := rs.GetRole("viewer")
	if err != nil {
		t.Fatalf("GetRole: %v", err)
	}
	if r.Name != "Viewer" {
		t.Errorf("Name = %q, want Viewer", r.Name)
	}

	_, err = rs.GetRole("nonexistent")
	if err == nil {
		t.Error("expected error for unknown role")
	}
}

func TestACLStore_GrantRevoke(t *testing.T) {
	t.Parallel()

	acl := NewACLStore()
	acl.Grant("bob", "product", "view")

	if !acl.Can("bob", "product", "view") {
		t.Error("bob should be able to view product")
	}

	acl.Revoke("bob", "product", "view")
	if acl.Can("bob", "product", "view") {
		t.Error("bob should not be able to view product after revoke")
	}
}

func TestPermissionChecker_Combined(t *testing.T) {
	t.Parallel()

	rs := NewRoleStore()
	rs.AddRole(Role{
		ID:          "editor",
		Name:        "Editor",
		Permissions: []Permission{{Resource: "article", Action: "edit"}},
	})
	rs.AssignRole("carol", "editor")

	acl := NewACLStore()
	acl.Grant("carol", "product", "publish")

	checker := PermissionChecker{}

	// Via RBAC.
	if !checker.CanWithRoles(rs, acl, "carol", "article", "edit") {
		t.Error("carol should be able to edit article via RBAC")
	}
	// Via ACL.
	if !checker.CanWithRoles(rs, acl, "carol", "product", "publish") {
		t.Error("carol should be able to publish product via ACL")
	}
	// Neither.
	if checker.CanWithRoles(rs, acl, "carol", "orders", "delete") {
		t.Error("carol should NOT be able to delete orders")
	}
}

func TestCache_HitMissExpiry(t *testing.T) {
	t.Parallel()

	c := NewCache()
	now := time.Now()

	c.Set("user1", "resource", "action", true, now)

	// Cache hit.
	result, ok := c.Get("user1", "resource", "action", now.Add(time.Second))
	if !ok {
		t.Fatal("expected cache hit")
	}
	if !result {
		t.Error("cached result should be true")
	}

	// Cache miss for different key.
	_, ok = c.Get("user2", "resource", "action", now)
	if ok {
		t.Error("expected cache miss for user2")
	}

	// Cache expiry.
	_, ok = c.Get("user1", "resource", "action", now.Add(31*time.Second))
	if ok {
		t.Error("expected cache miss after TTL expiry")
	}
}
