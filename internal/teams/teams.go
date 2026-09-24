package teams

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
	"github.com/crm/backend/pkg/apperrors"
)

type Team struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	OwnerUserID   *string    `json:"ownerUserId"`
	OwnerName     *string    `json:"ownerName"`
	IsActive      bool       `json:"isActive"`
	MemberIDs     []string   `json:"memberIds"`
	MemberCount   int        `json:"memberCount"`
	DeactivatedAt *time.Time `json:"deactivatedAt"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

type CreateInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	OwnerUserID *string  `json:"ownerUserId"`
	MemberIDs   []string `json:"memberIds"`
}

type UpdateInput struct {
	Name        *string  `json:"name"`
	Description *string  `json:"description"`
	OwnerUserID *string  `json:"ownerUserId"`
	MemberIDs   []string `json:"memberIds"`
	IsActive    *bool    `json:"isActive"`
}

type ListFilter struct {
	Search   string
	IsActive *bool
	Limit    int
	Offset   int
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
		activeClause = " AND t.is_active = $2"
		args = append(args, *f.IsActive)
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
		SELECT t.id::text, t.name, t.description, t.owner_user_id::text, u.full_name, t.is_active,
		       t.deactivated_at, t.created_at, t.updated_at,
		       (SELECT COUNT(*) FROM team_members tm WHERE tm.team_id = t.id)
		FROM teams t
		LEFT JOIN users u ON u.id = t.owner_user_id
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
			&t.ID, &t.Name, &t.Description, &t.OwnerUserID, &t.OwnerName, &t.IsActive,
			&t.DeactivatedAt, &t.CreatedAt, &t.UpdatedAt, &t.MemberCount,
		); err != nil {
			return nil, 0, err
		}
		teams = append(teams, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	for i := range teams {
		ids, err := r.listMemberIDs(ctx, teams[i].ID)
		if err != nil {
			return nil, 0, err
		}
		teams[i].MemberIDs = ids
	}
	return teams, total, nil
}

func (r *Repository) Get(ctx context.Context, id string) (*Team, error) {
	var t Team
	err := r.pool.QueryRow(ctx, `
		SELECT t.id::text, t.name, t.description, t.owner_user_id::text, u.full_name, t.is_active,
		       t.deactivated_at, t.created_at, t.updated_at,
		       (SELECT COUNT(*) FROM team_members tm WHERE tm.team_id = t.id)
		FROM teams t
		LEFT JOIN users u ON u.id = t.owner_user_id
		WHERE t.id = $1
	`, id).Scan(
		&t.ID, &t.Name, &t.Description, &t.OwnerUserID, &t.OwnerName, &t.IsActive,
		&t.DeactivatedAt, &t.CreatedAt, &t.UpdatedAt, &t.MemberCount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ids, err := r.listMemberIDs(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	t.MemberIDs = ids
	return &t, nil
}

func (r *Repository) Create(ctx context.Context, in CreateInput) (*Team, error) {
	id := uuid.NewString()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO teams (id, name, description, owner_user_id)
		VALUES ($1, $2, $3, $4)
	`, id, strings.TrimSpace(in.Name), strings.TrimSpace(in.Description), in.OwnerUserID)
	if err != nil {
		if strings.Contains(err.Error(), "teams_name_unique") {
			return nil, apperrors.Conflict("team name already exists")
		}
		return nil, err
	}
	members := in.MemberIDs
	if in.OwnerUserID != nil {
		members = appendUnique(members, *in.OwnerUserID)
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
	owner := current.OwnerUserID
	if in.OwnerUserID != nil {
		if *in.OwnerUserID == "" {
			owner = nil
		} else {
			owner = in.OwnerUserID
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
		UPDATE teams SET name=$2, description=$3, owner_user_id=$4, is_active=$5, deactivated_at=$6
		WHERE id=$1
	`, id, name, desc, owner, isActive, deactivatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "teams_name_unique") {
			return nil, apperrors.Conflict("team name already exists")
		}
		return nil, err
	}
	if in.MemberIDs != nil {
		members := in.MemberIDs
		if owner != nil {
			members = appendUnique(members, *owner)
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

type Service struct {
	repo  *Repository
	audit *audit.Service
}

func NewService(repo *Repository, auditSvc *audit.Service) *Service {
	return &Service{repo: repo, audit: auditSvc}
}

func (s *Service) List(ctx context.Context, f ListFilter) ([]Team, int, error) {
	return s.repo.List(ctx, f)
}

func (s *Service) Get(ctx context.Context, id string) (*Team, error) {
	t, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, apperrors.Internal("failed to load team", err)
	}
	if t == nil {
		return nil, apperrors.NotFound("team not found")
	}
	return t, nil
}

func (s *Service) Create(ctx context.Context, actorID string, in CreateInput, ip, ua string) (*Team, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, apperrors.Validation("name is required")
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

func (s *Service) Update(ctx context.Context, actorID, id string, in UpdateInput, ip, ua string) (*Team, error) {
	before, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, apperrors.Internal("failed to load team", err)
	}
	if before == nil {
		return nil, apperrors.NotFound("team not found")
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
	if in.OwnerUserID != nil {
		prev := ""
		if before.OwnerUserID != nil {
			prev = *before.OwnerUserID
		}
		next := ""
		if team.OwnerUserID != nil {
			next = *team.OwnerUserID
		}
		if prev != next {
			action := "TEAM_LEAD_ASSIGNED"
			if prev != "" && next != "" {
				action = "TEAM_LEAD_CHANGED"
			}
			_ = s.audit.Record(ctx, audit.Ptr(actorID), action, "team", audit.Ptr(id), map[string]any{
				"previousOwnerUserId": prev,
				"newOwnerUserId":      next,
			}, ip, ua)
		}
	}
	return team, nil
}

func (s *Service) SetActive(ctx context.Context, actorID, id string, active bool, ip, ua string) (*Team, error) {
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
