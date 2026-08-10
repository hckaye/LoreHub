package authz

import (
	"errors"
	"sort"
	"strings"
	"unicode"
)

const (
	PermissionRead       = "read"
	PermissionWrite      = "write"
	PermissionAdmin      = "admin"
	PermissionObliterate = "obliterate"
)

const (
	OperationRead             = "read"
	OperationWrite            = "write"
	OperationBranchPush       = "branch_push"
	OperationBranchCreate     = "branch_create"
	OperationBranchDelete     = "branch_delete"
	OperationRepositoryCreate = "repository_create"
	OperationObliterate       = "obliterate"
	OperationMerge            = "merge"
)

var (
	ErrInvalidResource = errors.New("invalid Lore resource")
	ErrScopeWidened    = errors.New("requested token scope is broader than the current grant")
)

// PermissionsForRole translates the control-plane role vocabulary to Lore's
// exact permission strings. Obliterate is deliberately never implied by a
// repository admin role.
func PermissionsForRole(role string) map[string]bool {
	permissions := make(map[string]bool)
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "owner":
		permissions[PermissionRead] = true
		permissions[PermissionWrite] = true
		permissions[PermissionAdmin] = true
	case "admin":
		permissions[PermissionRead] = true
		permissions[PermissionWrite] = true
		permissions[PermissionAdmin] = true
	case "maintain", "maintainer", "write":
		permissions[PermissionRead] = true
		permissions[PermissionWrite] = true
	case "triage", "read", "member":
		permissions[PermissionRead] = true
	}
	return permissions
}

func PermissionList(permissions map[string]bool) []string {
	result := make([]string, 0, len(permissions))
	for permission, enabled := range permissions {
		if enabled {
			result = append(result, permission)
		}
	}
	sort.Slice(result, func(left, right int) bool {
		return permissionRank(result[left]) < permissionRank(result[right])
	})
	return result
}

func permissionRank(permission string) int {
	switch permission {
	case PermissionRead:
		return 1
	case PermissionWrite:
		return 2
	case PermissionAdmin:
		return 3
	case PermissionObliterate:
		return 4
	default:
		return 100
	}
}

func IntersectPermissions(available map[string]bool, requested []string) (map[string]bool, error) {
	available = expandPermissionDependencies(available)
	if len(requested) == 0 {
		return clonePermissions(available), nil
	}
	result := make(map[string]bool)
	for _, permission := range requested {
		permission = strings.ToLower(strings.TrimSpace(permission))
		if permission != PermissionRead && permission != PermissionWrite &&
			permission != PermissionAdmin && permission != PermissionObliterate {
			return nil, ErrInvalidResource
		}
		if !available[permission] {
			return nil, ErrScopeWidened
		}
		result[permission] = true
	}
	return expandPermissionDependencies(result), nil
}

func ExpandPermissions(permissions map[string]bool) map[string]bool {
	return expandPermissionDependencies(permissions)
}

func clonePermissions(source map[string]bool) map[string]bool {
	result := make(map[string]bool, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func expandPermissionDependencies(permissions map[string]bool) map[string]bool {
	result := clonePermissions(permissions)
	if result[PermissionWrite] || result[PermissionAdmin] {
		result[PermissionRead] = true
	}
	if result[PermissionAdmin] {
		result[PermissionWrite] = true
	}
	return result
}

func ValidResourceID(resourceID string) bool {
	resourceID = strings.TrimSpace(resourceID)
	if !strings.HasPrefix(resourceID, "urc-") || len(resourceID) != len("urc-")+32 {
		return false
	}
	for _, character := range resourceID[len("urc-"):] {
		if unicode.IsUpper(character) || !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return resourceID != "urc-*"
}

func RequirePermission(operation string, permissions map[string]bool) bool {
	permissions = expandPermissionDependencies(permissions)
	switch operation {
	case OperationRead:
		return permissions[PermissionRead]
	case OperationWrite, OperationBranchPush, OperationMerge, OperationBranchCreate, OperationBranchDelete:
		return permissions[PermissionWrite]
	case OperationRepositoryCreate:
		return permissions[PermissionAdmin]
	case OperationObliterate:
		return permissions[PermissionObliterate]
	default:
		return false
	}
}
