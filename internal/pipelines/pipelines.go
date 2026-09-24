package pipelines

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/crm/backend/internal/audit"
	"github.com/crm/backend/internal/automation"
	"github.com/crm/backend/pkg/apperrors"
)

type Stage struct {
	ID                 string   `json:"id"`
	PipelineID         string   `json:"pipelineId"`
	Name               string   `json:"name"`
	Position           int      `json:"position"`
	Probability        float64  `json:"probability"`
	VisualAccent       string   `json:"visualAccent"`
	RequiredFields     []string `json:"requiredFields"`
	RequiredActivities []string `json:"requiredActivities"`
	RequiredDocuments  []string `json:"requiredDocuments"`
	SLAHours           *int     `json:"slaHours"`
	IsWon              bool     `json:"isWon"`
	IsLost             bool     `json:"isLost"`
	IsActive           bool     `json:"isActive"`
}

type Pipeline struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Kind        string    `json:"kind"`
	IsDefault   bool      `json:"isDefault"`
	IsActive    bool      `json:"isActive"`
	Stages      []Stage   `json:"stages"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type CreatePipelineInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	IsDefault   bool   `json:"isDefault"`
	IsActive    *bool  `json:"isActive"`
}

type UpdatePipelineInput struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Kind        *string `json:"kind"`
	IsDefault   *bool   `json:"isDefault"`
	IsActive    *bool   `json:"isActive"`
}

type CreateStageInput struct {
	Name               string   `json:"name"`
	Position           *int     `json:"position"`
	Probability        *float64 `json:"probability"`
	VisualAccent       string   `json:"visualAccent"`
	RequiredFields     []string `json:"requiredFields"`
	RequiredActivities []string `json:"requiredActivities"`
	RequiredDocuments  []string `json:"requiredDocuments"`
	SLAHours           *int     `json:"slaHours"`
	IsWon              bool     `json:"isWon"`
	IsLost             bool     `json:"isLost"`
	IsActive           *bool    `json:"isActive"`
}

type UpdateStageInput struct {
	Name               *string  `json:"name"`
	Position           *int     `json:"position"`
	Probability        *float64 `json:"probability"`
	VisualAccent       *string  `json:"visualAccent"`
	RequiredFields     []string `json:"requiredFields"`
	RequiredActivities []string `json:"requiredActivities"`
	RequiredDocuments  []string `json:"requiredDocuments"`
	SLAHours           *int     `json:"slaHours"`
	ClearSLA           bool     `json:"clearSla"`
	IsWon              *bool    `json:"isWon"`
	IsLost             *bool    `json:"isLost"`
	IsActive           *bool    `json:"isActive"`
}

type ReorderStagesInput struct {
	StageIDs []string `json:"stageIds"`
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) List(ctx context.Context, kind string, includeInactive bool) ([]Pipeline, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, name, description, kind, is_default, is_active, created_at, updated_at
		FROM pipelines
		WHERE ($1 = '' OR kind = $1)
		  AND ($2 OR is_active = TRUE)
		ORDER BY is_default DESC, name
	`, kind, includeInactive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pipes []Pipeline
	for rows.Next() {
		var p Pipeline
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Kind, &p.IsDefault, &p.IsActive, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		pipes = append(pipes, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range pipes {
		stages, err := r.ListStages(ctx, pipes[i].ID, includeInactive)
		if err != nil {
			return nil, err
		}
		pipes[i].Stages = stages
	}
	if pipes == nil {
		pipes = []Pipeline{}
	}
	return pipes, nil
}

func (r *Repository) Get(ctx context.Context, id string) (*Pipeline, error) {
	var p Pipeline
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, name, description, kind, is_default, is_active, created_at, updated_at
		FROM pipelines WHERE id = $1
	`, id).Scan(&p.ID, &p.Name, &p.Description, &p.Kind, &p.IsDefault, &p.IsActive, &p.CreatedAt, &p.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	stages, err := r.ListStages(ctx, id, true)
	if err != nil {
		return nil, err
	}
	p.Stages = stages
	return &p, nil
}

func (r *Repository) ListStages(ctx context.Context, pipelineID string, includeInactive bool) ([]Stage, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, pipeline_id::text, name, position, probability, visual_accent,
			required_fields, required_activities, required_documents, sla_hours,
			is_won, is_lost, is_active
		FROM pipeline_stages
		WHERE pipeline_id = $1 AND ($2 OR is_active = TRUE)
		ORDER BY position, created_at
	`, pipelineID, includeInactive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var stages []Stage
	for rows.Next() {
		var s Stage
		if err := rows.Scan(
			&s.ID, &s.PipelineID, &s.Name, &s.Position, &s.Probability, &s.VisualAccent,
			&s.RequiredFields, &s.RequiredActivities, &s.RequiredDocuments, &s.SLAHours,
			&s.IsWon, &s.IsLost, &s.IsActive,
		); err != nil {
			return nil, err
		}
		if s.RequiredFields == nil {
			s.RequiredFields = []string{}
		}
		if s.RequiredActivities == nil {
			s.RequiredActivities = []string{}
		}
		if s.RequiredDocuments == nil {
			s.RequiredDocuments = []string{}
		}
		stages = append(stages, s)
	}
	if stages == nil {
		stages = []Stage{}
	}
	return stages, rows.Err()
}

func (r *Repository) GetStage(ctx context.Context, id string) (*Stage, error) {
	var s Stage
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, pipeline_id::text, name, position, probability, visual_accent,
			required_fields, required_activities, required_documents, sla_hours,
			is_won, is_lost, is_active
		FROM pipeline_stages WHERE id = $1
	`, id).Scan(
		&s.ID, &s.PipelineID, &s.Name, &s.Position, &s.Probability, &s.VisualAccent,
		&s.RequiredFields, &s.RequiredActivities, &s.RequiredDocuments, &s.SLAHours,
		&s.IsWon, &s.IsLost, &s.IsActive,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if s.RequiredFields == nil {
		s.RequiredFields = []string{}
	}
	if s.RequiredActivities == nil {
		s.RequiredActivities = []string{}
	}
	if s.RequiredDocuments == nil {
		s.RequiredDocuments = []string{}
	}
	return &s, nil
}

func (r *Repository) Create(ctx context.Context, in CreatePipelineInput) (*Pipeline, error) {
	id := uuid.NewString()
	active := true
	if in.IsActive != nil {
		active = *in.IsActive
	}
	kind := in.Kind
	if kind == "" {
		kind = "sales"
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if in.IsDefault {
		if _, err := tx.Exec(ctx, `UPDATE pipelines SET is_default = FALSE WHERE kind = $1`, kind); err != nil {
			return nil, err
		}
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO pipelines (id, name, description, kind, is_default, is_active)
		VALUES ($1,$2,$3,$4,$5,$6)
	`, id, strings.TrimSpace(in.Name), in.Description, kind, in.IsDefault, active)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *Repository) Update(ctx context.Context, id string, current *Pipeline, in UpdatePipelineInput) (*Pipeline, error) {
	name := current.Name
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	desc := current.Description
	if in.Description != nil {
		desc = *in.Description
	}
	kind := current.Kind
	if in.Kind != nil && *in.Kind != "" {
		kind = *in.Kind
	}
	isDefault := current.IsDefault
	if in.IsDefault != nil {
		isDefault = *in.IsDefault
	}
	active := current.IsActive
	if in.IsActive != nil {
		active = *in.IsActive
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if isDefault {
		if _, err := tx.Exec(ctx, `UPDATE pipelines SET is_default = FALSE WHERE kind = $1 AND id <> $2`, kind, id); err != nil {
			return nil, err
		}
	}
	_, err = tx.Exec(ctx, `
		UPDATE pipelines SET name=$2, description=$3, kind=$4, is_default=$5, is_active=$6 WHERE id=$1
	`, id, name, desc, kind, isDefault, active)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *Repository) CreateStage(ctx context.Context, pipelineID string, in CreateStageInput) (*Stage, error) {
	id := uuid.NewString()
	pos := 0
	if in.Position != nil {
		pos = *in.Position
	} else {
		_ = r.pool.QueryRow(ctx, `SELECT COALESCE(MAX(position),0)+1 FROM pipeline_stages WHERE pipeline_id=$1`, pipelineID).Scan(&pos)
	}
	prob := 0.0
	if in.Probability != nil {
		prob = *in.Probability
	}
	accent := in.VisualAccent
	if accent == "" {
		accent = "neutral"
	}
	active := true
	if in.IsActive != nil {
		active = *in.IsActive
	}
	fields := in.RequiredFields
	if fields == nil {
		fields = []string{}
	}
	acts := in.RequiredActivities
	if acts == nil {
		acts = []string{}
	}
	docs := in.RequiredDocuments
	if docs == nil {
		docs = []string{}
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO pipeline_stages (
			id, pipeline_id, name, position, probability, visual_accent,
			required_fields, required_activities, required_documents, sla_hours,
			is_won, is_lost, is_active
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
	`, id, pipelineID, strings.TrimSpace(in.Name), pos, prob, accent, fields, acts, docs, in.SLAHours, in.IsWon, in.IsLost, active)
	if err != nil {
		return nil, err
	}
	return r.GetStage(ctx, id)
}

func (r *Repository) UpdateStage(ctx context.Context, id string, current *Stage, in UpdateStageInput) (*Stage, error) {
	name := current.Name
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	pos := current.Position
	if in.Position != nil {
		pos = *in.Position
	}
	prob := current.Probability
	if in.Probability != nil {
		prob = *in.Probability
	}
	accent := current.VisualAccent
	if in.VisualAccent != nil && *in.VisualAccent != "" {
		accent = *in.VisualAccent
	}
	fields := current.RequiredFields
	if in.RequiredFields != nil {
		fields = in.RequiredFields
	}
	acts := current.RequiredActivities
	if in.RequiredActivities != nil {
		acts = in.RequiredActivities
	}
	docs := current.RequiredDocuments
	if in.RequiredDocuments != nil {
		docs = in.RequiredDocuments
	}
	sla := current.SLAHours
	if in.ClearSLA {
		sla = nil
	} else if in.SLAHours != nil {
		sla = in.SLAHours
	}
	isWon := current.IsWon
	if in.IsWon != nil {
		isWon = *in.IsWon
	}
	isLost := current.IsLost
	if in.IsLost != nil {
		isLost = *in.IsLost
	}
	active := current.IsActive
	if in.IsActive != nil {
		active = *in.IsActive
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE pipeline_stages SET
			name=$2, position=$3, probability=$4, visual_accent=$5,
			required_fields=$6, required_activities=$7, required_documents=$8, sla_hours=$9,
			is_won=$10, is_lost=$11, is_active=$12
		WHERE id=$1
	`, id, name, pos, prob, accent, fields, acts, docs, sla, isWon, isLost, active)
	if err != nil {
		return nil, err
	}
	return r.GetStage(ctx, id)
}

func (r *Repository) ReorderStages(ctx context.Context, pipelineID string, stageIDs []string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for i, sid := range stageIDs {
		tag, err := tx.Exec(ctx, `
			UPDATE pipeline_stages SET position=$3 WHERE id=$1 AND pipeline_id=$2
		`, sid, pipelineID, i+1)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperrors.Validation("stage does not belong to pipeline")
		}
	}
	return tx.Commit(ctx)
}

func (r *Repository) StageBelongsToPipeline(ctx context.Context, stageID, pipelineID string) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM pipeline_stages
			WHERE id=$1 AND pipeline_id=$2 AND is_active = TRUE
		)
	`, stageID, pipelineID).Scan(&ok)
	return ok, err
}

type Service struct {
	repo  *Repository
	audit *audit.Service
	hooks *automation.Emitter
}

func NewService(repo *Repository, auditSvc *audit.Service, hooks *automation.Emitter) *Service {
	return &Service{repo: repo, audit: auditSvc, hooks: hooks}
}

func (s *Service) List(ctx context.Context, kind string, includeInactive bool) ([]Pipeline, error) {
	items, err := s.repo.List(ctx, kind, includeInactive)
	if err != nil {
		return nil, apperrors.Internal("failed to list pipelines", err)
	}
	return items, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Pipeline, error) {
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, apperrors.Internal("failed to load pipeline", err)
	}
	if p == nil {
		return nil, apperrors.NotFound("pipeline not found")
	}
	return p, nil
}

func (s *Service) Create(ctx context.Context, actorID string, in CreatePipelineInput, ip, ua string) (*Pipeline, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, apperrors.Validation("name is required")
	}
	if in.Kind == "" {
		in.Kind = "sales"
	}
	if in.Kind != "sales" && in.Kind != "leads" {
		return nil, apperrors.Validation("kind must be sales or leads")
	}
	p, err := s.repo.Create(ctx, in)
	if err != nil {
		return nil, apperrors.Internal("failed to create pipeline", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "pipeline.created", "pipeline", audit.Ptr(p.ID), map[string]any{
		"name": p.Name, "kind": p.Kind,
	}, ip, ua)
	_ = s.hooks.Emit(ctx, automation.EventPipelineCreated, "pipeline", p.ID, map[string]any{"name": p.Name})
	return p, nil
}

func (s *Service) Update(ctx context.Context, actorID, id string, in UpdatePipelineInput, ip, ua string) (*Pipeline, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	p, err := s.repo.Update(ctx, id, current, in)
	if err != nil {
		return nil, apperrors.Internal("failed to update pipeline", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "pipeline.updated", "pipeline", audit.Ptr(id), map[string]any{
		"name": p.Name, "isActive": p.IsActive,
	}, ip, ua)
	_ = s.hooks.Emit(ctx, automation.EventPipelineUpdated, "pipeline", id, map[string]any{"name": p.Name})
	return p, nil
}

func (s *Service) CreateStage(ctx context.Context, actorID, pipelineID string, in CreateStageInput, ip, ua string) (*Stage, error) {
	if _, err := s.Get(ctx, pipelineID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, apperrors.Validation("stage name is required")
	}
	st, err := s.repo.CreateStage(ctx, pipelineID, in)
	if err != nil {
		return nil, apperrors.Internal("failed to create stage", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "pipeline.stage_created", "pipeline_stage", audit.Ptr(st.ID), map[string]any{
		"pipelineId": pipelineID, "name": st.Name,
	}, ip, ua)
	return st, nil
}

func (s *Service) UpdateStage(ctx context.Context, actorID, stageID string, in UpdateStageInput, ip, ua string) (*Stage, error) {
	current, err := s.repo.GetStage(ctx, stageID)
	if err != nil {
		return nil, apperrors.Internal("failed to load stage", err)
	}
	if current == nil {
		return nil, apperrors.NotFound("stage not found")
	}
	st, err := s.repo.UpdateStage(ctx, stageID, current, in)
	if err != nil {
		return nil, apperrors.Internal("failed to update stage", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "pipeline.stage_updated", "pipeline_stage", audit.Ptr(stageID), map[string]any{
		"name": st.Name, "position": st.Position,
	}, ip, ua)
	return st, nil
}

func (s *Service) ReorderStages(ctx context.Context, actorID, pipelineID string, in ReorderStagesInput, ip, ua string) (*Pipeline, error) {
	if _, err := s.Get(ctx, pipelineID); err != nil {
		return nil, err
	}
	if len(in.StageIDs) == 0 {
		return nil, apperrors.Validation("stageIds required")
	}
	if err := s.repo.ReorderStages(ctx, pipelineID, in.StageIDs); err != nil {
		if app, ok := apperrors.AsAppError(err); ok {
			return nil, app
		}
		return nil, apperrors.Internal("failed to reorder stages", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "pipeline.stages_reordered", "pipeline", audit.Ptr(pipelineID), map[string]any{
		"stageIds": in.StageIDs,
	}, ip, ua)
	return s.Get(ctx, pipelineID)
}

// Repo exposes repository for deal move validation.
func (s *Service) Repo() *Repository {
	return s.repo
}
