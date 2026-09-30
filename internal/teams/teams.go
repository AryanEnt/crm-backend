package teams

import (
	"context"
	"errors"
	"fmt"
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

type TeamMember struct {
	ID       string `json:"id"`
	FullName string `json:"fullName"`
	Email    string `json:"email"`
	RoleCode string `json:"roleCode"`
	RoleName string `json:"roleName"`
}

type Team struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	Description    string       `json:"description"`
	TeamLeadUserID *string      `json:"teamLeadUserId"`
	TeamLeadName   *string      `json:"teamLeadName"`
	IsActive       bool         `json:"isActive"`
	MemberIDs      []string     `json:"memberIds"`
	Members        []TeamMember `json:"members"`
	MemberCount    int          `json:"memberCount"`
	DeactivatedAt  *time.Time   `json:"deactivatedAt"`
	CreatedAt      time.Time    `json:"createdAt"`
	UpdatedAt      time.Time    `json:"updatedAt"`
}

type CreateInput struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	TeamLeadUserID *string  `json:"teamLeadUserId"`
	OwnerUserID    *string  `json:"ownerUserId"`
	MemberIDs      []string `json:"memberIds"`
}

type UpdateInput struct {
	Name           *string  `json:"name"`
	Description    *string  `json:"description"`
	TeamLeadUserID *string  `json:"teamLeadUserId"`
	OwnerUserID    *string  `json:"ownerUserId"`
	MemberIDs      []string `json:"memberIds"`
	IsActive       *bool    `json:"isActive"`
}

type ListFilter struct {
	Search   string
	IsActive *bool
	Limit    int
	Offset   int
	// Access bounds the rows returned; the zero value returns none.
	Access Access
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) List(ctx context.Context, f ListFilter) ([]Team, int, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 25
	}
	activeClause := ""
	args := []any{f.Search}
	if f.IsActive != nil {
		args = append(args, *f.IsActive)
		activeClause = " AND t.is_active = $" + strconv.Itoa(len(args))
	}
	if !f.Access.organization() {
		teamIDs := []string{}
		if f.Access.Scope == permissions.ScopeTeam || f.Access.Scope == permissions.ScopeOwn {
			teamIDs = append(teamIDs, f.Access.TeamIDs...)
		}
		args = append(args, teamIDs)
		activeClause += " AND t.id = ANY($" + strconv.Itoa(len(args)) + "::uuid[])"
	}
	var total int
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM teams t
		WHERE ($1 = '' OR t.name ILIKE '%' || $1 || '%' OR t.description ILIKE '%' || $1 || '%')`+activeClause,
		args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limitIdx := len(args) + 1
	offsetIdx := len(args) + 2
	args = append(args, f.Limit, f.Offset)
	rows, err := r.pool.Query(ctx, `
		SELECT t.id::text, t.name, t.description, t.team_lead_user_id::text, u.full_name, t.is_active,
		       t.deactivated_at, t.created_at, t.updated_at,
		       (SELECT COUNT(*) FROM team_members tm WHERE tm.team_id = t.id)
		FROM teams t
		LEFT JOIN users u ON u.id = t.team_lead_user_id
		WHERE ($1 = '' OR t.name ILIKE '%' || $1 || '%' OR t.description ILIKE '%' || $1 || '%')`+activeClause+`
		ORDER BY t.created_at DESC
		LIMIT $`+strconv.Itoa(limitIdx)+` OFFSET $`+strconv.Itoa(offsetIdx), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var teams []Team
	for rows.Next() {
		var t Team
		if err := rows.Scan(
			&t.ID, &t.Name, &t.Description, &t.TeamLeadUserID, &t.TeamLeadName, &t.IsActive,
			&t.DeactivatedAt, &t.CreatedAt, &t.UpdatedAt, &t.MemberCount,
		); err != nil {
			return nil, 0, err
		}
		teams = append(teams, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := r.attachMembers(ctx, teams); err != nil {
		return nil, 0, err
	}
	return teams, total, nil
}

func (r *Repository) Get(ctx context.Context, id string) (*Team, error) {
	var t Team
	err := r.pool.QueryRow(ctx, `
		SELECT t.id::text, t.name, t.description, t.team_lead_user_id::text, u.full_name, t.is_active,
		       t.deactivated_at, t.created_at, t.updated_at,
		       (SELECT COUNT(*) FROM team_members tm WHERE tm.team_id = t.id)
		FROM teams t
		LEFT JOIN users u ON u.id = t.team_lead_user_id
		WHERE t.id = $1
	`, id).Scan(
		&t.ID, &t.Name, &t.Description, &t.TeamLeadUserID, &t.TeamLeadName, &t.IsActive,
		&t.DeactivatedAt, &t.CreatedAt, &t.UpdatedAt, &t.MemberCount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	loaded := []Team{t}
	if err := r.attachMembers(ctx, loaded); err != nil {
		return nil, err
	}
	return &loaded[0], nil
}

func (r *Repository) Create(ctx context.Context, in CreateInput) (*Team, error) {
	id := uuid.NewString()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO teams (id, name, description, team_lead_user_id)
		VALUES ($1, $2, $3, $4)
	`, id, strings.TrimSpace(in.Name), strings.TrimSpace(in.Description), in.TeamLeadUserID)
	if err != nil {
		if strings.Contains(err.Error(), "teams_name_unique") {
			return nil, apperrors.Conflict("team name already exists")
		}
		return nil, err
	}
	members := in.MemberIDs
	if in.TeamLeadUserID != nil && strings.TrimSpace(*in.TeamLeadUserID) != "" {
		members = appendUnique(members, strings.TrimSpace(*in.TeamLeadUserID))
	}
	if err := r.replaceMembers(ctx, id, members); err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *Repository) Update(ctx context.Context, id string, in UpdateInput) (*Team, error) {
	current, err := r.Get(ctx, id)
	if err != nil || current == nil {
		return current, err
	}
	name := current.Name
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	desc := current.Description
	if in.Description != nil {
		desc = strings.TrimSpace(*in.Description)
	}
	lead := current.TeamLeadUserID
	if in.TeamLeadUserID != nil {
		if strings.TrimSpace(*in.TeamLeadUserID) == "" {
			lead = nil
		} else {
			trimmed := strings.TrimSpace(*in.TeamLeadUserID)
			lead = &trimmed
		}
	}
	isActive := current.IsActive
	var deactivatedAt any = current.DeactivatedAt
	if in.IsActive != nil {
		isActive = *in.IsActive
		if !*in.IsActive {
			deactivatedAt = time.Now().UTC()
		} else {
			deactivatedAt = nil
		}
	}
	_, err = r.pool.Exec(ctx, `
		UPDATE teams SET name=$2, description=$3, team_lead_user_id=$4, is_active=$5, deactivated_at=$6
		WHERE id=$1
	`, id, name, desc, lead, isActive, deactivatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "teams_name_unique") {
			return nil, apperrors.Conflict("team name already exists")
		}
		return nil, err
	}
	if in.MemberIDs != nil {
		members := in.MemberIDs
		if lead != nil {
			members = appendUnique(members, *lead)
		}
		if err := r.replaceMembers(ctx, id, members); err != nil {
			return nil, err
		}
	}
	return r.Get(ctx, id)
}

func (r *Repository) SetActive(ctx context.Context, id string, active bool) (*Team, error) {
	var deactivatedAt any
	if !active {
		deactivatedAt = time.Now().UTC()
	}
	_, err := r.pool.Exec(ctx, `UPDATE teams SET is_active=$2, deactivated_at=$3 WHERE id=$1`, id, active, deactivatedAt)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *Repository) attachMembers(ctx context.Context, teams []Team) error {
	if len(teams) == 0 {
		return nil
	}
	ids := make([]string, len(teams))
	index := map[string]int{}
	for i := range teams {
		ids[i] = teams[i].ID
		index[teams[i].ID] = i
		teams[i].MemberIDs = []string{}
		teams[i].Members = []TeamMember{}
	}
	rows, err := r.pool.Query(ctx, `
		SELECT tm.team_id::text, u.id::text, u.full_name, u.email, r.code, r.name
		FROM team_members tm
		JOIN users u ON u.id = tm.user_id
		JOIN roles r ON r.id = u.role_id
		WHERE tm.team_id = ANY($1::uuid[])
		  AND r.code <> 'super_admin'
		ORDER BY u.full_name
	`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var teamID string
		var member TeamMember
		if err := rows.Scan(&teamID, &member.ID, &member.FullName, &member.Email, &member.RoleCode, &member.RoleName); err != nil {
			return err
		}
		i, ok := index[teamID]
		if !ok {
			continue
		}
		teams[i].Members = append(teams[i].Members, member)
		teams[i].MemberIDs = append(teams[i].MemberIDs, member.ID)
	}
	return rows.Err()
}

func (r *Repository) listMemberIDs(ctx context.Context, teamID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT user_id::text FROM team_members WHERE team_id=$1`, teamID)
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

func (r *Repository) replaceMembers(ctx context.Context, teamID string, memberIDs []string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM team_members WHERE team_id=$1`, teamID); err != nil {
		return err
	}
	for _, uid := range memberIDs {
		if uid == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO team_members (team_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING
		`, teamID, uid); err != nil {
			if msg := membershipConflict(err); msg != "" {
				return apperrors.Conflict(normalizeMembershipConflict(msg))
			}
			return err
		}
	}
	return tx.Commit(ctx)
}

func appendUnique(ids []string, id string) []string {
	for _, existing := range ids {
		if existing == id {
			return ids
		}
	}
	return append(ids, id)
}

func rejectOwnerField(owner *string) error {
	if owner != nil && strings.TrimSpace(*owner) != "" {
		return apperrors.Validation("teams do not have an owner; assign a Team Lead")
	}
	return nil
}

func (r *Repository) userRoles(ctx context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT u.id::text, r.code
		FROM users u
		JOIN roles r ON r.id = u.role_id
		WHERE u.id = ANY($1::uuid[])
	`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, code string
		if err := rows.Scan(&id, &code); err != nil {
			return nil, err
		}
		out[id] = code
	}
	return out, rows.Err()
}

func (s *Service) normalizeMembership(ctx context.Context, lead *string, memberIDs []string) (*string, []string, error) {
	leadID := ""
	if lead != nil {
		leadID = strings.TrimSpace(*lead)
	}
	seen := map[string]struct{}{}
	members := make([]string, 0, len(memberIDs)+1)
	for _, id := range memberIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		members = append(members, id)
	}
	if leadID != "" {
		if _, ok := seen[leadID]; !ok {
			members = append(members, leadID)
		}
	}
	roles, err := s.repo.userRoles(ctx, members)
	if err != nil {
		return nil, nil, apperrors.Internal("failed to check team members", err)
	}
	for _, id := range members {
		code, ok := roles[id]
		if !ok {
			return nil, nil, apperrors.Validation("one or more users do not exist")
		}
		if code == permissions.RoleSuperAdmin {
			return nil, nil, apperrors.Validation("Super Admin cannot be a team member or Team Lead")
		}
	}
	if leadID != "" && roles[leadID] != permissions.RoleSalesManager {
		return nil, nil, apperrors.Validation("Team Lead must be a user with the Team Lead role")
	}
	for _, id := range members {
		if roles[id] == permissions.RoleSalesManager && id != leadID {
			return nil, nil, apperrors.Validation("assign the Team Lead in Team Lead, not as a member")
		}
	}
	if leadID == "" {
		return nil, members, nil
	}
	return &leadID, members, nil
}

func (s *Service) ensureSingleTeam(ctx context.Context, lead *string, exceptTeamID string) error {
	if lead == nil || strings.TrimSpace(*lead) == "" {
		return nil
	}
	var other string
	err := s.repo.pool.QueryRow(ctx, `
		SELECT id::text FROM teams
		WHERE team_lead_user_id = $1 AND id::text <> $2
		LIMIT 1
	`, strings.TrimSpace(*lead), exceptTeamID).Scan(&other)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return apperrors.Internal("failed to check Team Lead assignment", err)
	}
	return apperrors.Conflict("Team Lead is already assigned to another team.")
}

func membershipConflict(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "already assigned to") || strings.Contains(msg, "Super Admin cannot") {
		if i := strings.Index(msg, "ERROR:"); i >= 0 {
			msg = strings.TrimSpace(msg[i+len("ERROR:"):])
		}
		if i := strings.Index(msg, " (SQLSTATE"); i >= 0 {
			msg = strings.TrimSpace(msg[:i])
		}
		return msg
	}
	return ""
}

func normalizeMembershipConflict(msg string) string {
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "super admin") {
		return "Super Admin cannot be a team member or Team Lead"
	}
	if strings.Contains(lower, "team lead") || strings.Contains(lower, "sales_manager") {
		return "Team Lead is already assigned to another team."
	}
	return "Sales Executive is already assigned to another team."
}

func (s *Service) rejectCrossTeamAssignments(ctx context.Context, userIDs []string, exceptTeamID string) error {
	if len(userIDs) == 0 {
		return nil
	}
	rows, err := s.repo.pool.Query(ctx, `
		SELECT u.full_name, r.code, t.name
		FROM team_members tm
		JOIN users u ON u.id = tm.user_id
		JOIN roles r ON r.id = u.role_id
		JOIN teams t ON t.id = tm.team_id
		WHERE tm.user_id = ANY($1::uuid[])
		  AND tm.team_id::text <> $2
		  AND r.code IN ('sales_executive', 'sales_manager')
		ORDER BY u.full_name
	`, userIDs, exceptTeamID)
	if err != nil {
		return apperrors.Internal("failed to check team membership", err)
	}
	defer rows.Close()
	var conflicts []string
	for rows.Next() {
		var name, roleCode, teamName string
		if err := rows.Scan(&name, &roleCode, &teamName); err != nil {
			return apperrors.Internal("failed to check team membership", err)
		}
		who := "Sales Executive"
		if roleCode == permissions.RoleSalesManager {
			who = "Team Lead"
		}
		conflicts = append(conflicts, fmt.Sprintf("%s is already assigned to another team.", who))
		_ = name
		_ = teamName
	}
	if err := rows.Err(); err != nil {
		return apperrors.Internal("failed to check team membership", err)
	}
	if len(conflicts) == 0 {
		return nil
	}
	// Deduplicate identical generic messages.
	seen := map[string]struct{}{}
	var unique []string
	for _, c := range conflicts {
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		unique = append(unique, c)
	}
	return apperrors.Conflict(strings.Join(unique, " "))
}

type Service struct {
	repo  *Repository
	audit *audit.Service
}

func NewService(repo *Repository, auditSvc *audit.Service) *Service {
	return &Service{repo: repo, audit: auditSvc}
}

func (s *Service) List(ctx context.Context, claims auth.Claims, f ListFilter) ([]Team, int, error) {
	access, err := accessFor(claims, permissions.TeamsView)
	if err != nil {
		return nil, 0, err
	}
	f.Access = access
	items, total, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, 0, apperrors.Internal("failed to list teams", err)
	}
	for i := range items {
		access.Redact(&items[i])
	}
	return items, total, nil
}

func (s *Service) Get(ctx context.Context, claims auth.Claims, id string) (*Team, error) {
	access, err := accessFor(claims, permissions.TeamsView)
	if err != nil {
		return nil, err
	}
	if !access.CanSee(id) {
		return nil, apperrors.NotFound("team not found")
	}
	t, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, apperrors.Internal("failed to load team", err)
	}
	if t == nil {
		return nil, apperrors.NotFound("team not found")
	}
	access.Redact(t)
	return t, nil
}

func (s *Service) Create(ctx context.Context, claims auth.Claims, in CreateInput, ip, ua string) (*Team, error) {
	access, err := accessFor(claims, permissions.TeamsCreate)
	if err != nil {
		return nil, err
	}
	if err := access.requireOrganization(); err != nil {
		return nil, err
	}
	actorID := claims.UserID
	if strings.TrimSpace(in.Name) == "" {
		return nil, apperrors.Validation("name is required")
	}
	if err := rejectOwnerField(in.OwnerUserID); err != nil {
		return nil, err
	}
	lead, members, err := s.normalizeMembership(ctx, in.TeamLeadUserID, in.MemberIDs)
	if err != nil {
		return nil, err
	}
	in.TeamLeadUserID = lead
	in.MemberIDs = members
	if err := s.ensureSingleTeam(ctx, lead, ""); err != nil {
		return nil, err
	}
	checkIDs := append([]string{}, members...)
	if lead != nil && strings.TrimSpace(*lead) != "" {
		checkIDs = appendUnique(checkIDs, strings.TrimSpace(*lead))
	}
	if err := s.rejectCrossTeamAssignments(ctx, checkIDs, ""); err != nil {
		return nil, err
	}
	team, err := s.repo.Create(ctx, in)
	if err != nil {
		if ae, ok := apperrors.AsAppError(err); ok {
			return nil, ae
		}
		return nil, apperrors.Internal("failed to create team", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "team.created", "team", audit.Ptr(team.ID), map[string]any{
		"name": team.Name,
	}, ip, ua)
	return team, nil
}

func (s *Service) Update(ctx context.Context, claims auth.Claims, id string, in UpdateInput, ip, ua string) (*Team, error) {
	access, err := accessFor(claims, permissions.TeamsEdit, permissions.TeamsAssign)
	if err != nil {
		return nil, err
	}
	if !access.CanSee(id) {
		return nil, apperrors.NotFound("team not found")
	}
	if err := access.requireOrganization(); err != nil {
		return nil, err
	}
	actorID := claims.UserID
	before, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, apperrors.Internal("failed to load team", err)
	}
	if before == nil {
		return nil, apperrors.NotFound("team not found")
	}
	if err := rejectOwnerField(in.OwnerUserID); err != nil {
		return nil, err
	}
	if in.TeamLeadUserID != nil || in.MemberIDs != nil {
		lead := in.TeamLeadUserID
		if lead == nil {
			lead = before.TeamLeadUserID
		}
		members := in.MemberIDs
		if members == nil {
			members = before.MemberIDs
		}
		normalizedLead, normalizedMembers, err := s.normalizeMembership(ctx, lead, members)
		if err != nil {
			return nil, err
		}
		in.TeamLeadUserID = normalizedLead
		if normalizedLead == nil {
			empty := ""
			in.TeamLeadUserID = &empty
		}
		in.MemberIDs = normalizedMembers
		if err := s.ensureSingleTeam(ctx, normalizedLead, id); err != nil {
			return nil, err
		}
		checkIDs := append([]string{}, normalizedMembers...)
		if normalizedLead != nil && strings.TrimSpace(*normalizedLead) != "" {
			checkIDs = appendUnique(checkIDs, strings.TrimSpace(*normalizedLead))
		}
		if err := s.rejectCrossTeamAssignments(ctx, checkIDs, id); err != nil {
			return nil, err
		}
	}
	team, err := s.repo.Update(ctx, id, in)
	if err != nil {
		if ae, ok := apperrors.AsAppError(err); ok {
			return nil, ae
		}
		return nil, apperrors.Internal("failed to update team", err)
	}
	if team == nil {
		return nil, apperrors.NotFound("team not found")
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "team.updated", "team", audit.Ptr(id), map[string]any{
		"name": team.Name, "isActive": team.IsActive,
	}, ip, ua)
	if in.TeamLeadUserID != nil {
		prev := ""
		if before.TeamLeadUserID != nil {
			prev = *before.TeamLeadUserID
		}
		next := ""
		if team.TeamLeadUserID != nil {
			next = *team.TeamLeadUserID
		}
		if prev != next {
			action := "TEAM_LEAD_ASSIGNED"
			if prev != "" && next != "" {
				action = "TEAM_LEAD_CHANGED"
			}
			_ = s.audit.Record(ctx, audit.Ptr(actorID), action, "team", audit.Ptr(id), map[string]any{
				"previousTeamLeadUserId": prev,
				"teamLeadUserId":         next,
			}, ip, ua)
		}
	}
	return team, nil
}

func (s *Service) SetActive(ctx context.Context, claims auth.Claims, id string, active bool, ip, ua string) (*Team, error) {
	access, err := accessFor(claims, permissions.TeamsDelete)
	if err != nil {
		return nil, err
	}
	if !access.CanSee(id) {
		return nil, apperrors.NotFound("team not found")
	}
	if err := access.requireOrganization(); err != nil {
		return nil, err
	}
	actorID := claims.UserID
	team, err := s.repo.SetActive(ctx, id, active)
	if err != nil {
		return nil, apperrors.Internal("failed to update team status", err)
	}
	if team == nil {
		return nil, apperrors.NotFound("team not found")
	}
	action := "team.activated"
	if !active {
		action = "team.deactivated"
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), action, "team", audit.Ptr(id), nil, ip, ua)
	return team, nil
}
