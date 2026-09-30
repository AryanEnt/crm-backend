package users

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/crm/backend/internal/audit"
	"github.com/crm/backend/internal/auth"
	"github.com/crm/backend/internal/permissions"
	"github.com/crm/backend/pkg/apperrors"
)

type User struct {
	ID            string     `json:"id"`
	Email         string     `json:"email"`
	FullName      string     `json:"fullName"`
	Phone         string     `json:"phone"`
	RoleID        string     `json:"roleId"`
	RoleCode      string     `json:"roleCode"`
	RoleName      string     `json:"roleName"`
	IsActive      bool       `json:"isActive"`
	TeamIDs       []string   `json:"teamIds"`
	LastLoginAt   *time.Time `json:"lastLoginAt"`
	DeactivatedAt *time.Time `json:"deactivatedAt"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

type Role struct {
	ID          string   `json:"id"`
	Code        string   `json:"code"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

type CreateInput struct {
	Email    string   `json:"email"`
	Password string   `json:"password"`
	FullName string   `json:"fullName"`
	Phone    string   `json:"phone"`
	RoleID   string   `json:"roleId"`
	TeamIDs  []string `json:"teamIds"`
	TeamID   string   `json:"teamId"` // convenience single-team alias
	IsActive *bool    `json:"isActive"`
}

type UpdateInput struct {
	Email             *string  `json:"email"`
	FullName          *string  `json:"fullName"`
	Phone             *string  `json:"phone"`
	RoleID            *string  `json:"roleId"`
	Password          *string  `json:"password"`
	TeamIDs           []string `json:"teamIds"`
	TeamID            *string  `json:"teamId"`
	IsActive          *bool    `json:"isActive"`
	ConfirmTeamChange bool     `json:"confirmTeamChange"`
}

type ListFilter struct {
	Search              string
	RoleID              string
	RoleCode            string
	TeamID              string
	AvailableForTeamID  string // when set (or "__new__"), exclude SE/TL already on another team
	IsActive            *bool
	Limit               int
	Offset              int
	// Access bounds the rows returned; the zero value returns none.
	Access Access
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const noUserID = "00000000-0000-0000-0000-000000000000"

func (r *Repository) List(ctx context.Context, f ListFilter) ([]User, int, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 25
	}
	where := []string{
		"($1 = '' OR u.email ILIKE '%' || $1 || '%' OR u.full_name ILIKE '%' || $1 || '%')",
		"($2 = '' OR u.role_id::text = $2)",
		"($3 = '' OR r.code = $3)",
	}
	args := []any{f.Search, f.RoleID, f.RoleCode}
	if !f.Access.organization() {
		teamIDs := []string{}
		if f.Access.Scope == permissions.ScopeTeam {
			teamIDs = append(teamIDs, f.Access.TeamIDs...)
		}
		selfID := f.Access.UserID
		if selfID == "" {
			selfID = noUserID
		}
		args = append(args, selfID, teamIDs)
		self, teams := strconv.Itoa(len(args)-1), strconv.Itoa(len(args))
		where = append(where, "(u.id::text = $"+self+" OR EXISTS (SELECT 1 FROM team_members tm WHERE tm.user_id = u.id AND tm.team_id = ANY($"+teams+"::uuid[])))")
	}
	if f.IsActive != nil {
		args = append(args, *f.IsActive)
		where = append(where, "u.is_active = $"+strconv.Itoa(len(args)))
	}
	if f.TeamID != "" {
		args = append(args, f.TeamID)
		where = append(where, "EXISTS (SELECT 1 FROM team_members tm WHERE tm.user_id = u.id AND tm.team_id::text = $"+strconv.Itoa(len(args))+")")
	}
	if f.AvailableForTeamID != "" {
		// Exclude Super Admin from team assignment pickers entirely.
		where = append(where, "r.code <> 'super_admin'")
		// For SE / Team Lead: allow unassigned users, or members of this team.
		// AvailableForTeamID == "new" means create-team → unassigned only.
		exceptID := f.AvailableForTeamID
		if exceptID == "new" {
			exceptID = "00000000-0000-0000-0000-000000000000"
		}
		args = append(args, exceptID)
		n := strconv.Itoa(len(args))
		where = append(where, `
			(
				r.code NOT IN ('sales_executive', 'sales_manager')
				OR NOT EXISTS (
					SELECT 1 FROM team_members tm
					WHERE tm.user_id = u.id
					  AND tm.team_id::text <> $`+n+`
				)
				OR (
					$`+n+` <> '00000000-0000-0000-0000-000000000000'
					AND EXISTS (
						SELECT 1 FROM team_members tm
						WHERE tm.user_id = u.id
						  AND tm.team_id::text = $`+n+`
					)
				)
			)`)
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM users u
		JOIN roles r ON r.id = u.role_id
		WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, f.Limit, f.Offset)
	limitIdx := len(args) - 1
	offsetIdx := len(args)

	q := `
		SELECT u.id::text, u.email, u.full_name, COALESCE(u.phone,''), u.role_id::text, r.code, r.name, u.is_active,
		       u.last_login_at, u.deactivated_at, u.created_at, u.updated_at
		FROM users u
		JOIN roles r ON r.id = u.role_id
		WHERE ` + whereSQL + `
		ORDER BY u.created_at DESC
		LIMIT $` + strconv.Itoa(limitIdx) + ` OFFSET $` + strconv.Itoa(offsetIdx)

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(
			&u.ID, &u.Email, &u.FullName, &u.Phone, &u.RoleID, &u.RoleCode, &u.RoleName, &u.IsActive,
			&u.LastLoginAt, &u.DeactivatedAt, &u.CreatedAt, &u.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	for i := range users {
		ids, err := r.listTeamIDs(ctx, users[i].ID)
		if err != nil {
			return nil, 0, err
		}
		users[i].TeamIDs = ids
	}
	return users, total, nil
}

func (r *Repository) Get(ctx context.Context, id string) (*User, error) {
	const q = `
		SELECT u.id::text, u.email, u.full_name, COALESCE(u.phone,''), u.role_id::text, r.code, r.name, u.is_active,
		       u.last_login_at, u.deactivated_at, u.created_at, u.updated_at
		FROM users u
		JOIN roles r ON r.id = u.role_id
		WHERE u.id = $1
	`
	var u User
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&u.ID, &u.Email, &u.FullName, &u.Phone, &u.RoleID, &u.RoleCode, &u.RoleName, &u.IsActive,
		&u.LastLoginAt, &u.DeactivatedAt, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ids, err := r.listTeamIDs(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	u.TeamIDs = ids
	return &u, nil
}

func (r *Repository) Create(ctx context.Context, in CreateInput, passwordHash string) (*User, error) {
	id := uuid.NewString()
	email := strings.ToLower(strings.TrimSpace(in.Email))
	phone := strings.TrimSpace(in.Phone)
	isActive := true
	if in.IsActive != nil {
		isActive = *in.IsActive
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, full_name, phone, role_id, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, id, email, passwordHash, strings.TrimSpace(in.FullName), phone, in.RoleID, isActive)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, apperrors.Conflict("email already exists")
		}
		return nil, err
	}
	if err := r.replaceTeams(ctx, id, normalizeTeamIDs(in)); err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *Repository) Update(ctx context.Context, id string, in UpdateInput, passwordHash *string, teamIDs []string, replaceTeams bool) (*User, error) {
	current, err := r.Get(ctx, id)
	if err != nil || current == nil {
		return current, err
	}

	email := current.Email
	if in.Email != nil {
		email = strings.ToLower(strings.TrimSpace(*in.Email))
	}
	fullName := current.FullName
	if in.FullName != nil {
		fullName = strings.TrimSpace(*in.FullName)
	}
	phone := current.Phone
	if in.Phone != nil {
		phone = strings.TrimSpace(*in.Phone)
	}
	roleID := current.RoleID
	if in.RoleID != nil {
		roleID = *in.RoleID
	}
	isActive := current.IsActive
	var deactivatedAt any
	deactivatedAt = current.DeactivatedAt
	if in.IsActive != nil {
		isActive = *in.IsActive
		if !*in.IsActive {
			deactivatedAt = time.Now().UTC()
		} else {
			deactivatedAt = nil
		}
	}

	if passwordHash != nil {
		_, err = r.pool.Exec(ctx, `
			UPDATE users SET email=$2, full_name=$3, phone=$4, role_id=$5, is_active=$6, deactivated_at=$7, password_hash=$8
			WHERE id=$1
		`, id, email, fullName, phone, roleID, isActive, deactivatedAt, *passwordHash)
	} else {
		_, err = r.pool.Exec(ctx, `
			UPDATE users SET email=$2, full_name=$3, phone=$4, role_id=$5, is_active=$6, deactivated_at=$7
			WHERE id=$1
		`, id, email, fullName, phone, roleID, isActive, deactivatedAt)
	}
	if err != nil {
		if isUniqueViolation(err) {
			return nil, apperrors.Conflict("email already exists")
		}
		return nil, err
	}
	if replaceTeams {
		if err := r.replaceTeams(ctx, id, teamIDs); err != nil {
			return nil, err
		}
	}
	return r.Get(ctx, id)
}

func (r *Repository) SetActive(ctx context.Context, id string, active bool) (*User, error) {
	var deactivatedAt any
	if !active {
		deactivatedAt = time.Now().UTC()
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE users SET is_active=$2, deactivated_at=$3 WHERE id=$1
	`, id, active, deactivatedAt)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *Repository) BulkSetActive(ctx context.Context, ids []string, active bool) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	var deactivatedAt any
	if !active {
		deactivatedAt = time.Now().UTC()
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE users SET is_active=$2, deactivated_at=$3 WHERE id = ANY($1::uuid[])
	`, ids, active, deactivatedAt)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (r *Repository) ListRoles(ctx context.Context) ([]Role, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, code, name, description FROM roles ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []Role
	for rows.Next() {
		var role Role
		if err := rows.Scan(&role.ID, &role.Code, &role.Name, &role.Description); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range roles {
		perms, err := r.listRolePermissions(ctx, roles[i].ID)
		if err != nil {
			return nil, err
		}
		roles[i].Permissions = perms
	}
	return roles, nil
}

func (r *Repository) RoleExists(ctx context.Context, roleID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM roles WHERE id=$1)`, roleID).Scan(&exists)
	return exists, err
}

func (r *Repository) RoleCode(ctx context.Context, roleID string) (string, error) {
	var code string
	err := r.pool.QueryRow(ctx, `SELECT code FROM roles WHERE id=$1`, roleID).Scan(&code)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return code, err
}

type teamInfo struct {
	ID             string
	Name           string
	IsActive       bool
	TeamLeadUserID *string
	TeamLeadName   *string
}

func (r *Repository) getTeam(ctx context.Context, teamID string) (*teamInfo, error) {
	var t teamInfo
	err := r.pool.QueryRow(ctx, `
		SELECT t.id::text, t.name, t.is_active, t.team_lead_user_id::text, u.full_name
		FROM teams t
		LEFT JOIN users u ON u.id = t.team_lead_user_id
		WHERE t.id = $1
	`, teamID).Scan(&t.ID, &t.Name, &t.IsActive, &t.TeamLeadUserID, &t.TeamLeadName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *Repository) setTeamLead(ctx context.Context, teamID, userID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE teams SET team_lead_user_id = $2 WHERE id = $1
	`, teamID, userID)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO team_members (team_id, user_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, teamID, userID)
	return err
}

func (r *Repository) clearTeamLeadIf(ctx context.Context, teamID, userID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE teams SET team_lead_user_id = NULL
		WHERE id = $1 AND team_lead_user_id = $2
	`, teamID, userID)
	return err
}

func (r *Repository) EmailExists(ctx context.Context, email string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE email=$1)`, strings.ToLower(email)).Scan(&exists)
	return exists, err
}

func normalizeTeamIDs(in CreateInput) []string {
	ids := append([]string{}, in.TeamIDs...)
	if strings.TrimSpace(in.TeamID) != "" {
		ids = append(ids, strings.TrimSpace(in.TeamID))
	}
	return uniqueNonEmpty(ids)
}

func resolveUpdateTeamIDs(in UpdateInput, current []string) (ids []string, provided bool) {
	if in.TeamID != nil {
		provided = true
		if strings.TrimSpace(*in.TeamID) == "" {
			return []string{}, true
		}
		return []string{strings.TrimSpace(*in.TeamID)}, true
	}
	if in.TeamIDs != nil {
		return uniqueNonEmpty(in.TeamIDs), true
	}
	return current, false
}

func uniqueNonEmpty(ids []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func teamsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[string]struct{}{}
	for _, x := range a {
		set[x] = struct{}{}
	}
	for _, x := range b {
		if _, ok := set[x]; !ok {
			return false
		}
	}
	return true
}

func (r *Repository) listTeamIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT team_id::text FROM team_members WHERE user_id=$1`, userID)
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

func (r *Repository) replaceTeams(ctx context.Context, userID string, teamIDs []string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM team_members WHERE user_id=$1`, userID); err != nil {
		return err
	}
	for _, teamID := range teamIDs {
		if teamID == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO team_members (team_id, user_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, teamID, userID); err != nil {
			if msg := membershipConflictMessage(err); msg != "" {
				return apperrors.Conflict(normalizeUserMembershipConflict(msg))
			}
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *Repository) listRolePermissions(ctx context.Context, roleID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT p.code FROM role_permissions rp
		JOIN permissions p ON p.id = rp.permission_id
		WHERE rp.role_id=$1 ORDER BY p.code
	`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var codes []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		codes = append(codes, c)
	}
	return codes, rows.Err()
}

type Service struct {
	repo   *Repository
	hasher *auth.BcryptHasher
	audit  *audit.Service
	sess   *auth.SessionRepository
}

func NewService(repo *Repository, hasher *auth.BcryptHasher, auditSvc *audit.Service, sess *auth.SessionRepository) *Service {
	return &Service{repo: repo, hasher: hasher, audit: auditSvc, sess: sess}
}

func (s *Service) List(ctx context.Context, claims auth.Claims, f ListFilter) ([]User, int, error) {
	access, err := accessFor(claims, permissions.UsersView)
	if err != nil {
		return nil, 0, err
	}
	f.Access = access
	items, total, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, 0, apperrors.Internal("failed to list users", err)
	}
	return items, total, nil
}

func (s *Service) Get(ctx context.Context, claims auth.Claims, id string) (*User, error) {
	access, err := accessFor(claims, permissions.UsersView)
	if err != nil {
		return nil, err
	}
	u, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if !access.CanSee(u) {
		return nil, apperrors.NotFound("user not found")
	}
	return u, nil
}

func (s *Service) load(ctx context.Context, id string) (*User, error) {
	u, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, apperrors.Internal("failed to load user", err)
	}
	if u == nil {
		return nil, apperrors.NotFound("user not found")
	}
	return u, nil
}

func (s *Service) Create(ctx context.Context, claims auth.Claims, in CreateInput, ip, ua string) (*User, error) {
	access, err := accessFor(claims, permissions.UsersCreate)
	if err != nil {
		return nil, err
	}
	actorID := claims.UserID
	if err := validateCreate(in); err != nil {
		return nil, err
	}
	roleCode, err := s.repo.RoleCode(ctx, in.RoleID)
	if err != nil {
		return nil, apperrors.Internal("failed to validate role", err)
	}
	if roleCode == "" {
		return nil, apperrors.Validation("invalid role")
	}

	teamIDs, err := access.ResolveCreateTeams(roleCode, normalizeTeamIDs(in))
	if err != nil {
		return nil, err
	}
	in.TeamIDs = teamIDs
	in.TeamID = ""
	if err := s.validateRoleTeamAssignment(ctx, roleCode, teamIDs, ""); err != nil {
		return nil, err
	}

	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return nil, apperrors.Internal("failed to hash password", err)
	}
	user, err := s.repo.Create(ctx, in, hash)
	if err != nil {
		if ae, ok := apperrors.AsAppError(err); ok {
			return nil, ae
		}
		return nil, apperrors.Internal("failed to create user", err)
	}

	if roleCode == permissions.RoleSalesManager && len(teamIDs) == 1 {
		prev, _ := s.repo.getTeam(ctx, teamIDs[0])
		if err := s.repo.setTeamLead(ctx, teamIDs[0], user.ID); err != nil {
			return nil, apperrors.Internal("failed to assign team lead", err)
		}
		_ = s.audit.Record(ctx, audit.Ptr(actorID), "TEAM_LEAD_ASSIGNED", "team", audit.Ptr(teamIDs[0]), map[string]any{
			"userId": user.ID, "previousTeamLeadUserId": prevTeamLeadID(prev),
		}, ip, ua)
	}

	action := "user.created"
	meta := map[string]any{"email": user.Email, "role": user.RoleCode, "teamIds": user.TeamIDs}
	if roleCode == permissions.RoleSalesExecutive {
		action = "SE_CREATED"
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), action, "user", audit.Ptr(user.ID), meta, ip, ua)
	return user, nil
}

func (s *Service) Update(ctx context.Context, claims auth.Claims, id string, in UpdateInput, ip, ua string) (*User, error) {
	access, err := accessFor(claims, permissions.UsersEdit, permissions.UsersAssign)
	if err != nil {
		return nil, err
	}
	actorID := claims.UserID
	current, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.IsActive != nil && *in.IsActive != current.IsActive {
		statusAccess, err := accessFor(claims, permissions.UsersDelete)
		if err != nil {
			return nil, apperrors.Forbidden("You don't have permission to activate or deactivate users")
		}
		if err := statusAccess.requireManage(current); err != nil {
			return nil, err
		}
	}

	var hash *string
	if in.Password != nil && *in.Password != "" {
		if len(*in.Password) < 8 {
			return nil, apperrors.Validation("password must be at least 8 characters")
		}
		h, err := s.hasher.Hash(*in.Password)
		if err != nil {
			return nil, apperrors.Internal("failed to hash password", err)
		}
		hash = &h
	}

	roleID := current.RoleID
	roleCode := current.RoleCode
	if in.RoleID != nil {
		roleID = *in.RoleID
		code, err := s.repo.RoleCode(ctx, roleID)
		if err != nil {
			return nil, apperrors.Internal("failed to validate role", err)
		}
		if code == "" {
			return nil, apperrors.Validation("invalid role")
		}
		roleCode = code
	}

	teamIDs, teamsProvided := resolveUpdateTeamIDs(in, current.TeamIDs)
	if err := access.AuthorizeUpdate(current, in, teamIDs, teamsProvided); err != nil {
		return nil, err
	}
	if roleCode == permissions.RoleSuperAdmin {
		teamIDs = nil
		teamsProvided = true
	}
	if teamsProvided || in.RoleID != nil {
		if err := s.validateRoleTeamAssignment(ctx, roleCode, teamIDs, id); err != nil {
			return nil, err
		}
	}

	teamChanged := teamsProvided && !teamsEqual(current.TeamIDs, teamIDs)
	if teamChanged && (roleCode == permissions.RoleSalesExecutive || roleCode == permissions.RoleSalesManager) && !in.ConfirmTeamChange {
		prevName, newName := teamNames(ctx, s.repo, current.TeamIDs, teamIDs)
		who := "Sales Executive"
		if roleCode == permissions.RoleSalesManager {
			who = "Team Lead"
		}
		return nil, apperrors.ValidationDetails(
			"Confirm team change before moving this "+who,
			map[string]any{
				"field":            "team_id",
				"message":          "Team change requires confirmation",
				"requiresConfirm":  true,
				"previousTeamIds":  current.TeamIDs,
				"previousTeamName": prevName,
				"newTeamIds":       teamIDs,
				"newTeamName":      newName,
			},
		)
	}

	user, err := s.repo.Update(ctx, id, in, hash, teamIDs, teamsProvided)
	if err != nil {
		if ae, ok := apperrors.AsAppError(err); ok {
			return nil, ae
		}
		return nil, apperrors.Internal("failed to update user", err)
	}
	if user == nil {
		return nil, apperrors.NotFound("user not found")
	}

	if teamChanged {
		for _, tid := range current.TeamIDs {
			_ = s.repo.clearTeamLeadIf(ctx, tid, id)
		}
		if roleCode == permissions.RoleSalesManager && len(teamIDs) == 1 {
			prev, _ := s.repo.getTeam(ctx, teamIDs[0])
			_ = s.repo.setTeamLead(ctx, teamIDs[0], id)
			_ = s.audit.Record(ctx, audit.Ptr(actorID), "TEAM_LEAD_CHANGED", "team", audit.Ptr(teamIDs[0]), map[string]any{
				"userId": id, "previousTeamLeadUserId": prevTeamLeadID(prev),
			}, ip, ua)
		}
		_ = s.audit.Record(ctx, audit.Ptr(actorID), "SE_TEAM_CHANGED", "user", audit.Ptr(id), map[string]any{
			"previousTeamIds": current.TeamIDs,
			"newTeamIds":      teamIDs,
			"role":            roleCode,
		}, ip, ua)
	} else if roleCode == permissions.RoleSalesManager && teamsProvided && len(teamIDs) == 1 {
		prev, _ := s.repo.getTeam(ctx, teamIDs[0])
		if prev == nil || prev.TeamLeadUserID == nil || *prev.TeamLeadUserID != id {
			_ = s.repo.setTeamLead(ctx, teamIDs[0], id)
			_ = s.audit.Record(ctx, audit.Ptr(actorID), "TEAM_LEAD_ASSIGNED", "team", audit.Ptr(teamIDs[0]), map[string]any{
				"userId": id, "previousTeamLeadUserId": prevTeamLeadID(prev),
			}, ip, ua)
		}
	}

	if in.IsActive != nil && !*in.IsActive {
		_ = s.sess.RevokeAllUserSessions(ctx, id)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "user.updated", "user", audit.Ptr(id), map[string]any{
		"email": user.Email, "isActive": user.IsActive, "role": user.RoleCode, "teamIds": user.TeamIDs,
	}, ip, ua)
	return user, nil
}

func (s *Service) SetActive(ctx context.Context, claims auth.Claims, id string, active bool, ip, ua string) (*User, error) {
	access, err := accessFor(claims, permissions.UsersDelete)
	if err != nil {
		return nil, err
	}
	actorID := claims.UserID
	current, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := access.requireManage(current); err != nil {
		return nil, err
	}
	user, err := s.repo.SetActive(ctx, id, active)
	if err != nil {
		return nil, apperrors.Internal("failed to update user status", err)
	}
	if user == nil {
		return nil, apperrors.NotFound("user not found")
	}
	if !active {
		_ = s.sess.RevokeAllUserSessions(ctx, id)
	}
	action := "user.activated"
	if !active {
		action = "user.deactivated"
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), action, "user", audit.Ptr(id), nil, ip, ua)
	return user, nil
}

func (s *Service) BulkSetActive(ctx context.Context, claims auth.Claims, ids []string, active bool, ip, ua string) (int, error) {
	access, err := accessFor(claims, permissions.UsersDelete)
	if err != nil {
		return 0, err
	}
	actorID := claims.UserID
	if !access.organization() {
		for _, id := range ids {
			current, err := s.load(ctx, id)
			if err != nil {
				return 0, err
			}
			if err := access.requireManage(current); err != nil {
				return 0, err
			}
		}
	}
	n, err := s.repo.BulkSetActive(ctx, ids, active)
	if err != nil {
		return 0, apperrors.Internal("failed to update users", err)
	}
	if !active {
		for _, id := range ids {
			_ = s.sess.RevokeAllUserSessions(ctx, id)
		}
	}
	action := "users.bulk_activated"
	if !active {
		action = "users.bulk_deactivated"
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), action, "user", nil, map[string]any{"ids": ids, "count": n}, ip, ua)
	return n, nil
}

func (s *Service) ListRoles(ctx context.Context) ([]Role, error) {
	roles, err := s.repo.ListRoles(ctx)
	if err != nil {
		return nil, apperrors.Internal("failed to list roles", err)
	}
	for i := range roles {
		roles[i].Permissions = permissions.ExpandImplies(roles[i].Permissions).List()
	}
	return roles, nil
}

func (s *Service) validateRoleTeamAssignment(ctx context.Context, roleCode string, teamIDs []string, excludeUserID string) error {
	if roleCode == permissions.RoleSuperAdmin {
		if len(teamIDs) > 0 {
			return apperrors.Validation("Super Admin is not assigned to a team")
		}
		return nil
	}
	requiresTeam := roleCode == permissions.RoleSalesExecutive || roleCode == permissions.RoleSalesManager
	if !requiresTeam {
		return nil
	}
	if len(teamIDs) == 0 {
		msg := "Team is required for Sales Executives"
		if roleCode == permissions.RoleSalesManager {
			msg = "Team is required for Team Leads"
		}
		return apperrors.FieldValidation("team_id", msg)
	}
	if len(teamIDs) > 1 {
		msg := "A Sales Executive belongs to exactly one team"
		if roleCode == permissions.RoleSalesManager {
			msg = "A Team Lead belongs to exactly one team"
		}
		return apperrors.FieldValidation("team_id", msg)
	}
	team, err := s.repo.getTeam(ctx, teamIDs[0])
	if err != nil {
		return apperrors.Internal("failed to validate team", err)
	}
	if team == nil {
		return apperrors.FieldValidation("team_id", "Selected team does not exist")
	}
	if !team.IsActive {
		return apperrors.FieldValidation("team_id", "Selected team is inactive")
	}
	if roleCode == permissions.RoleSalesExecutive {
		if team.TeamLeadUserID == nil || *team.TeamLeadUserID == "" {
			return apperrors.FieldValidation("team_id", "Selected team does not have a Team Lead configured")
		}
	}
	if roleCode == permissions.RoleSalesManager {
		if team.TeamLeadUserID != nil && *team.TeamLeadUserID != "" && *team.TeamLeadUserID != excludeUserID {
			return apperrors.FieldValidation("team_id", "Team already has a Team Lead assigned")
		}
	}
	return nil
}

func prevTeamLeadID(t *teamInfo) any {
	if t == nil || t.TeamLeadUserID == nil {
		return nil
	}
	return *t.TeamLeadUserID
}

func teamNames(ctx context.Context, repo *Repository, prev, next []string) (string, string) {
	prevName, newName := "", ""
	if len(prev) > 0 {
		if t, _ := repo.getTeam(ctx, prev[0]); t != nil {
			prevName = t.Name
		}
	}
	if len(next) > 0 {
		if t, _ := repo.getTeam(ctx, next[0]); t != nil {
			newName = t.Name
		}
	}
	return prevName, newName
}

func validateCreate(in CreateInput) error {
	if strings.TrimSpace(in.Email) == "" {
		return apperrors.Validation("email is required")
	}
	if strings.TrimSpace(in.FullName) == "" {
		return apperrors.Validation("full name is required")
	}
	if len(in.Password) < 8 {
		return apperrors.Validation("password must be at least 8 characters")
	}
	if strings.TrimSpace(in.RoleID) == "" {
		return apperrors.Validation("role is required")
	}
	return nil
}

func membershipConflictMessage(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if !strings.Contains(msg, "already assigned to") && !strings.Contains(msg, "Super Admin cannot") {
		return ""
	}
	if i := strings.Index(msg, "ERROR:"); i >= 0 {
		msg = strings.TrimSpace(msg[i+len("ERROR:"):])
	}
	if i := strings.Index(msg, " (SQLSTATE"); i >= 0 {
		msg = strings.TrimSpace(msg[:i])
	}
	return msg
}

func normalizeUserMembershipConflict(msg string) string {
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "super admin") {
		return "Super Admin cannot be a team member or Team Lead"
	}
	if strings.Contains(lower, "team lead") || strings.Contains(lower, "sales_manager") {
		return "Team Lead is already assigned to another team."
	}
	return "Sales Executive is already assigned to another team."
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "users_email_unique")
}
