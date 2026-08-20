package domain

import "time"

// AuthRole identifies a human operator's business permissions.
type AuthRole string

const (
	RoleDispatcher AuthRole = "dispatcher"
	RoleEngineer   AuthRole = "engineer"
	RoleAuditor    AuthRole = "auditor"
)

func (r AuthRole) Valid() bool {
	switch r {
	case RoleDispatcher, RoleEngineer, RoleAuditor:
		return true
	default:
		return false
	}
}

// User is a durable operator identity.
type User struct {
	ID           string
	Username     string
	PasswordHash string
	Role         AuthRole
	Active       bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Session is a revocable server-side bearer-token session.
type Session struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Principal is the authenticated identity propagated through a request.
type Principal struct {
	UserID    string
	Username  string
	Role      AuthRole
	SessionID string
}
