package automation

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/crm/backend/internal/attention"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) CreateAutomation(ctx context.Context, actorID string, in CreateInput) (*Automation, error) {
	id := uuid.NewString()
	active := true
	if in.IsActive != nil {
		active = *in.IsActive
	}
	cond, _ := json.Marshal(in.Conditions)
	acts, _ := json.Marshal(in.Actions)
	_, err := r.pool.Exec(ctx, `
		INSERT INTO automations (id, name, description, is_active, trigger_type, conditions, actions, created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8,$8)
	`, id, strings.TrimSpace(in.Name), strings.TrimSpace(in.Description), active, in.TriggerType,
		string(cond), string(acts), emptyUUID(actorID))
	if err != nil {
		return nil, err
	}
	return r.GetAutomation(ctx, id)
}

func (r *Repository) UpdateAutomation(ctx context.Context, actorID, id string, current *Automation, in UpdateInput) (*Automation, error) {
	name := current.Name
	desc := current.Description
	active := current.IsActive
	trigger := current.TriggerType
	conds := current.Conditions
	acts := current.Actions
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		desc = strings.TrimSpace(*in.Description)
	}
	if in.IsActive != nil {
		active = *in.IsActive
	}
	if in.TriggerType != nil {
		trigger = *in.TriggerType
	}
	if in.Conditions != nil {
		conds = *in.Conditions
	}
	if in.Actions != nil {
		acts = *in.Actions
	}
	condJSON, _ := json.Marshal(conds)
	actJSON, _ := json.Marshal(acts)
	_, err := r.pool.Exec(ctx, `
		UPDATE automations SET name=$2, description=$3, is_active=$4, trigger_type=$5,
			conditions=$6::jsonb, actions=$7::jsonb, updated_by=$8
		WHERE id=$1
	`, id, name, desc, active, trigger, string(condJSON), string(actJSON), emptyUUID(actorID))
	if err != nil {
		return nil, err
	}
	return r.GetAutomation(ctx, id)
}

func (r *Repository) DeleteAutomation(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM automations WHERE id=$1`, id)
	return err
}

func (r *Repository) GetAutomation(ctx context.Context, id string) (*Automation, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id::text, name, description, is_active, trigger_type, conditions, actions,
			created_by::text, updated_by::text, created_at, updated_at
		FROM automations WHERE id=$1
	`, id)
	return scanAutomation(row)
}

func (r *Repository) ListAutomations(ctx context.Context, trigger string, activeOnly bool, limit, offset int) ([]Automation, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM automations
		WHERE ($1='' OR trigger_type=$1) AND ($2=FALSE OR is_active=TRUE)
	`, trigger, activeOnly).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, name, description, is_active, trigger_type, conditions, actions,
			created_by::text, updated_by::text, created_at, updated_at
		FROM automations
		WHERE ($1='' OR trigger_type=$1) AND ($2=FALSE OR is_active=TRUE)
		ORDER BY updated_at DESC
		LIMIT $3 OFFSET $4
	`, trigger, activeOnly, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Automation
	for rows.Next() {
		a, err := scanAutomation(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *a)
	}
	if out == nil {
		out = []Automation{}
	}
	return out, total, rows.Err()
}

func (r *Repository) ListActiveByTrigger(ctx context.Context, trigger string) ([]Automation, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, name, description, is_active, trigger_type, conditions, actions,
			created_by::text, updated_by::text, created_at, updated_at
		FROM automations WHERE is_active=TRUE AND trigger_type=$1
		ORDER BY created_at ASC
	`, trigger)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Automation
	for rows.Next() {
		a, err := scanAutomation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

type eventRow struct {
	ID           string
	EventType    string
	ResourceType string
	ResourceID   *string
	Payload      map[string]any
}

func (r *Repository) ClaimPendingEvents(ctx context.Context, workerID string, limit int) ([]eventRow, error) {
	if limit <= 0 {
		limit = 20
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT id::text, event_type, resource_type, resource_id::text, payload
		FROM automation_events
		WHERE status='pending'
		ORDER BY created_at ASC
		LIMIT $1
		FOR UPDATE SKIP LOCKED
	`, limit)
	if err != nil {
		return nil, err
	}
	var events []eventRow
	var ids []string
	for rows.Next() {
		var e eventRow
		var payload []byte
		var rid *string
		if err := rows.Scan(&e.ID, &e.EventType, &e.ResourceType, &rid, &payload); err != nil {
			rows.Close()
			return nil, err
		}
		e.ResourceID = rid
		_ = json.Unmarshal(payload, &e.Payload)
		if e.Payload == nil {
			e.Payload = map[string]any{}
		}
		events = append(events, e)
		ids = append(ids, e.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, tx.Commit(ctx)
	}
	_, err = tx.Exec(ctx, `
		UPDATE automation_events
		SET status='processing', claimed_at=NOW(), claimed_by=$2, attempts=attempts+1
		WHERE id = ANY($1::uuid[])
	`, ids, workerID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return events, nil
}

func (r *Repository) MarkEventProcessed(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE automation_events SET status='processed', processed_at=NOW(), last_error='' WHERE id=$1
	`, id)
	return err
}

func (r *Repository) MarkEventFailed(ctx context.Context, id, errMsg string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE automation_events SET status='failed', processed_at=NOW(), last_error=$2 WHERE id=$1
	`, id, truncate(errMsg, 2000))
	return err
}

func (r *Repository) EnqueueJob(ctx context.Context, eventID *string, auto *Automation, resourceType string, resourceID *string, payload map[string]any) (string, error) {
	id := uuid.NewString()
	body, _ := json.Marshal(payload)
	var autoID any
	if auto != nil {
		autoID = auto.ID
	}
	trigger := ""
	if auto != nil {
		trigger = auto.TriggerType
	} else if payload != nil {
		if t, ok := payload["triggerType"].(string); ok {
			trigger = t
		}
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO automation_jobs (
			id, event_id, automation_id, trigger_type, resource_type, resource_id, payload, status, next_attempt_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,'pending',NOW())
	`, id, emptyUUIDPtr(eventID), autoID, trigger, resourceType, emptyUUIDPtr(resourceID), string(body))
	return id, err
}

func (r *Repository) ClaimJobs(ctx context.Context, workerID string, limit int) ([]Job, error) {
	if limit <= 0 {
		limit = 20
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT id::text, event_id::text, automation_id::text, trigger_type, resource_type, resource_id::text,
			payload, status, attempts, max_attempts, next_attempt_at, last_error, created_at
		FROM automation_jobs
		WHERE status IN ('pending','failed') AND next_attempt_at <= NOW() AND attempts < max_attempts
		ORDER BY next_attempt_at ASC
		LIMIT $1
		FOR UPDATE SKIP LOCKED
	`, limit)
	if err != nil {
		return nil, err
	}
	var jobs []Job
	var ids []string
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		jobs = append(jobs, *j)
		ids = append(ids, j.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, tx.Commit(ctx)
	}
	_, err = tx.Exec(ctx, `
		UPDATE automation_jobs
		SET status='processing', locked_at=NOW(), locked_by=$2, attempts=attempts+1
		WHERE id = ANY($1::uuid[])
	`, ids, workerID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (r *Repository) CompleteJob(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE automation_jobs SET status='succeeded', last_error='', locked_at=NULL, locked_by='' WHERE id=$1
	`, id)
	return err
}

func (r *Repository) FailJob(ctx context.Context, id, errMsg string, retry bool, maxAttempts, attempts int) error {
	status := "failed"
	next := time.Now().UTC().Add(backoff(attempts))
	if !retry || attempts >= maxAttempts {
		status = "dead"
		next = time.Now().UTC()
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE automation_jobs
		SET status=$2, last_error=$3, next_attempt_at=$4, locked_at=NULL, locked_by=''
		WHERE id=$1
	`, id, status, truncate(errMsg, 2000), next)
	return err
}

func (r *Repository) RetryJob(ctx context.Context, id string) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE automation_jobs
		SET status='pending', next_attempt_at=NOW(), last_error='', locked_at=NULL, locked_by=''
		WHERE id=$1 AND status IN ('failed','dead')
	`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *Repository) InsertRun(ctx context.Context, run *Run) (string, error) {
	id := uuid.NewString()
	cond, _ := json.Marshal(run.Conditions)
	acts, _ := json.Marshal(run.Actions)
	results, _ := json.Marshal(run.ActionResults)
	_, err := r.pool.Exec(ctx, `
		INSERT INTO automation_runs (
			id, automation_id, automation_name, job_id, event_id, trigger_type, resource_type, resource_id,
			conditions_json, conditions_matched, actions_json, action_results, status, error_message, finished_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$11::jsonb,$12::jsonb,$13,$14,$15)
	`, id, emptyUUIDPtr(run.AutomationID), run.AutomationName, emptyUUIDPtr(run.JobID), emptyUUIDPtr(run.EventID),
		run.TriggerType, run.ResourceType, emptyUUIDPtr(run.ResourceID),
		string(cond), run.ConditionsMatched, string(acts), string(results), run.Status, run.ErrorMessage, time.Now().UTC())
	return id, err
}

func (r *Repository) ListRuns(ctx context.Context, automationID, status string, limit, offset int) ([]Run, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var total int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM automation_runs
		WHERE ($1='' OR automation_id::text=$1) AND ($2='' OR status=$2)
	`, automationID, status).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, automation_id::text, automation_name, job_id::text, event_id::text,
			trigger_type, resource_type, resource_id::text, conditions_json, conditions_matched,
			actions_json, action_results, status, error_message, started_at, finished_at
		FROM automation_runs
		WHERE ($1='' OR automation_id::text=$1) AND ($2='' OR status=$2)
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4
	`, automationID, status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *run)
	}
	if out == nil {
		out = []Run{}
	}
	return out, total, rows.Err()
}

func (r *Repository) ListJobs(ctx context.Context, status string, limit, offset int) ([]Job, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var total int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM automation_jobs WHERE ($1='' OR status=$1)
	`, status).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, event_id::text, automation_id::text, trigger_type, resource_type, resource_id::text,
			payload, status, attempts, max_attempts, next_attempt_at, last_error, created_at
		FROM automation_jobs
		WHERE ($1='' OR status=$1)
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *j)
	}
	if out == nil {
		out = []Job{}
	}
	return out, total, rows.Err()
}

func (r *Repository) GetJob(ctx context.Context, id string) (*Job, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id::text, event_id::text, automation_id::text, trigger_type, resource_type, resource_id::text,
			payload, status, attempts, max_attempts, next_attempt_at, last_error, created_at
		FROM automation_jobs WHERE id=$1
	`, id)
	return scanJob(row)
}

func (r *Repository) StageBelongsToPipeline(ctx context.Context, stageID, pipelineID string) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM pipeline_stages WHERE id=$1 AND pipeline_id=$2 AND is_active=TRUE)
	`, stageID, pipelineID).Scan(&ok)
	return ok, err
}

func (r *Repository) LoadEntityContext(ctx context.Context, resourceType, resourceID string) (map[string]any, error) {
	out := map[string]any{"resourceType": resourceType, "resourceId": resourceID}
	switch resourceType {
	case "lead":
		var pipelineID, stageID, ownerID, teamID, anzscoID *string
		var source, priority, fullName, email, status string
		var lastAct, nextAct *time.Time
		var createdAt, updatedAt time.Time
		var archived bool
		var slaHours *int
		err := r.pool.QueryRow(ctx, `
			SELECT l.pipeline_id::text, l.stage_id::text, l.owner_user_id::text, l.team_id::text, l.anzsco_id::text,
				l.source, l.priority, l.last_activity_at, l.created_at,
				l.full_name, COALESCE(l.email,''), l.next_activity_at, l.status, l.is_archived, l.updated_at, ps.sla_hours
			FROM leads l
			LEFT JOIN pipeline_stages ps ON ps.id = l.stage_id
			WHERE l.id=$1
		`, resourceID).Scan(&pipelineID, &stageID, &ownerID, &teamID, &anzscoID, &source, &priority, &lastAct, &createdAt, &fullName, &email, &nextAct, &status, &archived, &updatedAt, &slaHours)
		if err != nil {
			return nil, err
		}
		out["pipelineId"] = deref(pipelineID)
		out["stageId"] = deref(stageID)
		out["ownerUserId"] = deref(ownerID)
		out["teamId"] = deref(teamID)
		out["anzscoId"] = deref(anzscoID)
		out["source"] = source
		out["priority"] = priority
		out["fullName"] = fullName
		out["email"] = email
		out["inactivityDays"] = daysSince(lastAct, createdAt)
		out["attention"] = attention.Compute(attention.Input{
			Closed:    archived || status == "converted" || status == "archived" || status == "unqualified",
			Next:      nextAct,
			Last:      lastAct,
			Anchor:    updatedAt,
			CreatedAt: createdAt,
			SLAHours:  slaHours,
			Now:       time.Now().UTC(),
		})
		out["tags"] = []string{}
		var tags []string
		_ = r.pool.QueryRow(ctx, `SELECT COALESCE(tags,'{}') FROM leads WHERE id=$1`, resourceID).Scan(&tags)
		out["tags"] = tags
	case "deal":
		if err := r.loadDealContext(ctx, resourceID, out); err != nil {
			return nil, err
		}
	case "customer":
		var ownerID, teamID, anzscoID *string
		var source, fullName, email string
		var nextFollow, lastContact *time.Time
		var createdAt time.Time
		var archived bool
		err := r.pool.QueryRow(ctx, `
			SELECT owner_user_id::text, team_id::text, anzsco_id::text, COALESCE(source,''),
				full_name, COALESCE(email,''), next_follow_up_at, last_contacted_at, is_archived, created_at
			FROM customers WHERE id=$1
		`, resourceID).Scan(&ownerID, &teamID, &anzscoID, &source, &fullName, &email, &nextFollow, &lastContact, &archived, &createdAt)
		if err != nil {
			return nil, err
		}
		out["ownerUserId"] = deref(ownerID)
		out["teamId"] = deref(teamID)
		out["anzscoId"] = deref(anzscoID)
		out["source"] = source
		out["fullName"] = fullName
		out["email"] = email
		out["attention"] = attention.Compute(attention.Input{
			Closed:    archived,
			Next:      nextFollow,
			Last:      lastContact,
			CreatedAt: createdAt,
			Now:       time.Now().UTC(),
		})
	case "activity":
		var leadID, dealID, customerID, ownerID *string
		err := r.pool.QueryRow(ctx, `
			SELECT lead_id::text, deal_id::text, customer_id::text, COALESCE(owner_user_id, actor_user_id)::text
			FROM activities WHERE id=$1
		`, resourceID).Scan(&leadID, &dealID, &customerID, &ownerID)
		if err != nil {
			return nil, err
		}
		out["leadId"] = deref(leadID)
		out["dealId"] = deref(dealID)
		out["customerId"] = deref(customerID)
		out["ownerUserId"] = deref(ownerID)
		if leadID != nil {
			ctxLead, err := r.LoadEntityContext(ctx, "lead", *leadID)
			if err == nil {
				for k, v := range ctxLead {
					if _, exists := out[k]; !exists || out[k] == "" {
						out[k] = v
					}
				}
			}
		} else if dealID != nil {
			ctxDeal, err := r.LoadEntityContext(ctx, "deal", *dealID)
			if err == nil {
				for k, v := range ctxDeal {
					if _, exists := out[k]; !exists || out[k] == "" {
						out[k] = v
					}
				}
			}
		}
	case "document":
		var leadID, dealID, customerID *string
		err := r.pool.QueryRow(ctx, `
			SELECT lead_id::text, deal_id::text, customer_id::text FROM documents WHERE id=$1
		`, resourceID).Scan(&leadID, &dealID, &customerID)
		if err != nil {
			return nil, err
		}
		out["leadId"] = deref(leadID)
		out["dealId"] = deref(dealID)
		out["customerId"] = deref(customerID)
		if leadID != nil {
			ctxLead, _ := r.LoadEntityContext(ctx, "lead", *leadID)
			for k, v := range ctxLead {
				out[k] = v
			}
		} else if dealID != nil {
			ctxDeal, _ := r.LoadEntityContext(ctx, "deal", *dealID)
			for k, v := range ctxDeal {
				out[k] = v
			}
		}
	}
	return out, nil
}

// Fix deal LoadEntityContext - I had a bug with customer_id scan. Rewrite that case cleanly.
func (r *Repository) loadDealContext(ctx context.Context, resourceID string, out map[string]any) error {
	var pipelineID, stageID, ownerID, teamID, customerID *string
	var source, priority, title, status, fullName, email string
	var value *float64
	var lastAct, nextAct *time.Time
	var createdAt, stageEntered time.Time
	var slaHours *int
	err := r.pool.QueryRow(ctx, `
		SELECT d.pipeline_id::text, d.stage_id::text, d.owner_user_id::text, d.team_id::text, d.customer_id::text,
			d.source, d.priority, d.value, d.last_activity_at, d.created_at,
			d.title, d.status, d.next_activity_at, d.stage_entered_at, ps.sla_hours,
			COALESCE(c.full_name,''), COALESCE(c.email,'')
		FROM deals d
		LEFT JOIN pipeline_stages ps ON ps.id = d.stage_id
		LEFT JOIN customers c ON c.id = d.customer_id
		WHERE d.id=$1
	`, resourceID).Scan(
		&pipelineID, &stageID, &ownerID, &teamID, &customerID,
		&source, &priority, &value, &lastAct, &createdAt,
		&title, &status, &nextAct, &stageEntered, &slaHours,
		&fullName, &email,
	)
	if err != nil {
		return err
	}
	out["pipelineId"] = deref(pipelineID)
	out["stageId"] = deref(stageID)
	out["ownerUserId"] = deref(ownerID)
	out["teamId"] = deref(teamID)
	out["customerId"] = deref(customerID)
	out["source"] = source
	out["priority"] = priority
	out["dealTitle"] = title
	out["fullName"] = fullName
	out["email"] = email
	if value != nil {
		out["dealValue"] = *value
	} else {
		out["dealValue"] = 0.0
	}
	out["inactivityDays"] = daysSince(lastAct, createdAt)
	anchor := stageEntered
	if anchor.IsZero() {
		anchor = createdAt
	}
	out["attention"] = attention.Compute(attention.Input{
		Closed:    status != "open",
		Next:      nextAct,
		Last:      lastAct,
		Anchor:    anchor,
		CreatedAt: createdAt,
		SLAHours:  slaHours,
		Now:       time.Now().UTC(),
	})
	return nil
}

func (r *Repository) FindOverdueActivities(ctx context.Context, limit int) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT a.id::text
		FROM activities a
		WHERE (a.status='overdue' OR (a.status IN ('upcoming','due') AND a.due_at IS NOT NULL AND a.due_at < NOW()))
		  AND NOT EXISTS (
			SELECT 1 FROM automation_events e
			WHERE e.event_type=$1 AND e.resource_id=a.id
			  AND e.created_at > NOW() - INTERVAL '12 hours'
		  )
		ORDER BY a.due_at ASC NULLS LAST
		LIMIT $2
	`, TriggerActivityOverdue, limit)
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

func (r *Repository) FindInactiveDeals(ctx context.Context, inactivityDays, limit int) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT d.id::text
		FROM deals d
		WHERE d.status='open'
		  AND COALESCE(d.last_activity_at, d.created_at) < NOW() - ($1 || ' days')::interval
		  AND NOT EXISTS (
			SELECT 1 FROM automation_events e
			WHERE e.event_type=$2 AND e.resource_id=d.id
			  AND e.created_at > NOW() - INTERVAL '24 hours'
		  )
		ORDER BY COALESCE(d.last_activity_at, d.created_at) ASC
		LIMIT $3
	`, inactivityDays, TriggerDealInactive, limit)
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

type scannable interface {
	Scan(dest ...any) error
}

func scanAutomation(row scannable) (*Automation, error) {
	var a Automation
	var cond, acts []byte
	var createdBy, updatedBy *string
	var createdAt, updatedAt time.Time
	if err := row.Scan(&a.ID, &a.Name, &a.Description, &a.IsActive, &a.TriggerType, &cond, &acts,
		&createdBy, &updatedBy, &createdAt, &updatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal(cond, &a.Conditions)
	_ = json.Unmarshal(acts, &a.Actions)
	if a.Conditions == nil {
		a.Conditions = []Condition{}
	}
	if a.Actions == nil {
		a.Actions = []Action{}
	}
	a.CreatedBy = createdBy
	a.UpdatedBy = updatedBy
	a.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	a.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return &a, nil
}

func scanJob(row scannable) (*Job, error) {
	var j Job
	var eventID, autoID, resourceID *string
	var payload []byte
	var nextAt, createdAt time.Time
	if err := row.Scan(&j.ID, &eventID, &autoID, &j.TriggerType, &j.ResourceType, &resourceID,
		&payload, &j.Status, &j.Attempts, &j.MaxAttempts, &nextAt, &j.LastError, &createdAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	j.EventID = eventID
	j.AutomationID = autoID
	j.ResourceID = resourceID
	_ = json.Unmarshal(payload, &j.Payload)
	if j.Payload == nil {
		j.Payload = map[string]any{}
	}
	j.NextAttemptAt = nextAt.UTC().Format(time.RFC3339)
	j.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	return &j, nil
}

func scanRun(row scannable) (*Run, error) {
	var run Run
	var autoID, jobID, eventID, resourceID *string
	var cond, acts, results []byte
	var startedAt time.Time
	var finishedAt *time.Time
	if err := row.Scan(&run.ID, &autoID, &run.AutomationName, &jobID, &eventID,
		&run.TriggerType, &run.ResourceType, &resourceID, &cond, &run.ConditionsMatched,
		&acts, &results, &run.Status, &run.ErrorMessage, &startedAt, &finishedAt); err != nil {
		return nil, err
	}
	run.AutomationID = autoID
	run.JobID = jobID
	run.EventID = eventID
	run.ResourceID = resourceID
	_ = json.Unmarshal(cond, &run.Conditions)
	_ = json.Unmarshal(acts, &run.Actions)
	_ = json.Unmarshal(results, &run.ActionResults)
	if run.Conditions == nil {
		run.Conditions = []Condition{}
	}
	if run.Actions == nil {
		run.Actions = []Action{}
	}
	if run.ActionResults == nil {
		run.ActionResults = []ActionResult{}
	}
	run.StartedAt = startedAt.UTC().Format(time.RFC3339)
	if finishedAt != nil {
		s := finishedAt.UTC().Format(time.RFC3339)
		run.FinishedAt = &s
	}
	return &run, nil
}

func emptyUUID(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func emptyUUIDPtr(s *string) any {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	return *s
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func daysSince(last *time.Time, created time.Time) float64 {
	ref := created
	if last != nil {
		ref = *last
	}
	return time.Since(ref).Hours() / 24
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func backoff(attempts int) time.Duration {
	switch {
	case attempts <= 1:
		return 30 * time.Second
	case attempts == 2:
		return 2 * time.Minute
	case attempts == 3:
		return 10 * time.Minute
	default:
		return 30 * time.Minute
	}
}
