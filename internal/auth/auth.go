package auth

import (
	"context"
	"time"

	"github.com/crm/backend/internal/permissions"
)

// Role codes used by the CRM.
const (
	RoleSuperAdmin     = permissions.RoleSuperAdmin
	RoleSalesManager   = permissions.RoleSalesManager
	RoleSalesExecutive = permissions.RoleSalesExecutive
	RoleSalesSupport   = permissions.RoleSalesSupport
)

// Claims holds authenticated identity information.
type Claims struct {
	UserID             string
	Email              string
	FullName           string
	RoleCode           string
	RoleID             string
	TeamIDs            []string
	TeamMemberUserIDs  []string // users sharing the caller's team memberships (for TEAM scope filters)
	Permissions        []string
	PermissionScopes   map[string]permissions.Scope
	ExpiresAt          time.Time
}

// SessionUser is the authenticated principal returned to clients.
type SessionUser struct {
	ID               string                       `json:"id"`
	Email            string                       `json:"email"`
	FullName         string                       `json:"fullName"`
	RoleCode         string                       `json:"roleCode"`
	RoleName         string                       `json:"roleName"`
	IsActive         bool                         `json:"isActive"`
	Timezone         string                       `json:"timezone"`
	Phone            string                       `json:"phone"`
	TeamIDs          []string                     `json:"teamIds"`
	Permissions      []string                     `json:"permissions"`
	PermissionScopes map[string]permissions.Scope `json:"permissionScopes,omitempty"`
}

// TokenPair is issued after successful authentication.
type TokenPair struct {
	AccessToken      string    `json:"accessToken"`
	RefreshToken     string    `json:"refreshToken"`
	AccessExpiresAt  time.Time `json:"accessExpiresAt"`
	RefreshExpiresAt time.Time `json:"refreshExpiresAt"`
}

// Authenticator verifies credentials and manages sessions.
type Authenticator interface {
	Login(ctx context.Context, email, password, userAgent, ip string) (*TokenPair, *SessionUser, error)
	Logout(ctx context.Context, refreshToken string) error
	Refresh(ctx context.Context, refreshToken, userAgent, ip string) (*TokenPair, *SessionUser, error)
	Me(ctx context.Context, accessToken string) (*SessionUser, error)
	UpdateProfile(ctx context.Context, userID string, in UpdateProfileInput) (*SessionUser, error)
	ChangePassword(ctx context.Context, userID string, in ChangePasswordInput, userAgent, ip string) (*TokenPair, *SessionUser, error)
}

type contextKey string

const ClaimsKey contextKey = "auth_claims"

func WithClaims(ctx context.Context, claims Claims) context.Context {
	return context.WithValue(ctx, ClaimsKey, claims)
}

func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	claims, ok := ctx.Value(ClaimsKey).(Claims)
	return claims, ok
}

func PermissionSetFromContext(ctx context.Context) permissions.Set {
	claims, ok := ClaimsFromContext(ctx)
	if !ok {
		return permissions.NewSet()
	}
	return permissions.ExpandImplies(claims.Permissions)
}
