package automation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Executor applies THEN actions via SQL (independent of UI / domain HTTP handlers).
// Side-effects are audited; they do not re-enter the automation engine (loop guard).
type Executor struct {
	pool  *pgxpool.Pool
	repo  *Repository
	audit AuditRecorder
}

type AuditRecorder interface {
	Record(ctx context.Context, actorUserID *string, action, resourceType string, resourceID *string, metadata map[string]any, ip, userAgent string) error
}

func NewExecutor(pool *pgxpool.Pool, repo *Repository, audit AuditRecorder) *Executor {
	return &Executor{pool: pool, repo: repo, audit: audit}
}

func (e *Executor) Execute(ctx context.Context, auto *Automation, entityCtx map[string]any, resourceType string, resourceID string) ([]ActionResult, error) {
	results := make([]ActionResult, 0, len(auto.Actions))
	var firstErr error
	for _, action := range auto.Actions {
		res := e.runAction(ctx, auto, action, entityCtx, resourceType, resourceID)
		results = append(results, res)
		if res.Status == "failed" && firstErr == nil {
			firstErr = fmt.Errorf("%s: %s", res.Type, res.Error)
		}
	}
	return results, firstErr
}

func (e *Executor) runAction(ctx context.Context, auto *Automation, action Action, entityCtx map[string]any, resourceType, resourceID string) ActionResult {
	params := action.Params
	if params == nil {
		params = map[string]any{}
	}
	switch action.Type {
	case ActionCreateTask:
		return e.createTask(ctx, auto, params, entityCtx, resourceType, resourceID)
	case ActionAssignUser:
		return e.assignUser(ctx, auto, params, resourceType, resourceID)
	case ActionChangeStage:
		return e.changeStage(ctx, auto, params, entityCtx, resourceType, resourceID)
	case ActionSendNotification:
		return e.sendNotification(ctx, auto, params, resourceType, resourceID)
	case ActionUpdateField:
		return e.updateField(ctx, auto, params, resourceType, resourceID)
	case ActionAddTag:
		return e.addTag(ctx, auto, params, resourceType, resourceID)
	case ActionCreateActivity:
		return e.createActivity(ctx, auto, params, entityCtx, resourceType, resourceID)
	default:
		return ActionResult{Type: action.Type, Status: "failed", Error: "unsupported action"}
	}
}

func (e *Executor) createTask(ctx context.Context, auto *Automation, p, entityCtx map[string]any, resourceType, resourceID string) ActionResult {
	title := strings.TrimSpace(asString(p["title"]))
	if title == "" {
		return ActionResult{Type: ActionCreateTask, Status: "failed", Error: "title required"}
	}
	dueHours := toFloat(p["dueInHours"])
	var due any
	if dueHours > 0 {
		due = time.Now().UTC().Add(time.Duration(dueHours) * time.Hour)
	}
	owner := strings.TrimSpace(asString(p["ownerUserId"]))
	if owner == "" {
		owner = asString(entityCtx["ownerUserId"])
	}
	id := uuid.NewString()
	leadID, dealID, customerID := resolveLinks(resourceType, resourceID, entityCtx)
	_, err := e.pool.Exec(ctx, `
		INSERT INTO activities (
			id, kind, subject, title, body, status, due_at, owner_user_id, actor_user_id,
			lead_id, deal_id, customer_id, priority, created_by_user_id
		) VALUES (
			$1, 'task', $2, $2, $3, 'upcoming', $4, $5, $5, $6, $7, $8, 'medium', $5
		)
	`, id, title, "Created by automation: "+auto.Name, due, emptyUUID(owner), emptyUUID(leadID), emptyUUID(dealID), emptyUUID(customerID))
	if err != nil {
		return ActionResult{Type: ActionCreateTask, Status: "failed", Error: err.Error()}
	}
	e.record(ctx, ActionCreateTask, "activity", id, map[string]any{"automationId": auto.ID, "title": title})
	return ActionResult{Type: ActionCreateTask, Status: "succeeded", Detail: "Task created", Payload: map[string]any{"activityId": id}}
}

func (e *Executor) assignUser(ctx context.Context, auto *Automation, p map[string]any, resourceType, resourceID string) ActionResult {
	owner := strings.TrimSpace(asString(p["ownerUserId"]))
	if owner == "" {
		return ActionResult{Type: ActionAssignUser, Status: "failed", Error: "ownerUserId required"}
	}
	var err error
	switch resourceType {
	case "lead":
		_, err = e.pool.Exec(ctx, `UPDATE leads SET owner_user_id=$2 WHERE id=$1`, resourceID, owner)
	case "deal":
		_, err = e.pool.Exec(ctx, `UPDATE deals SET owner_user_id=$2 WHERE id=$1`, resourceID, owner)
	case "customer":
		_, err = e.pool.Exec(ctx, `UPDATE customers SET owner_user_id=$2 WHERE id=$1`, resourceID, owner)
	case "activity", "document":
		// assign underlying lead/deal if present is handled by entity context actions on those resources
		return ActionResult{Type: ActionAssignUser, Status: "skipped", Detail: "assign_user applies to lead, deal, or customer"}
	default:
		return ActionResult{Type: ActionAssignUser, Status: "failed", Error: "unsupported resource"}
	}
	if err != nil {
		return ActionResult{Type: ActionAssignUser, Status: "failed", Error: err.Error()}
	}
	e.record(ctx, "automation.assign_user", resourceType, resourceID, map[string]any{"automationId": auto.ID, "ownerUserId": owner})
	return ActionResult{Type: ActionAssignUser, Status: "succeeded", Detail: "Owner assigned", Payload: map[string]any{"ownerUserId": owner}}
}

func (e *Executor) changeStage(ctx context.Context, auto *Automation, p, entityCtx map[string]any, resourceType, resourceID string) ActionResult {
	stageID := strings.TrimSpace(asString(p["stageId"]))
	pipelineID := strings.TrimSpace(asString(p["pipelineId"]))
	entityPipe := asString(entityCtx["pipelineId"])
	if err := ValidateChangeStageTarget(ctx, entityPipe, pipelineID, stageID, e.repo.StageBelongsToPipeline); err != nil {
		return ActionResult{Type: ActionChangeStage, Status: "failed", Error: err.Error()}
	}
	pipe := pipelineID
	if pipe == "" {
		pipe = entityPipe
	}
	var err error
	switch resourceType {
	case "lead":
		_, err = e.pool.Exec(ctx, `
			UPDATE leads SET stage_id=$2, pipeline_id=COALESCE(NULLIF($3,'')::uuid, pipeline_id) WHERE id=$1
		`, resourceID, stageID, pipe)
	case "deal":
		_, err = e.pool.Exec(ctx, `
			UPDATE deals SET stage_id=$2, pipeline_id=COALESCE(NULLIF($3,'')::uuid, pipeline_id), stage_entered_at=NOW() WHERE id=$1
		`, resourceID, stageID, pipe)
	default:
		return ActionResult{Type: ActionChangeStage, Status: "skipped", Detail: "change_stage applies to lead or deal"}
	}
	if err != nil {
		return ActionResult{Type: ActionChangeStage, Status: "failed", Error: err.Error()}
	}
	e.record(ctx, "automation.change_stage", resourceType, resourceID, map[string]any{
		"automationId": auto.ID, "stageId": stageID, "pipelineId": pipe,
	})
	return ActionResult{Type: ActionChangeStage, Status: "succeeded", Detail: "Stage updated", Payload: map[string]any{"stageId": stageID}}
}

func (e *Executor) sendNotification(ctx context.Context, auto *Automation, p map[string]any, resourceType, resourceID string) ActionResult {
	message := strings.TrimSpace(asString(p["message"]))
	userID := strings.TrimSpace(asString(p["userId"]))
	title := strings.TrimSpace(asString(p["title"]))
	if title == "" {
		title = "Automation: " + auto.Name
	}
	// Persist as audit + timeline-friendly notification row in automation_runs payload path.
	// Dedicated notifications table can replace this later without changing THEN contract.
	e.record(ctx, "automation.notification", resourceType, resourceID, map[string]any{
		"automationId": auto.ID, "title": title, "message": message, "userId": userID,
		"channel": asString(p["channel"]),
	})
	return ActionResult{
		Type: ActionSendNotification, Status: "succeeded",
		Detail: "Notification recorded for delivery workers",
		Payload: map[string]any{"title": title, "message": message, "userId": userID},
	}
}

func (e *Executor) updateField(ctx context.Context, auto *Automation, p map[string]any, resourceType, resourceID string) ActionResult {
	field := strings.TrimSpace(asString(p["field"]))
	value := asString(p["value"])
	var q string
	switch resourceType {
	case "lead":
		switch field {
		case "priority":
			q = `UPDATE leads SET priority=$2 WHERE id=$1`
		case "source":
			q = `UPDATE leads SET source=$2 WHERE id=$1`
		case "notes":
			q = `UPDATE leads SET notes=$2 WHERE id=$1`
		case "status":
			q = `UPDATE leads SET status=$2 WHERE id=$1`
		default:
			return ActionResult{Type: ActionUpdateField, Status: "failed", Error: "field not allowed"}
		}
	case "deal":
		switch field {
		case "priority":
			q = `UPDATE deals SET priority=$2 WHERE id=$1`
		case "source":
			q = `UPDATE deals SET source=$2 WHERE id=$1`
		case "notes":
			q = `UPDATE deals SET notes=$2 WHERE id=$1`
		case "status":
			q = `UPDATE deals SET status=$2 WHERE id=$1`
		default:
			return ActionResult{Type: ActionUpdateField, Status: "failed", Error: "field not allowed"}
		}
	default:
		return ActionResult{Type: ActionUpdateField, Status: "skipped", Detail: "update_field applies to lead or deal"}
	}
	if _, err := e.pool.Exec(ctx, q, resourceID, value); err != nil {
		return ActionResult{Type: ActionUpdateField, Status: "failed", Error: err.Error()}
	}
	e.record(ctx, "automation.update_field", resourceType, resourceID, map[string]any{
		"automationId": auto.ID, "field": field, "value": value,
	})
	return ActionResult{Type: ActionUpdateField, Status: "succeeded", Detail: "Field updated", Payload: map[string]any{"field": field}}
}

func (e *Executor) addTag(ctx context.Context, auto *Automation, p map[string]any, resourceType, resourceID string) ActionResult {
	tag := strings.TrimSpace(asString(p["tag"]))
	if tag == "" {
		return ActionResult{Type: ActionAddTag, Status: "failed", Error: "tag required"}
	}
	if resourceType != "lead" {
		return ActionResult{Type: ActionAddTag, Status: "skipped", Detail: "add_tag applies to leads"}
	}
	_, err := e.pool.Exec(ctx, `
		UPDATE leads SET tags = (
			SELECT ARRAY(SELECT DISTINCT unnest(COALESCE(tags,'{}') || ARRAY[$2]::text[]))
		) WHERE id=$1
	`, resourceID, tag)
	if err != nil {
		return ActionResult{Type: ActionAddTag, Status: "failed", Error: err.Error()}
	}
	e.record(ctx, "automation.add_tag", resourceType, resourceID, map[string]any{"automationId": auto.ID, "tag": tag})
	return ActionResult{Type: ActionAddTag, Status: "succeeded", Detail: "Tag added", Payload: map[string]any{"tag": tag}}
}

func (e *Executor) createActivity(ctx context.Context, auto *Automation, p, entityCtx map[string]any, resourceType, resourceID string) ActionResult {
	title := strings.TrimSpace(asString(p["title"]))
	if title == "" {
		title = strings.TrimSpace(asString(p["subject"]))
	}
	kind := strings.TrimSpace(asString(p["kind"]))
	if kind == "" {
		kind = "note"
	}
	body := asString(p["body"])
	id := uuid.NewString()
	leadID, dealID, customerID := resolveLinks(resourceType, resourceID, entityCtx)
	owner := asString(p["ownerUserId"])
	if owner == "" {
		owner = asString(entityCtx["ownerUserId"])
	}
	_, err := e.pool.Exec(ctx, `
		INSERT INTO activities (
			id, kind, subject, title, body, status, owner_user_id, actor_user_id,
			lead_id, deal_id, customer_id, created_by_user_id
		) VALUES ($1,$2,$3,$3,$4,'completed',$5,$5,$6,$7,$8,$5)
	`, id, kind, title, body+"\n\n(via automation: "+auto.Name+")", emptyUUID(owner),
		emptyUUID(leadID), emptyUUID(dealID), emptyUUID(customerID))
	if err != nil {
		return ActionResult{Type: ActionCreateActivity, Status: "failed", Error: err.Error()}
	}
	e.record(ctx, "automation.create_activity", "activity", id, map[string]any{"automationId": auto.ID, "kind": kind})
	return ActionResult{Type: ActionCreateActivity, Status: "succeeded", Detail: "Activity created", Payload: map[string]any{"activityId": id}}
}

func (e *Executor) record(ctx context.Context, action, resourceType, resourceID string, meta map[string]any) {
	if e.audit == nil {
		return
	}
	rid := resourceID
	_ = e.audit.Record(ctx, nil, action, resourceType, &rid, meta, "", "automation-worker")
}

func resolveLinks(resourceType, resourceID string, entityCtx map[string]any) (leadID, dealID, customerID string) {
	switch resourceType {
	case "lead":
		leadID = resourceID
	case "deal":
		dealID = resourceID
		customerID = asString(entityCtx["customerId"])
	case "customer":
		customerID = resourceID
	case "activity", "document":
		leadID = asString(entityCtx["leadId"])
		dealID = asString(entityCtx["dealId"])
		customerID = asString(entityCtx["customerId"])
	}
	return
}
