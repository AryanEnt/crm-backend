package automation

import (
	"context"
	"strings"

	"github.com/crm/backend/internal/audit"
	"github.com/crm/backend/pkg/apperrors"
)

type Service struct {
	repo     *Repository
	executor *Executor
	worker   *Worker
	audit    *audit.Service
}

func NewService(repo *Repository, executor *Executor, worker *Worker, auditSvc *audit.Service) *Service {
	return &Service{repo: repo, executor: executor, worker: worker, audit: auditSvc}
}

func (s *Service) Catalog() Catalog {
	return BuilderCatalog()
}

func (s *Service) List(ctx context.Context, trigger string, activeOnly bool, limit, offset int) ([]Automation, int, error) {
	items, total, err := s.repo.ListAutomations(ctx, trigger, activeOnly, limit, offset)
	if err != nil {
		return nil, 0, apperrors.Internal("failed to list automations", err)
	}
	return items, total, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Automation, error) {
	a, err := s.repo.GetAutomation(ctx, id)
	if err != nil {
		return nil, apperrors.Internal("failed to load automation", err)
	}
	if a == nil {
		return nil, apperrors.NotFound("automation not found")
	}
	return a, nil
}

func (s *Service) Create(ctx context.Context, actorID string, in CreateInput, ip, ua string) (*Automation, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, apperrors.Validation("name is required")
	}
	if in.Conditions == nil {
		in.Conditions = []Condition{}
	}
	if err := ValidateDefinition(in.TriggerType, in.Conditions, in.Actions); err != nil {
		return nil, err
	}
	if err := s.validateStageActions(ctx, in.Actions); err != nil {
		return nil, err
	}
	a, err := s.repo.CreateAutomation(ctx, actorID, in)
	if err != nil {
		return nil, apperrors.Internal("failed to create automation", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "automation.created", "automation", audit.Ptr(a.ID), map[string]any{
		"name": a.Name, "triggerType": a.TriggerType,
	}, ip, ua)
	return a, nil
}

func (s *Service) Update(ctx context.Context, actorID, id string, in UpdateInput, ip, ua string) (*Automation, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	trigger := current.TriggerType
	conds := current.Conditions
	acts := current.Actions
	if in.TriggerType != nil {
		trigger = *in.TriggerType
	}
	if in.Conditions != nil {
		conds = *in.Conditions
	}
	if in.Actions != nil {
		acts = *in.Actions
	}
	if err := ValidateDefinition(trigger, conds, acts); err != nil {
		return nil, err
	}
	if err := s.validateStageActions(ctx, acts); err != nil {
		return nil, err
	}
	a, err := s.repo.UpdateAutomation(ctx, actorID, id, current, in)
	if err != nil {
		return nil, apperrors.Internal("failed to update automation", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "automation.updated", "automation", audit.Ptr(id), map[string]any{
		"name": a.Name, "isActive": a.IsActive,
	}, ip, ua)
	return a, nil
}

func (s *Service) Delete(ctx context.Context, actorID, id string, ip, ua string) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	if err := s.repo.DeleteAutomation(ctx, id); err != nil {
		return apperrors.Internal("failed to delete automation", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "automation.deleted", "automation", audit.Ptr(id), nil, ip, ua)
	return nil
}

func (s *Service) ListRuns(ctx context.Context, automationID, status string, limit, offset int) ([]Run, int, error) {
	items, total, err := s.repo.ListRuns(ctx, automationID, status, limit, offset)
	if err != nil {
		return nil, 0, apperrors.Internal("failed to list automation runs", err)
	}
	return items, total, nil
}

func (s *Service) ListJobs(ctx context.Context, status string, limit, offset int) ([]Job, int, error) {
	items, total, err := s.repo.ListJobs(ctx, status, limit, offset)
	if err != nil {
		return nil, 0, apperrors.Internal("failed to list automation jobs", err)
	}
	return items, total, nil
}

func (s *Service) RetryJob(ctx context.Context, actorID, id string, ip, ua string) (*Job, error) {
	job, err := s.repo.GetJob(ctx, id)
	if err != nil {
		return nil, apperrors.Internal("failed to load job", err)
	}
	if job == nil {
		return nil, apperrors.NotFound("job not found")
	}
	if err := s.repo.RetryJob(ctx, id); err != nil {
		return nil, apperrors.Validation("job cannot be retried")
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "automation.job_retry", "automation_job", audit.Ptr(id), nil, ip, ua)
	if s.worker != nil {
		s.worker.Wake()
	}
	return s.repo.GetJob(ctx, id)
}

func (s *Service) validateStageActions(ctx context.Context, actions []Action) error {
	for _, a := range actions {
		if a.Type != ActionChangeStage {
			continue
		}
		stageID := asString(a.Params["stageId"])
		pipelineID := asString(a.Params["pipelineId"])
		if pipelineID == "" {
			// Without pipeline we validate existence of stage only
			var pipe string
			err := s.repo.pool.QueryRow(ctx, `
				SELECT pipeline_id::text FROM pipeline_stages WHERE id=$1 AND is_active=TRUE
			`, stageID).Scan(&pipe)
			if err != nil || pipe == "" {
				return apperrors.Validation("impossible stage transition: stage not found or inactive")
			}
			continue
		}
		ok, err := s.repo.StageBelongsToPipeline(ctx, stageID, pipelineID)
		if err != nil {
			return apperrors.Internal("failed to validate stage", err)
		}
		if !ok {
			return apperrors.Validation("impossible stage transition: stage is not an active stage on the selected pipeline")
		}
	}
	return nil
}
