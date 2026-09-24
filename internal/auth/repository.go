package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/crm/backend/internal/permissions"
)

type UserRecord struct {
	ID           string
	Email        string
	PasswordHash string
	FullName     string
	Phone        string
	RoleID       string
	RoleCode     string
	RoleName     string
	IsActive     bool
	Timezone     string
}

type SessionRepository struct {
	pool *pgxpool.Pool
}

func NewSessionRepository(pool *pgxpool.Pool) *SessionRepository {
	return &SessionRepository{pool: pool}
}

func (r *SessionRepository) FindUserByEmail(ctx context.Context, email string) (*UserRecord, error) {
	const q = `
		SELECT u.id, u.email, u.password_hash, u.full_name, COALESCE(u.phone,''), u.role_id, r.code, r.name, u.is_active, u.timezone
		FROM users u
		JOIN roles r ON r.id = u.role_id
		WHERE u.email = $1
	`
	var u UserRecord
	err := r.pool.QueryRow(ctx, q, email).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.FullName, &u.Phone, &u.RoleID, &u.RoleCode, &u.RoleName, &u.IsActive, &u.Timezone,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *SessionRepository) FindUserByID(ctx context.Context, id string) (*UserRecord, error) {
	const q = `
		SELECT u.id, u.email, u.password_hash, u.full_name, COALESCE(u.phone,''), u.role_id, r.code, r.name, u.is_active, u.timezone
		FROM users u
		JOIN roles r ON r.id = u.role_id
		WHERE u.id = $1
	`
	var u UserRecord
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.FullName, &u.Phone, &u.RoleID, &u.RoleCode, &u.RoleName, &u.IsActive, &u.Timezone,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *SessionRepository) ListPermissionCodes(ctx context.Context, roleID string) ([]string, error) {
	grants, err := r.ListPermissionGrants(ctx, roleID)
	if err != nil {
		return nil, err
	}
	codes := make([]string, 0, len(grants))
	for _, g := range grants {
		codes = append(codes, g.Code)
	}
	return codes, nil
}

func (r *SessionRepository) ListPermissionGrants(ctx context.Context, roleID string) ([]permissions.Grant, error) {
	const q = `
		SELECT p.code, COALESCE(rp.scope, 'organization')
		FROM role_permissions rp
		JOIN permissions p ON p.id = rp.permission_id
		WHERE rp.role_id = $1
		ORDER BY p.code
	`
	rows, err := r.pool.Query(ctx, q, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var grants []permissions.Grant
	for rows.Next() {
		var g permissions.Grant
		var scope string
		if err := rows.Scan(&g.Code, &scope); err != nil {
			return nil, err
		}
		g.Scope = permissions.Scope(scope)
		grants = append(grants, g)
	}
	return grants, rows.Err()
}

func (r *SessionRepository) ListTeamIDs(ctx context.Context, userID string) ([]string, error) {
	const q = `SELECT team_id::text FROM team_members WHERE user_id = $1`
	rows, err := r.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ListTeamMemberUserIDs returns distinct user IDs that share any team with userID.
func (r *SessionRepository) ListTeamMemberUserIDs(ctx context.Context, userID string) ([]string, error) {
	const q = `
		SELECT DISTINCT tm2.user_id::text
		FROM team_members tm
		JOIN team_members tm2 ON tm2.team_id = tm.team_id
		WHERE tm.user_id = $1
	`
	rows, err := r.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *SessionRepository) TouchLastLogin(ctx context.Context, userID string) error {
	_, err := r.pool.Exec(ctx, `UPDATE users SET last_login_at = NOW() WHERE id = $1`, userID)
	return err
}

func (r *SessionRepository) CreateRefreshSession(ctx context.Context, userID, tokenHash, ua, ip string, expiresAt time.Time) (string, error) {
	id := uuid.NewString()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO refresh_sessions (id, user_id, token_hash, expires_at, user_agent, ip_address)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, id, userID, tokenHash, expiresAt, ua, ip)
	return id, err
}

func (r *SessionRepository) FindRefreshSession(ctx context.Context, tokenHash string) (sessionID, userID string, expiresAt time.Time, revoked bool, err error) {
	var revokedAt *time.Time
	err = r.pool.QueryRow(ctx, `
		SELECT id::text, user_id::text, expires_at, revoked_at
		FROM refresh_sessions
		WHERE token_hash = $1
	`, tokenHash).Scan(&sessionID, &userID, &expiresAt, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", time.Time{}, false, nil
	}
	if err != nil {
		return "", "", time.Time{}, false, err
	}
	return sessionID, userID, expiresAt, revokedAt != nil, nil
}

func (r *SessionRepository) RevokeRefreshSession(ctx context.Context, tokenHash string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE refresh_sessions SET revoked_at = NOW()
		WHERE token_hash = $1 AND revoked_at IS NULL
	`, tokenHash)
	return err
}

func (r *SessionRepository) RevokeAllUserSessions(ctx context.Context, userID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE refresh_sessions SET revoked_at = NOW()
		WHERE user_id = $1 AND revoked_at IS NULL
	`, userID)
	return err
}

func (r *SessionRepository) RotateRefreshSession(ctx context.Context, oldHash, newHash, ua, ip string, expiresAt time.Time) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var userID string
	err = tx.QueryRow(ctx, `
		UPDATE refresh_sessions SET revoked_at = NOW()
		WHERE token_hash = $1 AND revoked_at IS NULL
		RETURNING user_id::text
	`, oldHash).Scan(&userID)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO refresh_sessions (id, user_id, token_hash, expires_at, user_agent, ip_address)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, uuid.NewString(), userID, newHash, expiresAt, ua, ip)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
