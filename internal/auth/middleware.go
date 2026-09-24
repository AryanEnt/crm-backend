package auth

import (
	"net/http"
	"strings"

	"github.com/crm/backend/internal/permissions"
	"github.com/crm/backend/pkg/apperrors"
	"github.com/crm/backend/pkg/response"
)

type Middleware struct {
	service *Service
}

func NewMiddleware(service *Service) *Middleware {
	return &Middleware{service: service}
}

// Authenticate validates the Bearer access token and loads claims.
func (m *Middleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractBearer(r)
		if token == "" {
			response.Fail(w, apperrors.Unauthorized("authentication required"))
			return
		}
		claims, err := m.service.AuthenticateAccessToken(r.Context(), token)
		if err != nil {
			response.Fail(w, err)
			return
		}
		ctx := WithClaims(r.Context(), *claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequirePermission enforces that the caller has at least one of the given permissions.
func RequirePermission(codes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			set := PermissionSetFromContext(r.Context())
			if !set.HasAny(codes...) {
				response.Fail(w, apperrors.Forbidden("insufficient permissions"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAllPermissions enforces that the caller has all given permissions.
func RequireAllPermissions(codes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			set := PermissionSetFromContext(r.Context())
			if !set.HasAll(codes...) {
				response.Fail(w, apperrors.Forbidden("insufficient permissions"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	if c, err := r.Cookie(AccessCookie); err == nil && c.Value != "" {
		return c.Value
	}
	return ""
}

// Ensure Super Admin shortcut for manage-heavy admin routes.
func RequireUsersAdmin() func(http.Handler) http.Handler {
	return RequirePermission(permissions.UsersManage, permissions.UsersView)
}
