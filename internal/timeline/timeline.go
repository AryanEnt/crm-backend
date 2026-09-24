package timeline

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/crm/backend/pkg/apperrors"
)

// Event type constants — keep stable for filters and external writers.
const (
	EventLeadCreated         = "lead.created"
	EventCustomerCreated     = "customer.created"
	EventDealCreated         = "deal.created"
	EventActivityCreated     = "activity.created"
	EventActivityCompleted   = "activity.completed"
	EventCall                = "call"
	EventWhatsApp            = "whatsapp"
	EventEmail               = "email"
	EventMeeting             = "meeting"
	EventNote                = "note"
	EventStageChange         = "stage_change"
	EventAssignmentChange    = "assignment_change"
	EventDocumentRequested   = "document.requested"
	EventDocumentUploaded    = "document.uploaded"
	EventDocumentVerified    = "document.verified"
	EventDocumentRejected    = "document.rejected"
	EventAutomation          = "automation.event"
)

type Event struct {
	ID               string         `json:"id"`
	EventType        string         `json:"eventType"`
	Title            string         `json:"title"`
	Body             string         `json:"body"`
	OccurredAt       time.Time      `json:"occurredAt"`
	ActorUserID      *string        `json:"actorUserId"`
	ActorName        *string        `json:"actorName"`
	Source           string         `json:"source"`
	ExternalID       string         `json:"externalId"`
	ExternalProvider string         `json:"externalProvider"`
	LeadID           *string        `json:"leadId"`
	CustomerID       *string        `json:"customerId"`
	DealID           *string        `json:"dealId"`
	ActivityID       *string        `json:"activityId"`
	DocumentID       *string        `json:"documentId"`
	Metadata         map[string]any `json:"metadata"`
	CreatedAt        time.Time      `json:"createdAt"`
}

type WriteInput struct {
	EventType        string
	Title            string
	Body             string
	OccurredAt       *time.Time
	ActorUserID      *string
	Source           string
	ExternalID       string
	ExternalProvider string
	LeadID           *string
	CustomerID       *string
	DealID           *string
	ActivityID       *string
	DocumentID       *string
	Metadata         map[string]any
}

type ListFilter struct {
	CustomerID string
	DealID     string
	LeadID     string
	EventTypes []string
	Source     string
	Limit      int
	Offset     int
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Insert(ctx context.Context, in WriteInput) (*Event, error) {
	id := uuid.NewString()
	occurred := time.Now().UTC()
	if in.OccurredAt != nil {
		occurred = in.OccurredAt.UTC()
	}
	source := in.Source
	if source == "" {
		source = "crm"
	}
	meta, _ := json.Marshal(in.Metadata)
	if in.Metadata == nil {
		meta = []byte("{}")
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO timeline_events (
			id, event_type, title, body, occurred_at, actor_user_id, source,
			external_id, external_provider,
			lead_id, customer_id, deal_id, activity_id, document_id, metadata
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::jsonb)
	`, id, in.EventType, in.Title, in.Body, occurred, emptyToNil(in.ActorUserID), source,
		in.ExternalID, in.ExternalProvider,
		emptyToNil(in.LeadID), emptyToNil(in.CustomerID), emptyToNil(in.DealID),
		emptyToNil(in.ActivityID), emptyToNil(in.DocumentID), string(meta))
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

// InsertIdempotent writes a timeline event keyed by external_provider + external_id.
// Provider webhook retries return the existing event without creating duplicates.
func (r *Repository) InsertIdempotent(ctx context.Context, in WriteInput) (*Event, bool, error) {
	if strings.TrimSpace(in.ExternalID) == "" || strings.TrimSpace(in.ExternalProvider) == "" {
		ev, err := r.Insert(ctx, in)
		return ev, false, err
	}
	existing, err := r.FindByExternal(ctx, in.ExternalProvider, in.ExternalID)
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		return existing, true, nil
	}
	ev, err := r.Insert(ctx, in)
	if err != nil {
		// Race with unique index — fetch winner
		if existing, e2 := r.FindByExternal(ctx, in.ExternalProvider, in.ExternalID); e2 == nil && existing != nil {
			return existing, true, nil
		}
		return nil, false, err
	}
	return ev, false, nil
}

func (r *Repository) FindByExternal(ctx context.Context, provider, externalID string) (*Event, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT e.id::text, e.event_type, e.title, e.body, e.occurred_at,
			e.actor_user_id::text, u.full_name, e.source, e.external_id, e.external_provider,
			e.lead_id::text, e.customer_id::text, e.deal_id::text,
			e.activity_id::text, e.document_id::text, e.metadata, e.created_at
		FROM timeline_events e
		LEFT JOIN users u ON u.id = e.actor_user_id
		WHERE e.external_provider=$1 AND e.external_id=$2
		LIMIT 1
	`, provider, externalID)
	ev, err := scanEvent(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return ev, err
}

func (r *Repository) Get(ctx context.Context, id string) (*Event, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT e.id::text, e.event_type, e.title, e.body, e.occurred_at,
			e.actor_user_id::text, u.full_name, e.source, e.external_id, e.external_provider,
			e.lead_id::text, e.customer_id::text, e.deal_id::text,
			e.activity_id::text, e.document_id::text, e.metadata, e.created_at
		FROM timeline_events e
		LEFT JOIN users u ON u.id = e.actor_user_id
		WHERE e.id=$1
	`, id)
	return scanEvent(row)
}

func (r *Repository) List(ctx context.Context, f ListFilter) ([]Event, int, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 30
	}
	args := []any{}
	where := []string{"1=1"}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.Replace(cond, "?", "$"+itoa(len(args)), 1))
	}
	if f.CustomerID != "" {
		add("e.customer_id::text = ?", f.CustomerID)
	}
	if f.DealID != "" {
		add("e.deal_id::text = ?", f.DealID)
	}
	if f.LeadID != "" {
		add("e.lead_id::text = ?", f.LeadID)
	}
	if f.Source != "" {
		add("e.source = ?", f.Source)
	}
	if len(f.EventTypes) > 0 {
		args = append(args, f.EventTypes)
		where = append(where, "e.event_type = ANY($"+itoa(len(args))+")")
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM timeline_events e WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, f.Limit, f.Offset)
	rows, err := r.pool.Query(ctx, `
		SELECT e.id::text, e.event_type, e.title, e.body, e.occurred_at,
			e.actor_user_id::text, u.full_name, e.source, e.external_id, e.external_provider,
			e.lead_id::text, e.customer_id::text, e.deal_id::text,
			e.activity_id::text, e.document_id::text, e.metadata, e.created_at
		FROM timeline_events e
		LEFT JOIN users u ON u.id = e.actor_user_id
		WHERE `+whereSQL+`
		ORDER BY e.occurred_at DESC, e.created_at DESC
		LIMIT $`+itoa(len(args)-1)+` OFFSET $`+itoa(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var items []Event
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *ev)
	}
	if items == nil {
		items = []Event{}
	}
	return items, total, rows.Err()
}

func scanEvent(row pgx.Row) (*Event, error) {
	var e Event
	var meta []byte
	err := row.Scan(
		&e.ID, &e.EventType, &e.Title, &e.Body, &e.OccurredAt,
		&e.ActorUserID, &e.ActorName, &e.Source, &e.ExternalID, &e.ExternalProvider,
		&e.LeadID, &e.CustomerID, &e.DealID, &e.ActivityID, &e.DocumentID, &meta, &e.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	e.Metadata = map[string]any{}
	_ = json.Unmarshal(meta, &e.Metadata)
	return &e, nil
}

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// Record writes a timeline event. Best-effort callers may ignore errors; services should log.
func (s *Service) Record(ctx context.Context, in WriteInput) (*Event, error) {
	if strings.TrimSpace(in.EventType) == "" {
		return nil, apperrors.Validation("eventType is required")
	}
	if emptyToNil(in.LeadID) == nil && emptyToNil(in.CustomerID) == nil && emptyToNil(in.DealID) == nil &&
		emptyToNil(in.ActivityID) == nil && emptyToNil(in.DocumentID) == nil {
		return nil, apperrors.Validation("timeline event must reference a CRM entity")
	}
	if strings.TrimSpace(in.Title) == "" {
		in.Title = in.EventType
	}
	ev, err := s.repo.Insert(ctx, in)
	if err != nil {
		return nil, apperrors.Internal("failed to write timeline event", err)
	}
	return ev, nil
}

// RecordIdempotent writes once per external_provider + external_id (webhook-safe).
func (s *Service) RecordIdempotent(ctx context.Context, in WriteInput) (*Event, bool, error) {
	if strings.TrimSpace(in.EventType) == "" {
		return nil, false, apperrors.Validation("eventType is required")
	}
	if emptyToNil(in.LeadID) == nil && emptyToNil(in.CustomerID) == nil && emptyToNil(in.DealID) == nil &&
		emptyToNil(in.ActivityID) == nil && emptyToNil(in.DocumentID) == nil {
		return nil, false, apperrors.Validation("timeline event must reference a CRM entity")
	}
	if strings.TrimSpace(in.Title) == "" {
		in.Title = in.EventType
	}
	ev, existed, err := s.repo.InsertIdempotent(ctx, in)
	if err != nil {
		return nil, false, apperrors.Internal("failed to write timeline event", err)
	}
	return ev, existed, nil
}

func (s *Service) List(ctx context.Context, f ListFilter) ([]Event, int, error) {
	if f.CustomerID == "" && f.DealID == "" && f.LeadID == "" {
		return nil, 0, apperrors.Validation("customerId, dealId, or leadId is required")
	}
	items, total, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, 0, apperrors.Internal("failed to list timeline", err)
	}
	return items, total, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Event, error) {
	ev, err := s.repo.Get(ctx, id)
	if err == pgx.ErrNoRows {
		return nil, apperrors.NotFound("timeline event not found")
	}
	if err != nil {
		return nil, apperrors.Internal("failed to load timeline event", err)
	}
	return ev, nil
}

// ExternalWriteInput is the public payload for integrations (WhatsApp, Twilio, email, etc.).
type ExternalWriteInput struct {
	EventType        string         `json:"eventType"`
	Title            string         `json:"title"`
	Body             string         `json:"body"`
	OccurredAt       *string        `json:"occurredAt"`
	Source           string         `json:"source"`
	ExternalID       string         `json:"externalId"`
	ExternalProvider string         `json:"externalProvider"`
	LeadID           *string        `json:"leadId"`
	CustomerID       *string        `json:"customerId"`
	DealID           *string        `json:"dealId"`
	ActivityID       *string        `json:"activityId"`
	DocumentID       *string        `json:"documentId"`
	Metadata         map[string]any `json:"metadata"`
}

func (s *Service) WriteExternal(ctx context.Context, actorID string, in ExternalWriteInput) (*Event, error) {
	source := in.Source
	if source == "" {
		source = "external"
	}
	switch source {
	case "whatsapp", "twilio", "email", "automation", "external", "system":
	default:
		return nil, apperrors.Validation("invalid source for external timeline write")
	}
	var occurred *time.Time
	if in.OccurredAt != nil && strings.TrimSpace(*in.OccurredAt) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(*in.OccurredAt))
		if err != nil {
			return nil, apperrors.Validation("invalid occurredAt")
		}
		utc := t.UTC()
		occurred = &utc
	}
	var actor *string
	if actorID != "" {
		actor = &actorID
	}
	return s.Record(ctx, WriteInput{
		EventType: in.EventType, Title: in.Title, Body: in.Body, OccurredAt: occurred,
		ActorUserID: actor, Source: source,
		ExternalID: in.ExternalID, ExternalProvider: in.ExternalProvider,
		LeadID: in.LeadID, CustomerID: in.CustomerID, DealID: in.DealID,
		ActivityID: in.ActivityID, DocumentID: in.DocumentID, Metadata: in.Metadata,
	})
}

func emptyToNil(v *string) *string {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil
	}
	return v
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
