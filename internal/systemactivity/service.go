package systemactivity

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	apperrors "github.com/crm/backend/pkg/apperrors"
)

type Event struct {
	ID          string         `json:"id"`
	EventType   string         `json:"eventType"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	ActorUserID *string        `json:"actorUserId"`
	ActorName   *string        `json:"actorName"`
	TeamID      *string        `json:"teamId"`
	TeamName    *string        `json:"teamName"`
	EntityType  string         `json:"entityType"`
	EntityID    *string        `json:"entityId"`
	EntityLabel string         `json:"entityLabel"`
	Result      string         `json:"result"`
	Metadata    map[string]any `json:"metadata"`
	CreatedAt   time.Time      `json:"createdAt"`
}

type WriteInput struct {
	EventType   string
	Title       string
	Description string
	ActorUserID string
	TeamID      string
	EntityType  string
	EntityID    string
	EntityLabel string
	Result      string
	Metadata    map[string]any
}

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

func (s *Service) Record(ctx context.Context, in WriteInput) {
	if in.Result == "" {
		in.Result = "success"
	}
	meta, _ := json.Marshal(in.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	_, _ = s.pool.Exec(ctx, `
		INSERT INTO system_activity_events
			(event_type, title, description, actor_user_id, team_id, entity_type, entity_id, entity_label, result, metadata)
		VALUES ($1,$2,$3,
			NULLIF($4,'')::uuid, NULLIF($5,'')::uuid, $6, NULLIF($7,'')::uuid, $8, $9, $10::jsonb)`,
		in.EventType, in.Title, in.Description, in.ActorUserID, in.TeamID, in.EntityType, in.EntityID, in.EntityLabel, in.Result, string(meta),
	)
}

func (s *Service) List(ctx context.Context, q, eventType, entityType, result, actorID string, limit, offset int) ([]Event, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var total int
	_ = s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM system_activity_events e
		WHERE ($1 = '' OR e.title ILIKE '%'||$1||'%' OR e.description ILIKE '%'||$1||'%' OR e.entity_label ILIKE '%'||$1||'%')
		  AND ($2 = '' OR e.event_type = $2)
		  AND ($3 = '' OR e.entity_type = $3)
		  AND ($4 = '' OR e.result = $4)
		  AND ($5 = '' OR e.actor_user_id::text = $5)`,
		strings.TrimSpace(q), eventType, entityType, result, actorID,
	).Scan(&total)

	rows, err := s.pool.Query(ctx, `
		SELECT e.id::text, e.event_type, e.title, e.description,
			e.actor_user_id::text, u.full_name, e.team_id::text, t.name,
			e.entity_type, e.entity_id::text, e.entity_label, e.result, e.metadata, e.created_at
		FROM system_activity_events e
		LEFT JOIN users u ON u.id = e.actor_user_id
		LEFT JOIN teams t ON t.id = e.team_id
		WHERE ($1 = '' OR e.title ILIKE '%'||$1||'%' OR e.description ILIKE '%'||$1||'%' OR e.entity_label ILIKE '%'||$1||'%')
		  AND ($2 = '' OR e.event_type = $2)
		  AND ($3 = '' OR e.entity_type = $3)
		  AND ($4 = '' OR e.result = $4)
		  AND ($5 = '' OR e.actor_user_id::text = $5)
		ORDER BY e.created_at DESC
		LIMIT $6 OFFSET $7`,
		strings.TrimSpace(q), eventType, entityType, result, actorID, limit, offset)
	if err != nil {
		return nil, 0, apperrors.Internal("list system activity", err)
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var raw []byte
		_ = rows.Scan(&e.ID, &e.EventType, &e.Title, &e.Description,
			&e.ActorUserID, &e.ActorName, &e.TeamID, &e.TeamName,
			&e.EntityType, &e.EntityID, &e.EntityLabel, &e.Result, &raw, &e.CreatedAt)
		e.Metadata = map[string]any{}
		_ = json.Unmarshal(raw, &e.Metadata)
		out = append(out, e)
	}
	if out == nil {
		out = []Event{}
	}
	return out, total, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Event, error) {
	var e Event
	var raw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT e.id::text, e.event_type, e.title, e.description,
			e.actor_user_id::text, u.full_name, e.team_id::text, t.name,
			e.entity_type, e.entity_id::text, e.entity_label, e.result, e.metadata, e.created_at
		FROM system_activity_events e
		LEFT JOIN users u ON u.id = e.actor_user_id
		LEFT JOIN teams t ON t.id = e.team_id
		WHERE e.id=$1::uuid`, id,
	).Scan(&e.ID, &e.EventType, &e.Title, &e.Description,
		&e.ActorUserID, &e.ActorName, &e.TeamID, &e.TeamName,
		&e.EntityType, &e.EntityID, &e.EntityLabel, &e.Result, &raw, &e.CreatedAt)
	if err != nil {
		return nil, apperrors.NotFound("event not found")
	}
	e.Metadata = map[string]any{}
	_ = json.Unmarshal(raw, &e.Metadata)
	return &e, nil
}
