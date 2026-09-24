package auth

import (
	"context"
	"strings"
	"time"

	"github.com/crm/backend/internal/permissions"
	"github.com/crm/backend/pkg/apperrors"
)

type Service struct {
	repo   *SessionRepository
	jwt    *JWTService
	hasher *BcryptHasher
}

func NewService(repo *SessionRepository, jwt *JWTService, hasher *BcryptHasher) *Service {
	return &Service{repo: repo, jwt: jwt, hasher: hasher}
}

func (s *Service) Login(ctx context.Context, email, password, userAgent, ip string) (*TokenPair, *SessionUser, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || password == "" {
		return nil, nil, apperrors.Validation("email and password are required")
	}

	user, err := s.repo.FindUserByEmail(ctx, email)
	if err != nil {
		return nil, nil, apperrors.Internal("failed to look up user", err)
	}
	if user == nil || s.hasher.Compare(user.PasswordHash, password) != nil {
		return nil, nil, apperrors.Unauthorized("invalid email or password")
	}
	if !user.IsActive {
		return nil, nil, apperrors.Unauthorized("account is deactivated")
	}

	pair, session, err := s.issueSession(ctx, user, userAgent, ip)
	if err != nil {
		return nil, nil, err
	}
	_ = s.repo.TouchLastLogin(ctx, user.ID)
	return pair, session, nil
}

func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}
	return s.repo.RevokeRefreshSession(ctx, HashRefreshToken(refreshToken))
}

func (s *Service) Refresh(ctx context.Context, refreshToken, userAgent, ip string) (*TokenPair, *SessionUser, error) {
	if refreshToken == "" {
		return nil, nil, apperrors.Unauthorized("refresh token required")
	}
	hash := HashRefreshToken(refreshToken)
	_, userID, expiresAt, revoked, err := s.repo.FindRefreshSession(ctx, hash)
	if err != nil {
		return nil, nil, apperrors.Internal("failed to load session", err)
	}
	if userID == "" || revoked || time.Now().UTC().After(expiresAt) {
		return nil, nil, apperrors.Unauthorized("session expired")
	}

	user, err := s.repo.FindUserByID(ctx, userID)
	if err != nil {
		return nil, nil, apperrors.Internal("failed to load user", err)
	}
	if user == nil || !user.IsActive {
		_ = s.repo.RevokeRefreshSession(ctx, hash)
		return nil, nil, apperrors.Unauthorized("account is deactivated")
	}

	raw, newHash, err := NewRefreshToken()
	if err != nil {
		return nil, nil, apperrors.Internal("failed to create refresh token", err)
	}
	refreshExp := time.Now().UTC().Add(RefreshTokenTTL)
	if err := s.repo.RotateRefreshSession(ctx, hash, newHash, userAgent, ip, refreshExp); err != nil {
		return nil, nil, apperrors.Internal("failed to rotate session", err)
	}

	access, accessExp, err := s.jwt.IssueAccessToken(user.ID, user.Email, user.RoleCode)
	if err != nil {
		return nil, nil, apperrors.Internal("failed to issue access token", err)
	}

	session, err := s.buildSessionUser(ctx, user)
	if err != nil {
		return nil, nil, err
	}

	return &TokenPair{
		AccessToken:      access,
		RefreshToken:     raw,
		AccessExpiresAt:  accessExp,
		RefreshExpiresAt: refreshExp,
	}, session, nil
}

func (s *Service) Me(ctx context.Context, accessToken string) (*SessionUser, error) {
	claims, err := s.jwt.ParseAccessToken(accessToken)
	if err != nil {
		return nil, apperrors.Unauthorized("invalid or expired session")
	}
	user, err := s.repo.FindUserByID(ctx, claims.UserID)
	if err != nil {
		return nil, apperrors.Internal("failed to load user", err)
	}
	if user == nil || !user.IsActive {
		return nil, apperrors.Unauthorized("account is deactivated")
	}
	return s.buildSessionUser(ctx, user)
}

func (s *Service) AuthenticateAccessToken(ctx context.Context, accessToken string) (*Claims, error) {
	parsed, err := s.jwt.ParseAccessToken(accessToken)
	if err != nil {
		return nil, apperrors.Unauthorized("invalid or expired session")
	}
	user, err := s.repo.FindUserByID(ctx, parsed.UserID)
	if err != nil {
		return nil, apperrors.Internal("failed to load user", err)
	}
	if user == nil || !user.IsActive {
		return nil, apperrors.Unauthorized("account is deactivated")
	}

	grants, err := s.repo.ListPermissionGrants(ctx, user.RoleID)
	if err != nil {
		return nil, apperrors.Internal("failed to load permissions", err)
	}
	scopes := permissions.ExpandImpliesScoped(grants)
	codes := make([]string, 0, len(scopes))
	for c := range scopes {
		codes = append(codes, c)
	}
	teams, err := s.repo.ListTeamIDs(ctx, user.ID)
	if err != nil {
		return nil, apperrors.Internal("failed to load teams", err)
	}
	members, err := s.repo.ListTeamMemberUserIDs(ctx, user.ID)
	if err != nil {
		return nil, apperrors.Internal("failed to load team members", err)
	}

	return &Claims{
		UserID:            user.ID,
		Email:             user.Email,
		FullName:          user.FullName,
		RoleCode:          user.RoleCode,
		RoleID:            user.RoleID,
		TeamIDs:           teams,
		TeamMemberUserIDs: members,
		Permissions:       codes,
		PermissionScopes:  scopes,
		ExpiresAt:         parsed.ExpiresAt.Time,
	}, nil
}

func (s *Service) issueSession(ctx context.Context, user *UserRecord, userAgent, ip string) (*TokenPair, *SessionUser, error) {
	access, accessExp, err := s.jwt.IssueAccessToken(user.ID, user.Email, user.RoleCode)
	if err != nil {
		return nil, nil, apperrors.Internal("failed to issue access token", err)
	}
	raw, hash, err := NewRefreshToken()
	if err != nil {
		return nil, nil, apperrors.Internal("failed to create refresh token", err)
	}
	refreshExp := time.Now().UTC().Add(RefreshTokenTTL)
	if _, err := s.repo.CreateRefreshSession(ctx, user.ID, hash, userAgent, ip, refreshExp); err != nil {
		return nil, nil, apperrors.Internal("failed to persist session", err)
	}
	session, err := s.buildSessionUser(ctx, user)
	if err != nil {
		return nil, nil, err
	}
	return &TokenPair{
		AccessToken:      access,
		RefreshToken:     raw,
		AccessExpiresAt:  accessExp,
		RefreshExpiresAt: refreshExp,
	}, session, nil
}

func (s *Service) buildSessionUser(ctx context.Context, user *UserRecord) (*SessionUser, error) {
	grants, err := s.repo.ListPermissionGrants(ctx, user.RoleID)
	if err != nil {
		return nil, apperrors.Internal("failed to load permissions", err)
	}
	scopes := permissions.ExpandImpliesScoped(grants)
	codes := make([]string, 0, len(scopes))
	for c := range scopes {
		codes = append(codes, c)
	}
	teams, err := s.repo.ListTeamIDs(ctx, user.ID)
	if err != nil {
		return nil, apperrors.Internal("failed to load teams", err)
	}
	return &SessionUser{
		ID:               user.ID,
		Email:            user.Email,
		FullName:         user.FullName,
		RoleCode:         user.RoleCode,
		RoleName:         user.RoleName,
		IsActive:         user.IsActive,
		Timezone:         coalesceTZ(user.Timezone),
		Phone:            user.Phone,
		TeamIDs:          teams,
		Permissions:      codes,
		PermissionScopes: scopes,
	}, nil
}

func coalesceTZ(tz string) string {
	if strings.TrimSpace(tz) == "" {
		return "UTC"
	}
	return tz
}

func (s *Service) Hasher() *BcryptHasher { return s.hasher }
