package leads

import (
	"context"
	"strings"
	"time"

	"github.com/crm/backend/internal/attention"
	"github.com/crm/backend/internal/audit"
	"github.com/crm/backend/internal/auth"
	"github.com/crm/backend/internal/automation"
	"github.com/crm/backend/internal/customfields"
	"github.com/crm/backend/internal/permissions"
	"github.com/crm/backend/internal/referrals"
	"github.com/crm/backend/internal/systemactivity"
	"github.com/crm/backend/internal/timeline"
	"github.com/crm/backend/pkg/apperrors"
)

type Service struct {
	repo         *Repository
	audit        *audit.Service
	timeline     *timeline.Service
	hooks        *automation.Emitter
	referrals    *referrals.Service
	customFields *customfields.Service
	sysActivity  *systemactivity.Service
}

func NewService(repo *Repository, auditSvc *audit.Service, timelineSvc *timeline.Service, hooks *automation.Emitter, referralSvc *referrals.Service, customFields *customfields.Service, sysActivity *systemactivity.Service) *Service {
	return &Service{repo: repo, audit: auditSvc, timeline: timelineSvc, hooks: hooks, referrals: referralSvc, customFields: customFields, sysActivity: sysActivity}
}

func (s *Service) recordSystemActivity(ctx context.Context, in systemactivity.WriteInput) {
	if s.sysActivity != nil {
		s.sysActivity.Record(ctx, in)
	}
}

func (s *Service) List(ctx context.Context, f ListFilter) ([]Lead, int, error) {
	if !validLeadStatus(f.Status) {
		return nil, 0, apperrors.Validation("invalid status")
	}
	if !attention.ValidCode(f.Attention) {
		return nil, 0, apperrors.Validation("invalid attention")
	}
	items, total, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, 0, apperrors.Internal("failed to list leads", err)
	}
	return items, total, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Lead, error) {
	l, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, apperrors.Internal("failed to load lead", err)
	}
	if l == nil {
		return nil, apperrors.NotFound("lead not found")
	}
	return l, nil
}

func (s *Service) Qualify(ctx context.Context, actorID, id, ip, ua string) (*Lead, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	switch current.Status {
	case "qualified":
		return current, nil
	case "inbox", "open":
	default:
		return nil, apperrors.Validation("only inbox or open leads can be qualified")
	}
	lead, err := s.repo.SetStatus(ctx, id, "qualified")
	if err != nil {
		return nil, apperrors.Internal("failed to qualify lead", err)
	}
	if lead == nil {
		return nil, apperrors.NotFound("lead not found")
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "lead.qualified", "lead", audit.Ptr(id), map[string]any{
		"from": current.Status, "to": "qualified",
	}, ip, ua)
	return lead, nil
}

func validLeadStatus(status string) bool {
	switch status {
	case "", "all", "working", "inbox", "open", "qualified", "converted", "unqualified", "archived":
		return true
	default:
		return false
	}
}

func (s *Service) Create(ctx context.Context, actorID string, in CreateInput, ip, ua string) (*CreateResult, error) {
	if strings.TrimSpace(in.FullName) == "" {
		return nil, apperrors.Validation("full name is required")
	}
	if strings.TrimSpace(in.Source) == "" {
		return nil, apperrors.Validation("lead source is required")
	}
	if in.Priority == "" {
		in.Priority = "medium"
	}
	if !validPriority(in.Priority) {
		return nil, apperrors.Validation("invalid priority")
	}
	if referrals.IsReferralSource(in.Source) {
		if in.Referral == nil {
			return nil, apperrors.Validation("referral details are required when lead source is Referral")
		}
		if err := referrals.ValidateRequired(*in.Referral); err != nil {
			return nil, err
		}
	}

	// Server-side ownership: SEs cannot spoof owner/team from the client.
	if claims, ok := auth.ClaimsFromContext(ctx); ok {
		if claims.RoleCode == permissions.RoleSalesExecutive || in.OwnerUserID == nil || *in.OwnerUserID == "" {
			uid := claims.UserID
			in.OwnerUserID = &uid
		}
		if len(claims.TeamIDs) > 0 {
			if in.TeamID == nil || *in.TeamID == "" || claims.RoleCode == permissions.RoleSalesExecutive {
				tid := claims.TeamIDs[0]
				in.TeamID = &tid
			}
		}
	}

	email := normalizeEmailPtr(in.Email)
	phone := normalizePhonePtr(in.Phone)
	name := strings.TrimSpace(in.FullName)

	dups, err := s.repo.FindDuplicates(ctx, "", email, phone, &name)
	if err != nil {
		return nil, apperrors.Internal("failed to check duplicates", err)
	}
	if len(dups) > 0 && !in.ForceCreate {
		return &CreateResult{Duplicates: dups, NeedsReview: true}, nil
	}

	nextAt, err := parseOptionalTime(in.NextActivityAt)
	if err != nil {
		return nil, apperrors.Validation("invalid nextActivityAt")
	}

	lead, err := s.repo.Create(ctx, in, nextAt)
	if err != nil {
		return nil, apperrors.Internal("failed to create lead", err)
	}
	if referrals.IsReferralSource(in.Source) && in.Referral != nil && s.referrals != nil {
		if _, err := s.referrals.AttachToLead(ctx, actorID, lead.ID, *in.Referral, ip, ua); err != nil {
			return nil, err
		}
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "lead.created", "lead", audit.Ptr(lead.ID), map[string]any{
		"fullName": lead.FullName, "source": lead.Source,
	}, ip, ua)
	if s.timeline != nil {
		_, _ = s.timeline.Record(ctx, timeline.WriteInput{
			EventType: timeline.EventLeadCreated, Title: "Lead created", Body: lead.FullName,
			ActorUserID: audit.Ptr(actorID), Source: "crm", LeadID: &lead.ID,
			Metadata: map[string]any{"source": lead.Source},
		})
	}
	if s.hooks != nil {
		_ = s.hooks.Emit(ctx, automation.TriggerLeadCreated, "lead", lead.ID, map[string]any{
			"fullName": lead.FullName, "source": lead.Source, "priority": lead.Priority,
			"pipelineId": lead.PipelineID, "stageId": lead.StageID, "ownerUserId": lead.OwnerUserID,
		})
	}
	if s.customFields != nil && len(in.CustomFields) > 0 {
		_ = s.customFields.SetValues(ctx, actorID, "lead", lead.ID, customfields.ValueMap(in.CustomFields))
	}
	s.recordSystemActivity(ctx, systemactivity.WriteInput{
		EventType: "lead.created", Title: "Lead created",
		ActorUserID: actorID, EntityType: "lead", EntityID: lead.ID, EntityLabel: lead.FullName,
		Metadata: map[string]any{"source": lead.Source},
	})
	return &CreateResult{Lead: lead}, nil
}

func (s *Service) Update(ctx context.Context, actorID, id string, in UpdateInput, ip, ua string) (*CreateResult, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	email := current.Email
	if in.Email != nil {
		email = normalizeEmailPtr(in.Email)
	}
	phone := current.Phone
	if in.Phone != nil {
		phone = normalizePhonePtr(in.Phone)
	}
	name := current.FullName
	if in.FullName != nil {
		name = strings.TrimSpace(*in.FullName)
	}
	if in.Priority != nil && !validPriority(*in.Priority) {
		return nil, apperrors.Validation("invalid priority")
	}

	dups, err := s.repo.FindDuplicates(ctx, id, email, phone, &name)
	if err != nil {
		return nil, apperrors.Internal("failed to check duplicates", err)
	}
	if len(dups) > 0 && !in.ForceUpdate {
		return &CreateResult{Duplicates: dups, NeedsReview: true}, nil
	}

	var nextPtr **time.Time
	if in.NextActivityAt != nil {
		t, err := parseOptionalTime(in.NextActivityAt)
		if err != nil {
			return nil, apperrors.Validation("invalid nextActivityAt")
		}
		nextPtr = &t
	}

	beforeOwner := current.OwnerUserID
	beforeStage := current.StageID

	lead, err := s.repo.Update(ctx, id, current, in, nextPtr)
	if err != nil {
		return nil, apperrors.Internal("failed to update lead", err)
	}

	meta := map[string]any{"fullName": lead.FullName}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "lead.updated", "lead", audit.Ptr(id), meta, ip, ua)

	if ptrChanged(beforeOwner, lead.OwnerUserID) {
		_ = s.audit.Record(ctx, audit.Ptr(actorID), "lead.assignment_changed", "lead", audit.Ptr(id), map[string]any{
			"ownerUserId": lead.OwnerUserID,
		}, ip, ua)
		if s.timeline != nil {
			_, _ = s.timeline.Record(ctx, timeline.WriteInput{
				EventType: timeline.EventAssignmentChange, Title: "Assignment change", Body: "Lead owner updated",
				ActorUserID: audit.Ptr(actorID), Source: "crm", LeadID: &id,
				Metadata: map[string]any{"ownerUserId": lead.OwnerUserID},
			})
		}
		if s.hooks != nil {
			_ = s.hooks.Emit(ctx, automation.TriggerLeadAssigned, "lead", id, map[string]any{
				"ownerUserId": lead.OwnerUserID, "teamId": lead.TeamID,
			})
		}
	}
	if ptrChanged(beforeStage, lead.StageID) {
		_ = s.audit.Record(ctx, audit.Ptr(actorID), "lead.stage_changed", "lead", audit.Ptr(id), map[string]any{
			"stageId": lead.StageID, "pipelineId": lead.PipelineID,
		}, ip, ua)
		if s.timeline != nil {
			_, _ = s.timeline.Record(ctx, timeline.WriteInput{
				EventType: timeline.EventStageChange, Title: "Stage change", Body: "Lead stage updated",
				ActorUserID: audit.Ptr(actorID), Source: "crm", LeadID: &id,
				Metadata: map[string]any{"stageId": lead.StageID, "pipelineId": lead.PipelineID},
			})
		}
		if s.hooks != nil {
			_ = s.hooks.Emit(ctx, automation.TriggerLeadStageChanged, "lead", id, map[string]any{
				"stageId": lead.StageID, "pipelineId": lead.PipelineID,
			})
		}
	}

	if s.customFields != nil && len(in.CustomFields) > 0 {
		_ = s.customFields.SetValues(ctx, actorID, "lead", id, customfields.ValueMap(in.CustomFields))
	}

	return &CreateResult{Lead: lead}, nil
}

func (s *Service) Archive(ctx context.Context, actorID string, ids []string, archive bool, ip, ua string) (int, error) {
	if len(ids) == 0 {
		return 0, apperrors.Validation("ids are required")
	}
	n, err := s.repo.SetArchived(ctx, ids, archive)
	if err != nil {
		return 0, apperrors.Internal("failed to archive leads", err)
	}
	action := "lead.restored"
	if archive {
		action = "lead.archived"
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), action, "lead", nil, map[string]any{"ids": ids, "count": n}, ip, ua)
	return n, nil
}

func (s *Service) BulkAssign(ctx context.Context, actorID string, in BulkAssignInput, ip, ua string) (int, error) {
	if len(in.IDs) == 0 {
		return 0, apperrors.Validation("ids are required")
	}
	n, err := s.repo.BulkAssign(ctx, in.IDs, in.OwnerUserID, in.TeamID)
	if err != nil {
		return 0, apperrors.Internal("failed to assign leads", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "lead.assignment_changed", "lead", nil, map[string]any{
		"ids": in.IDs, "ownerUserId": in.OwnerUserID, "teamId": in.TeamID,
	}, ip, ua)
	if s.hooks != nil {
		for _, id := range in.IDs {
			_ = s.hooks.Emit(ctx, automation.TriggerLeadAssigned, "lead", id, map[string]any{
				"ownerUserId": in.OwnerUserID, "teamId": in.TeamID, "bulk": true,
			})
		}
	}
	return n, nil
}

func (s *Service) BulkStage(ctx context.Context, actorID string, in BulkStageInput, ip, ua string) (int, error) {
	if len(in.IDs) == 0 || in.PipelineID == "" || in.StageID == "" {
		return 0, apperrors.Validation("ids, pipelineId and stageId are required")
	}
	n, err := s.repo.BulkStage(ctx, in.IDs, in.PipelineID, in.StageID)
	if err != nil {
		return 0, apperrors.Internal("failed to change stage", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "lead.stage_changed", "lead", nil, map[string]any{
		"ids": in.IDs, "pipelineId": in.PipelineID, "stageId": in.StageID,
	}, ip, ua)
	if s.hooks != nil {
		for _, id := range in.IDs {
			_ = s.hooks.Emit(ctx, automation.TriggerLeadStageChanged, "lead", id, map[string]any{
				"pipelineId": in.PipelineID, "stageId": in.StageID, "bulk": true,
			})
		}
	}
	return n, nil
}

func (s *Service) CheckDuplicates(ctx context.Context, excludeID string, email, phone, name *string) ([]DuplicateMatch, error) {
	items, err := s.repo.FindDuplicates(ctx, excludeID, normalizeEmailPtr(email), normalizePhonePtr(phone), name)
	if err != nil {
		return nil, apperrors.Internal("failed to check duplicates", err)
	}
	return items, nil
}

func (s *Service) Repo() *Repository { return s.repo }

func validPriority(p string) bool {
	switch p {
	case "low", "medium", "high", "urgent":
		return true
	default:
		return false
	}
}

func parseOptionalTime(v *string) (*time.Time, error) {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(*v))
	if err != nil {
		t2, err2 := time.Parse("2006-01-02", strings.TrimSpace(*v))
		if err2 != nil {
			return nil, err
		}
		t = t2
	}
	utc := t.UTC()
	return &utc, nil
}

func ptrChanged(a, b *string) bool {
	if a == nil && b == nil {
		return false
	}
	if a == nil || b == nil {
		return true
	}
	return *a != *b
}
