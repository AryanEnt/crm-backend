package leadsources

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/crm/backend/internal/audit"
	apperrors "github.com/crm/backend/pkg/apperrors"
)

type Source struct {
	ID          string    `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsActive    bool      `json:"isActive"`
	Position    int       `json:"position"`
	UsageCount  int       `json:"usageCount"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type CreateInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IsActive    *bool  `json:"isActive"`
	Position    *int   `json:"position"`
}

type UpdateInput struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	IsActive    *bool   `json:"isActive"`
	Position    *int    `json:"position"`
}

type Service struct {
	pool  *pgxpool.Pool
	audit *audit.Service
}

func NewService(pool *pgxpool.Pool, auditSvc *audit.Service) *Service {
	return &Service{pool: pool, audit: auditSvc}
}

func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, " ", "_")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" {
		out = "source"
	}
	return out
}

func (s *Service) List(ctx context.Context, activeOnly bool, q string) ([]Source, error) {
	sql := `
		SELECT ls.id::text, ls.code, ls.name, ls.description, ls.is_active, ls.position,
			(SELECT COUNT(*)::int FROM leads l WHERE lower(l.source) = lower(ls.name))
			+ (SELECT COUNT(*)::int FROM customers c WHERE lower(c.source) = lower(ls.name)) AS usage,
			ls.created_at, ls.updated_at
		FROM lead_sources ls
		WHERE ($1::bool = false OR ls.is_active = true)
		  AND ($2 = '' OR ls.name ILIKE '%'||$2||'%' OR ls.code ILIKE '%'||$2||'%')
		ORDER BY ls.position, ls.name`
	rows, err := s.pool.Query(ctx, sql, activeOnly, strings.TrimSpace(q))
	if err != nil {
		return nil, apperrors.Internal("failed to list lead sources", err)
	}
	defer rows.Close()
	var out []Source
	for rows.Next() {
		var it Source
		if err := rows.Scan(&it.ID, &it.Code, &it.Name, &it.Description, &it.IsActive, &it.Position,
			&it.UsageCount, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, apperrors.Internal("failed to scan lead source", err)
		}
		out = append(out, it)
	}
	if out == nil {
		out = []Source{}
	}
	return out, nil
}

func (s *Service) Create(ctx context.Context, actorID string, in CreateInput, ip, ua string) (*Source, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, apperrors.Validation("source name is required")
	}
	code := slugify(name)
	active := true
	if in.IsActive != nil {
		active = *in.IsActive
	}
	pos := 100
	if in.Position != nil {
		pos = *in.Position
	}
	var it Source
	err := s.pool.QueryRow(ctx, `
		INSERT INTO lead_sources (code, name, description, is_active, position, created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6::uuid,$6::uuid)
		RETURNING id::text, code, name, description, is_active, position, 0, created_at, updated_at`,
		code, name, strings.TrimSpace(in.Description), active, pos, actorID,
	).Scan(&it.ID, &it.Code, &it.Name, &it.Description, &it.IsActive, &it.Position, &it.UsageCount, &it.CreatedAt, &it.UpdatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "unique") {
			return nil, apperrors.Conflict("a lead source with this name already exists")
		}
		return nil, apperrors.Internal("failed to create lead source", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "lead_source.created", "lead_source", audit.Ptr(it.ID), map[string]any{
		"name": it.Name, "code": it.Code,
	}, ip, ua)
	return &it, nil
}

func (s *Service) Update(ctx context.Context, actorID, id string, in UpdateInput, ip, ua string) (*Source, error) {
	cur, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	name := cur.Name
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, apperrors.Validation("source name is required")
		}
	}
	desc := cur.Description
	if in.Description != nil {
		desc = strings.TrimSpace(*in.Description)
	}
	active := cur.IsActive
	if in.IsActive != nil {
		active = *in.IsActive
	}
	pos := cur.Position
	if in.Position != nil {
		pos = *in.Position
	}
	var it Source
	err = s.pool.QueryRow(ctx, `
		UPDATE lead_sources SET name=$2, description=$3, is_active=$4, position=$5, updated_by=$6::uuid
		WHERE id=$1::uuid
		RETURNING id::text, code, name, description, is_active, position,
			(SELECT COUNT(*)::int FROM leads l WHERE lower(l.source) = lower(lead_sources.name))
			+ (SELECT COUNT(*)::int FROM customers c WHERE lower(c.source) = lower(lead_sources.name)),
			created_at, updated_at`,
		id, name, desc, active, pos, actorID,
	).Scan(&it.ID, &it.Code, &it.Name, &it.Description, &it.IsActive, &it.Position, &it.UsageCount, &it.CreatedAt, &it.UpdatedAt)
	if err != nil {
		return nil, apperrors.Internal("failed to update lead source", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "lead_source.updated", "lead_source", audit.Ptr(it.ID), map[string]any{
		"name": it.Name, "isActive": it.IsActive,
	}, ip, ua)
	return &it, nil
}

func (s *Service) Delete(ctx context.Context, actorID, id string, ip, ua string) error {
	cur, err := s.get(ctx, id)
	if err != nil {
		return err
	}
	if cur.UsageCount > 0 {
		return apperrors.Validation("cannot delete a lead source in use; deactivate it instead")
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM lead_sources WHERE id=$1::uuid`, id)
	if err != nil {
		return apperrors.Internal("failed to delete lead source", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "lead_source.deleted", "lead_source", audit.Ptr(id), map[string]any{
		"name": cur.Name,
	}, ip, ua)
	return nil
}

func (s *Service) get(ctx context.Context, id string) (*Source, error) {
	var it Source
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, code, name, description, is_active, position,
			(SELECT COUNT(*)::int FROM leads l WHERE lower(l.source) = lower(lead_sources.name))
			+ (SELECT COUNT(*)::int FROM customers c WHERE lower(c.source) = lower(lead_sources.name)),
			created_at, updated_at
		FROM lead_sources WHERE id=$1::uuid`, id,
	).Scan(&it.ID, &it.Code, &it.Name, &it.Description, &it.IsActive, &it.Position, &it.UsageCount, &it.CreatedAt, &it.UpdatedAt)
	if err != nil {
		return nil, apperrors.NotFound("lead source not found")
	}
	return &it, nil
}
