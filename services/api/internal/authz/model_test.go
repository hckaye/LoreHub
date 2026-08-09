package authz

import (
	"errors"
	"reflect"
	"testing"
)

func TestRolePermissionMapping(t *testing.T) {
	tests := []struct {
		role string
		want []string
	}{
		{role: "read", want: []string{"read"}},
		{role: "triage", want: []string{"read"}},
		{role: "write", want: []string{"read", "write"}},
		{role: "maintain", want: []string{"read", "write"}},
		{role: "admin", want: []string{"read", "write", "admin"}},
	}
	for _, test := range tests {
		t.Run(test.role, func(t *testing.T) {
			if got := PermissionList(PermissionsForRole(test.role)); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("permissions = %v, want %v", got, test.want)
			}
		})
	}
}

func TestObliterateIsNotImplied(t *testing.T) {
	if PermissionsForRole("admin")[PermissionObliterate] {
		t.Fatal("admin must not imply obliterate")
	}
	if RequirePermission(OperationObliterate, PermissionsForRole("admin")) {
		t.Fatal("admin must not authorize obliterate")
	}
}

func TestObliterateDoesNotGrantLowerOperations(t *testing.T) {
	permissions := map[string]bool{PermissionObliterate: true}
	if RequirePermission(OperationRead, permissions) || RequirePermission(OperationWrite, permissions) {
		t.Fatal("obliterate must remain a separate operation permission")
	}
	if !RequirePermission(OperationObliterate, permissions) {
		t.Fatal("explicit obliterate permission must authorize obliterate")
	}
}

func TestScopeCannotWiden(t *testing.T) {
	available := PermissionsForRole("read")
	if _, err := IntersectPermissions(available, []string{PermissionWrite}); !errors.Is(err, ErrScopeWidened) {
		t.Fatalf("error = %v, want scope widening error", err)
	}
	got, err := IntersectPermissions(PermissionsForRole("admin"), []string{PermissionWrite})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(PermissionList(got), []string{"read", "write"}) {
		t.Fatalf("narrowed permissions = %v", PermissionList(got))
	}
}

func TestResourceIDRejectsWildcard(t *testing.T) {
	for _, resource := range []string{"urc-*", "urc-", "urc-a*", "repo-a", "urc-repository-a",
		"urc-0123456789abcdef0123456789ABCDEf"} {
		if ValidResourceID(resource) {
			t.Fatalf("resource %q was accepted", resource)
		}
	}
	if !ValidResourceID("urc-0123456789abcdef0123456789abcdef") {
		t.Fatal("valid resource was rejected")
	}
}
