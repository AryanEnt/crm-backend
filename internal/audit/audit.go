package audit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Entry struct {
	ID           string         `json:"id"`
	ActorUserID  *string        `json:"actorUserId"`
	ActorName    *string        `json:"actorName,omitempty"`
	ActorEmail   *string        `json:"actorEmail,omitempty"`
	Action       string         `json:"action"`
	ResourceType string         `json:"resourceType"`
	ResourceID   *string        `json:"resourceId"`
	Metadata     map[string]any `json:"metadata"`
	IPAddress    string         `json:"ipAddress"`
	UserAgent    string         `json:"userAgent"`
	CreatedAt    time.Time      `json:"createdAt"`
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Record(ctx context.Context, actorUserID *string, action, resourceType string, resourceID *string, metadata map[string]any, ip, ua string) error {
	if metadata == nil {
		metadata = map[string]any{}
	}
	b, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO audit_logs (id, actor_user_id, action, resource_type, resource_id, metadata, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8)
	`, uuid.NewString(), actorUserID, action, resourceType, resourceID, string(b), ip, ua)
	return err
}

type ListFilter struct {
	Search string
	Limit  int
	Offset int
}

func (r *Repository) List(ctx context.Context, f ListFilter) ([]Entry, int, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 25
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	var total int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM audit_logs a
		LEFT JOIN users u ON u.id = a.actor_user_id
		WHERE ($1 = '' OR a.action ILIKE '%' || $1 || '%' OR a.resource_type ILIKE '%' || $1 || '%'
			OR COALESCE(u.email, '') ILIKE '%' || $1 || '%' OR COALESCE(u.full_name, '') ILIKE '%' || $1 || '%')
	`, f.Search).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT a.id::text, a.actor_user_id::text, u.full_name, u.email, a.action, a.resource_type,
		       a.resource_id, a.metadata, a.ip_address, a.user_agent, a.created_at
		FROM audit_logs a
		LEFT JOIN users u ON u.id = a.actor_user_id
		WHERE ($1 = '' OR a.action ILIKE '%' || $1 || '%' OR a.resource_type ILIKE '%' || $1 || '%'
			OR COALESCE(u.email, '') ILIKE '%' || $1 || '%' OR COALESCE(u.full_name, '') ILIKE '%' || $1 || '%')
		ORDER BY a.created_at DESC
		LIMIT $2 OFFSET $3
	`, f.Search, f.Limit, f.Offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var items []Entry
	for rows.Next() {
		var e Entry
		var actorID, actorName, actorEmail, resourceID *string
		var metaBytes []byte
		if err := rows.Scan(
			&e.ID, &actorID, &actorName, &actorEmail, &e.Action, &e.ResourceType,
			&resourceID, &metaBytes, &e.IPAddress, &e.UserAgent, &e.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		e.ActorUserID = actorID
		e.ActorName = actorName
		e.ActorEmail = actorEmail
		e.ResourceID = resourceID
		_ = json.Unmarshal(metaBytes, &e.Metadata)
		if e.Metadata == nil {
			e.Metadata = map[string]any{}
		}
		items = append(items, e)
	}
	return items, total, rows.Err()
}

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Record(ctx context.Context, actorUserID *string, action, resourceType string, resourceID *string, metadata map[string]any, ip, ua string) error {
	return s.repo.Record(ctx, actorUserID, action, resourceType, resourceID, metadata, ip, ua)
}

func (s *Service) List(ctx context.Context, f ListFilter) ([]Entry, int, error) {
	return s.repo.List(ctx, f)
}

func Ptr(s string) *string { return &s }
