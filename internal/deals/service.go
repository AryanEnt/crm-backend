package deals

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/crm/backend/internal/audit"
	"github.com/crm/backend/internal/automation"
	"github.com/crm/backend/internal/pipelines"
	"github.com/crm/backend/internal/systemactivity"
	"github.com/crm/backend/internal/timeline"
	"github.com/crm/backend/pkg/apperrors"
)

type Service struct {
	repo        *Repository
	pipelines   *pipelines.Service
	audit       *audit.Service
	hooks       *automation.Emitter
	timeline    *timeline.Service
	sysActivity *systemactivity.Service
}

func NewService(repo *Repository, pipelinesSvc *pipelines.Service, auditSvc *audit.Service, hooks *automation.Emitter, timelineSvc *timeline.Service, sysActivity *systemactivity.Service) *Service {
	return &Service{repo: repo, pipelines: pipelinesSvc, audit: auditSvc, hooks: hooks, timeline: timelineSvc, sysActivity: sysActivity}
}

func (s *Service) recordSystemActivity(ctx context.Context, in systemactivity.WriteInput) {
	if s.sysActivity != nil {
		s.sysActivity.Record(ctx, in)
	}
}

func (s *Service) List(ctx context.Context, f ListFilter) ([]Deal, int, error) {
	items, total, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, 0, apperrors.Internal("failed to list deals", err)
	}
	return items, total, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Deal, error) {
	d, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, apperrors.Internal("failed to load deal", err)
	}
	if d == nil {
		return nil, apperrors.NotFound("deal not found")
	}
	return d, nil
}

func (s *Service) GetDetail(ctx context.Context, id string) (*DealDetail, error) {
	d, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	customer, err := s.repo.CustomerSnapshot(ctx, d.CustomerID)
	if err != nil {
		return nil, apperrors.Internal("failed to load customer", err)
	}
	transitions, err := s.repo.ListTransitions(ctx, id)
	if err != nil {
		return nil, apperrors.Internal("failed to load transitions", err)
	}
	activities, err := s.repo.ListActivities(ctx, id)
	if err != nil {
		return nil, apperrors.Internal("failed to load activities", err)
	}
	docs, err := s.repo.ListDocuments(ctx, id)
	if err != nil {
		return nil, apperrors.Internal("failed to load documents", err)
	}
	var pipe *pipelines.Pipeline
	if d.PipelineID != nil {
		pipe, err = s.pipelines.Get(ctx, *d.PipelineID)
		if err != nil {
			return nil, err
		}
	}
	return &DealDetail{
		Deal: d, Customer: customer, Transitions: transitions,
		Activities: activities, Documents: docs, Pipeline: pipe,
	}, nil
}

func (s *Service) Board(ctx context.Context, pipelineID string) (*Board, error) {
	pipe, err := s.pipelines.Get(ctx, pipelineID)
	if err != nil {
		return nil, err
	}
	deals, err := s.repo.Board(ctx, pipelineID)
	if err != nil {
		return nil, apperrors.Internal("failed to load board", err)
	}
	byStage := map[string][]Deal{}
	for _, d := range deals {
		key := ""
		if d.StageID != nil {
			key = *d.StageID
		}
		byStage[key] = append(byStage[key], d)
	}
	cols := make([]BoardColumn, 0, len(pipe.Stages))
	for _, st := range pipe.Stages {
		if !st.IsActive {
			continue
		}
		items := byStage[st.ID]
		if items == nil {
			items = []Deal{}
		}
		cols = append(cols, BoardColumn{Stage: st, Deals: items})
	}
	return &Board{Pipeline: pipe, Columns: cols}, nil
}

func (s *Service) Create(ctx context.Context, actorID string, in CreateInput, ip, ua string) (*Deal, error) {
	if strings.TrimSpace(in.CustomerID) == "" || strings.TrimSpace(in.Title) == "" {
		return nil, apperrors.Validation("customerId and title are required")
	}
	if in.PipelineID != nil && *in.PipelineID != "" && in.StageID != nil && *in.StageID != "" {
		ok, err := s.pipelines.Repo().StageBelongsToPipeline(ctx, *in.StageID, *in.PipelineID)
		if err != nil {
			return nil, apperrors.Internal("failed to validate stage", err)
		}
		if !ok {
			return nil, apperrors.Validation("stage does not belong to pipeline or is inactive")
		}
		st, err := s.pipelines.Repo().GetStage(ctx, *in.StageID)
		if err != nil {
			return nil, apperrors.Internal("failed to load stage", err)
		}
		if in.Probability == nil && st != nil {
			p := st.Probability
			in.Probability = &p
		}
	}
	var closeDate *time.Time
	if in.ExpectedCloseAt != nil && strings.TrimSpace(*in.ExpectedCloseAt) != "" {
		t, err := time.Parse("2006-01-02", strings.TrimSpace(*in.ExpectedCloseAt))
		if err != nil {
			return nil, apperrors.Validation("invalid expectedCloseAt")
		}
		utc := t.UTC()
		closeDate = &utc
	}
	fieldJSON, _ := json.Marshal(in.FieldValues)
	if in.FieldValues == nil {
		fieldJSON = []byte("{}")
	}
	d, err := s.repo.Create(ctx, in, closeDate, fieldJSON)
	if err != nil {
		return nil, apperrors.Internal("failed to create deal", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "deal.created", "deal", audit.Ptr(d.ID), map[string]any{
		"customerId": d.CustomerID, "title": d.Title, "pipelineId": d.PipelineID, "stageId": d.StageID,
	}, ip, ua)
	_ = s.hooks.Emit(ctx, automation.EventDealCreated, "deal", d.ID, map[string]any{
		"customerId": d.CustomerID, "title": d.Title,
	})
	if s.timeline != nil {
		_, _ = s.timeline.Record(ctx, timeline.WriteInput{
			EventType: timeline.EventDealCreated, Title: "Deal created", Body: d.Title,
			ActorUserID: audit.Ptr(actorID), Source: "crm", CustomerID: &d.CustomerID, DealID: &d.ID,
		})
		_, _ = s.timeline.Record(ctx, timeline.WriteInput{
			EventType: timeline.EventAutomation, Title: "Automation event", Body: "deal.created",
			ActorUserID: audit.Ptr(actorID), Source: "automation", CustomerID: &d.CustomerID, DealID: &d.ID,
			Metadata: map[string]any{"event": automation.EventDealCreated},
		})
	}
	s.recordSystemActivity(ctx, systemactivity.WriteInput{
		EventType: "deal.created", Title: "Deal created",
		ActorUserID: actorID, EntityType: "deal", EntityID: d.ID, EntityLabel: d.Title,
		Metadata: map[string]any{"customerId": d.CustomerID},
	})
	return d, nil
}

func (s *Service) Update(ctx context.Context, actorID, id string, in UpdateInput, ip, ua string) (*Deal, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.PipelineID != nil && in.StageID != nil && *in.PipelineID != "" && *in.StageID != "" {
		ok, err := s.pipelines.Repo().StageBelongsToPipeline(ctx, *in.StageID, *in.PipelineID)
		if err != nil {
			return nil, apperrors.Internal("failed to validate stage", err)
		}
		if !ok {
			return nil, apperrors.Validation("stage does not belong to pipeline or is inactive")
		}
	}
	var closeDate *time.Time
	setClose := false
	if in.ClearCloseDate {
		setClose = true
		closeDate = nil
	} else if in.ExpectedCloseAt != nil {
		setClose = true
		if strings.TrimSpace(*in.ExpectedCloseAt) != "" {
			t, err := time.Parse("2006-01-02", strings.TrimSpace(*in.ExpectedCloseAt))
			if err != nil {
				return nil, apperrors.Validation("invalid expectedCloseAt")
			}
			utc := t.UTC()
			closeDate = &utc
		}
	}
	var fieldJSON []byte
	if in.FieldValues != nil {
		fieldJSON, _ = json.Marshal(in.FieldValues)
	}
	d, err := s.repo.Update(ctx, id, current, in, closeDate, setClose, fieldJSON)
	if err != nil {
		return nil, apperrors.Internal("failed to update deal", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "deal.updated", "deal", audit.Ptr(id), map[string]any{
		"title": d.Title,
	}, ip, ua)
	_ = s.hooks.Emit(ctx, automation.EventDealUpdated, "deal", id, map[string]any{"title": d.Title})
	return d, nil
}

func (s *Service) Move(ctx context.Context, actorID, id string, in MoveInput, ip, ua string) (*Deal, error) {
	deal, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.StageID) == "" {
		return nil, apperrors.Validation("stageId is required")
	}
	toStage, err := s.pipelines.Repo().GetStage(ctx, in.StageID)
	if err != nil {
		return nil, apperrors.Internal("failed to load target stage", err)
	}
	if toStage == nil || !toStage.IsActive {
		return nil, apperrors.Validation("target stage is invalid or inactive")
	}
	pipelineID := in.PipelineID
	if pipelineID == "" {
		pipelineID = toStage.PipelineID
	}
	if pipelineID != toStage.PipelineID {
		return nil, apperrors.Validation("stage does not belong to the specified pipeline")
	}
	if deal.PipelineID != nil && *deal.PipelineID != "" && *deal.PipelineID != pipelineID {
		return nil, apperrors.Validation("cannot move deal across pipelines via board move")
	}
	ok, err := s.pipelines.Repo().StageBelongsToPipeline(ctx, in.StageID, pipelineID)
	if err != nil {
		return nil, apperrors.Internal("failed to validate stage", err)
	}
	if !ok {
		return nil, apperrors.Validation("target stage is invalid or inactive")
	}

	if deal.StageID != nil && *deal.StageID == in.StageID {
		return deal, nil
	}

	if !in.Force {
		missingFields := missingRequiredFields(deal, toStage.RequiredFields)
		missingActs, err := s.repo.MissingRequiredActivities(ctx, deal.ID, toStage.RequiredActivities)
		if err != nil {
			return nil, apperrors.Internal("failed to check required activities", err)
		}
		missingDocs, err := s.repo.MissingRequiredDocuments(ctx, deal.ID, toStage.RequiredDocuments)
		if err != nil {
			return nil, apperrors.Internal("failed to check required documents", err)
		}
		if len(missingFields) > 0 || len(missingActs) > 0 || len(missingDocs) > 0 {
			return nil, apperrors.ValidationDetails("Stage requirements not met", map[string]any{
				"missingFields":     missingFields,
				"missingActivities": missingActs,
				"missingDocuments":  missingDocs,
				"stageId":           toStage.ID,
				"stageName":         toStage.Name,
			})
		}
	}

	if toStage.IsLost && strings.TrimSpace(in.LostReason) == "" {
		return nil, apperrors.Validation("lost reason is required")
	}

	prob := toStage.Probability
	d, err := s.repo.MoveTransactional(ctx, actorID, deal, pipelineID, in.StageID, toStage, &prob, in.LostReason)
	if err != nil {
		return nil, apperrors.Internal("failed to move deal", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "deal.stage_changed", "deal", audit.Ptr(id), map[string]any{
		"fromStageId": deal.StageID,
		"toStageId":   in.StageID,
		"toStageName": toStage.Name,
		"forced":      in.Force,
		"lostReason":  in.LostReason,
		"status":      d.Status,
	}, ip, ua)
	_ = s.hooks.Emit(ctx, automation.EventDealStageChanged, "deal", id, map[string]any{
		"fromStageId": deal.StageID,
		"toStageId":   in.StageID,
		"pipelineId":  pipelineID,
		"status":      d.Status,
	})
	if s.timeline != nil {
		body := toStage.Name
		_, _ = s.timeline.Record(ctx, timeline.WriteInput{
			EventType: timeline.EventStageChange, Title: "Stage change", Body: body,
			ActorUserID: audit.Ptr(actorID), Source: "crm", CustomerID: &deal.CustomerID, DealID: &id,
			Metadata: map[string]any{
				"fromStageId": deal.StageID, "toStageId": in.StageID, "toStageName": toStage.Name, "forced": in.Force,
				"lostReason": in.LostReason, "status": d.Status,
			},
		})
		_, _ = s.timeline.Record(ctx, timeline.WriteInput{
			EventType: timeline.EventAutomation, Title: "Automation event", Body: "deal.stage_changed",
			ActorUserID: audit.Ptr(actorID), Source: "automation", CustomerID: &deal.CustomerID, DealID: &id,
			Metadata: map[string]any{"event": automation.EventDealStageChanged, "toStageId": in.StageID},
		})
	}
	return d, nil
}

func missingRequiredFields(deal *Deal, fields []string) []string {
	missing := []string{}
	for _, f := range fields {
		key := strings.TrimSpace(f)
		if key == "" {
			continue
		}
		ok := false
		switch strings.ToLower(key) {
		case "value":
			ok = deal.Value != nil
		case "expectedcloseat", "expected_close_at":
			ok = deal.ExpectedCloseAt != nil && *deal.ExpectedCloseAt != ""
		case "source":
			ok = strings.TrimSpace(deal.Source) != ""
		case "priority":
			ok = strings.TrimSpace(deal.Priority) != ""
		case "notes":
			ok = strings.TrimSpace(deal.Notes) != ""
		case "owner", "owneruserid", "owner_user_id":
			ok = deal.OwnerUserID != nil && *deal.OwnerUserID != ""
		default:
			if deal.FieldValues != nil {
				if v, exists := deal.FieldValues[key]; exists && v != nil && v != "" {
					ok = true
				}
			}
		}
		if !ok {
			missing = append(missing, key)
		}
	}
	return missing
}

// AddDocument is a helper for tests / future upload flow.
func (s *Service) AddDocument(ctx context.Context, actorID, dealID, name, category string) error {
	d, err := s.Get(ctx, dealID)
	if err != nil {
		return err
	}
	return s.repo.AddDocument(ctx, dealID, d.CustomerID, name, category, actorID)
}
