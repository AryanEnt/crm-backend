package automation

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Event types emitted for the automation engine / future external workers.
const (
	EventDealCreated      = "deal.created"
	EventDealUpdated      = "deal.updated"
	EventDealStageChanged = "deal.stage_changed"
	EventPipelineCreated  = "pipeline.created"
	EventPipelineUpdated  = "pipeline.updated"
)

// Emitter records domain events for backend workers. Never called from UI components.
type Emitter struct {
	pool   *pgxpool.Pool
	notify func()
}

func NewEmitter(pool *pgxpool.Pool) *Emitter {
	return &Emitter{pool: pool}
}

// SetNotify registers an optional wake-up callback (in-process worker).
func (e *Emitter) SetNotify(fn func()) {
	if e != nil {
		e.notify = fn
	}
}

// Emit records an automation hook event. Failures are returned; callers typically log and continue.
func (e *Emitter) Emit(ctx context.Context, eventType, resourceType, resourceID string, payload map[string]any) error {
	if e == nil || e.pool == nil {
		return nil
	}
	if payload == nil {
		payload = map[string]any{}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		body = []byte("{}")
	}
	var rid any
	if resourceID != "" {
		rid = resourceID
	}
	_, err = e.pool.Exec(ctx, `
		INSERT INTO automation_events (id, event_type, resource_type, resource_id, payload)
		VALUES ($1, $2, $3, $4, $5::jsonb)
	`, uuid.NewString(), eventType, resourceType, rid, string(body))
	if err != nil {
		slog.Warn("automation emit failed", "event", eventType, "resource", resourceType, "error", err)
		return err
	}
	if e.notify != nil {
		e.notify()
	}
	return nil
}
