package collab

import (
	"testing"
)

func TestRolePermission(t *testing.T) {
	t.Parallel()
	cases := []struct {
		role string
		want Permission
	}{
		{"admin", PermAdmin},
		{"write", PermWrite},
		{"triage", PermTriage},
		{"read", PermRead},
		{"bogus", PermNone},
	}
	for _, tc := range cases {
		role := tc.role
		if got := rolePermission(&role); got != tc.want {
			t.Errorf("rolePermission(%q) = %v, want %v", tc.role, got, tc.want)
		}
	}
	if got := rolePermission(nil); got != PermNone {
		t.Errorf("rolePermission(nil) = %v, want PermNone", got)
	}
}

func TestOrgRolePermission(t *testing.T) {
	t.Parallel()
	cases := []struct {
		role string
		want Permission
	}{
		{"owner", PermAdmin},
		{"maintainer", PermWrite},
		{"member", PermNone},
		{"bogus", PermNone},
	}
	for _, tc := range cases {
		role := tc.role
		if got := orgRolePermission(&role); got != tc.want {
			t.Errorf("orgRolePermission(%q) = %v, want %v", tc.role, got, tc.want)
		}
	}
	if got := orgRolePermission(nil); got != PermNone {
		t.Errorf("orgRolePermission(nil) = %v, want PermNone", got)
	}
}

func TestCombineRoles(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		repoRole string
		orgRole  string
		want     Permission
	}{
		{"repo admin wins over org member", "admin", "member", PermAdmin},
		{"org owner overrides repo read", "read", "owner", PermAdmin},
		{"repo write over org member", "write", "member", PermWrite},
		{"org maintainer over repo read", "read", "maintainer", PermWrite},
		{"both nil", "", "", PermNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var repoRole, orgRole *string
			if tc.repoRole != "" {
				r := tc.repoRole
				repoRole = &r
			}
			if tc.orgRole != "" {
				o := tc.orgRole
				orgRole = &o
			}
			if got := combineRoles(repoRole, orgRole); got != tc.want {
				t.Errorf("combineRoles = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAccessAtLeast(t *testing.T) {
	t.Parallel()
	access := Access{Permission: PermWrite}
	if !access.AtLeast(PermRead) {
		t.Error("write should be at least read")
	}
	if !access.AtLeast(PermWrite) {
		t.Error("write should be at least write")
	}
	if access.AtLeast(PermAdmin) {
		t.Error("write should not be at least admin")
	}
}

func TestCanManageBranchRules(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		access Access
		want   bool
	}{
		{"admin", Access{Permission: PermAdmin}, true},
		{"org maintainer", Access{Permission: PermWrite, OrgMaintainer: true}, true},
		{"org owner via perm", Access{Permission: PermAdmin, OrgOwner: true}, true},
		{"plain write", Access{Permission: PermWrite}, false},
		{"triage", Access{Permission: PermTriage}, false},
		{"none", Access{Permission: PermNone}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.access.CanManageBranchRules(); got != tc.want {
				t.Errorf("CanManageBranchRules = %v, want %v", got, tc.want)
			}
		})
	}
}
