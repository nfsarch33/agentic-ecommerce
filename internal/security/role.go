package security

import (
	"errors"
	"strings"
)

type Role string

const (
	RoleViewer   Role = "viewer"
	RoleOperator Role = "operator"
	RoleAdmin    Role = "admin"
)

var ErrInvalidRole = errors.New("invalid role")

func ParseRole(value string) (Role, error) {
	switch Role(strings.ToLower(strings.TrimSpace(value))) {
	case RoleViewer:
		return RoleViewer, nil
	case RoleOperator:
		return RoleOperator, nil
	case RoleAdmin:
		return RoleAdmin, nil
	default:
		return "", ErrInvalidRole
	}
}

func (r Role) Allows(required Role) bool {
	if required == "" {
		return true
	}
	return roleRank(r) >= roleRank(required) && roleRank(r) > 0
}

func roleRank(role Role) int {
	switch role {
	case RoleViewer:
		return 1
	case RoleOperator:
		return 2
	case RoleAdmin:
		return 3
	default:
		return 0
	}
}
