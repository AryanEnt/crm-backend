package customers

import (
	"context"
	"strings"
	"time"

	"github.com/crm/backend/internal/audit"
	"github.com/crm/backend/internal/automation"
	"github.com/crm/backend/internal/customfields"
	"github.com/crm/backend/internal/leads"
	"github.com/crm/backend/internal/referrals"
	"github.com/crm/backend/internal/systemactivity"
	"github.com/crm/backend/internal/timeline"
	"github.com/crm/backend/pkg/apperrors"
)

type Service struct {
	repo         *Repository
	leadsRepo    *leads.Repository
	audit        *audit.Service
	timeline     *timeline.Service
	hooks        *automation.Emitter
	referrals    *referrals.Service
	customFields *customfields.Service
	sysActivity  *systemactivity.Service
}

func NewService(repo *Repository, leadsRepo *leads.Repository, auditSvc *audit.Service, timelineSvc *timeline.Service, hooks *automation.Emitter, referralSvc *referrals.Service, customFields *customfields.Service, sysActivity *systemactivity.Service) *Service {
	return &Service{repo: repo, leadsRepo: leadsRepo, audit: auditSvc, timeline: timelineSvc, hooks: hooks, referrals: referralSvc, customFields: customFields, sysActivity: sysActivity}
}

func (s *Service) recordSystemActivity(ctx context.Context, in systemactivity.WriteInput) {
	if s.sysActivity != nil {
		s.sysActivity.Record(ctx, in)
	}
}

func (s *Service) List(ctx context.Context, f ListFilter) ([]Customer, int, error) {
	items, total, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, 0, apperrors.Internal("failed to list customers", err)
	}
	return items, total, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Customer, error) {
	c, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, apperrors.Internal("failed to load customer", err)
	}
	if c == nil {
		return nil, apperrors.NotFound("customer not found")
	}
	return c, nil
}

func (s *Service) Get360(ctx context.Context, id string) (*Profile360, error) {
	c, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	deals, err := s.repo.ListDeals(ctx, id)
	if err != nil {
		return nil, apperrors.Internal("failed to load deals", err)
	}
	activities, err := s.repo.ListActivities(ctx, id, 50)
	if err != nil {
		return nil, apperrors.Internal("failed to load activities", err)
	}
	docs, err := s.repo.ListDocuments(ctx, id)
	if err != nil {
		return nil, apperrors.Internal("failed to load documents", err)
	}
	var referral *referrals.Referral
	if s.referrals != nil {
		referral, err = s.referrals.GetForCustomer(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	return &Profile360{Customer: c, Deals: deals, Activities: activities, Documents: docs, Referral: referral}, nil
}

func (s *Service) Create(ctx context.Context, actorID string, in CreateInput, ip, ua string) (*Customer, []leads.DuplicateMatch, error) {
	if strings.TrimSpace(in.FullName) == "" {
		return nil, nil, apperrors.Validation("full name is required")
	}
	if strings.TrimSpace(in.Source) == "" {
		return nil, nil, apperrors.Validation("customer source is required")
	}
	if in.Priority == "" {
		in.Priority = "medium"
	}
	if referrals.IsReferralSource(in.Source) {
		if in.Referral == nil {
			return nil, nil, apperrors.Validation("referral details are required when source is Referral")
		}
		if err := referrals.ValidateRequired(*in.Referral); err != nil {
			return nil, nil, err
		}
	}
	name := strings.TrimSpace(in.FullName)
	dups, err := s.leadsRepo.FindDuplicates(ctx, "", normalizeEmail(in.Email), normalizePhone(in.Phone), &name)
	if err != nil {
		return nil, nil, apperrors.Internal("failed to check duplicates", err)
	}
	if len(dups) > 0 && !in.ForceCreate {
		return nil, dups, nil
	}
	c, err := s.repo.Create(ctx, in)
	if err != nil {
		return nil, nil, apperrors.Internal("failed to create customer", err)
	}
	if referrals.IsReferralSource(in.Source) && in.Referral != nil && s.referrals != nil {
		if _, err := s.referrals.AttachToCustomer(ctx, actorID, c.ID, *in.Referral, ip, ua); err != nil {
			return nil, nil, err
		}
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "customer.created", "customer", audit.Ptr(c.ID), map[string]any{
		"fullName": c.FullName,
	}, ip, ua)
	if s.timeline != nil {
		_, _ = s.timeline.Record(ctx, timeline.WriteInput{
			EventType: timeline.EventCustomerCreated, Title: "Customer created", Body: c.FullName,
			ActorUserID: audit.Ptr(actorID), Source: "crm", CustomerID: &c.ID,
		})
	}
	if s.customFields != nil && len(in.CustomFields) > 0 {
		_ = s.customFields.SetValues(ctx, actorID, "customer", c.ID, customfields.ValueMap(in.CustomFields))
	}
	s.recordSystemActivity(ctx, systemactivity.WriteInput{
		EventType: "customer.created", Title: "Customer created",
		ActorUserID: actorID, EntityType: "customer", EntityID: c.ID, EntityLabel: c.FullName,
	})
	return c, nil, nil
}

func (s *Service) Update(ctx context.Context, actorID, id string, in UpdateInput, ip, ua string) (*Customer, []leads.DuplicateMatch, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	email := current.Email
	if in.Email != nil {
		email = normalizeEmail(in.Email)
	}
	phone := current.Phone
	if in.Phone != nil {
		phone = normalizePhone(in.Phone)
	}
	name := current.FullName
	if in.FullName != nil {
		name = strings.TrimSpace(*in.FullName)
	}
	dups, err := s.leadsRepo.FindDuplicates(ctx, "", email, phone, &name)
	if err != nil {
		return nil, nil, apperrors.Internal("failed to check duplicates", err)
	}
	// filter out self from customer matches
	filtered := make([]leads.DuplicateMatch, 0, len(dups))
	for _, d := range dups {
		if d.Entity == "customer" && d.ID == id {
			continue
		}
		filtered = append(filtered, d)
	}
	if len(filtered) > 0 && !in.ForceUpdate {
		return nil, filtered, nil
	}

	var next *time.Time
	setNext := false
	if in.NextFollowUpAt != nil {
		setNext = true
		if strings.TrimSpace(*in.NextFollowUpAt) != "" {
			t, err := time.Parse(time.RFC3339, strings.TrimSpace(*in.NextFollowUpAt))
			if err != nil {
				t2, err2 := time.Parse("2006-01-02", strings.TrimSpace(*in.NextFollowUpAt))
				if err2 != nil {
					return nil, nil, apperrors.Validation("invalid nextFollowUpAt")
				}
				t = t2
			}
			utc := t.UTC()
			next = &utc
		}
	}

	beforeStage := current.StageID
	beforeOwner := current.OwnerUserID
	c, err := s.repo.Update(ctx, id, current, in, next, setNext)
	if err != nil {
		return nil, nil, apperrors.Internal("failed to update customer", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "customer.updated", "customer", audit.Ptr(id), map[string]any{
		"fullName": c.FullName,
	}, ip, ua)
	if ptrStrChanged(beforeOwner, c.OwnerUserID) {
		_ = s.audit.Record(ctx, audit.Ptr(actorID), "customer.assignment_changed", "customer", audit.Ptr(id), map[string]any{
			"ownerUserId": c.OwnerUserID,
		}, ip, ua)
		if s.timeline != nil {
			_, _ = s.timeline.Record(ctx, timeline.WriteInput{
				EventType: timeline.EventAssignmentChange, Title: "Assignment change", Body: "Customer owner updated",
				ActorUserID: audit.Ptr(actorID), Source: "crm", CustomerID: &id,
				Metadata: map[string]any{"ownerUserId": c.OwnerUserID},
			})
		}
	}
	if ptrStrChanged(beforeStage, c.StageID) {
		_ = s.audit.Record(ctx, audit.Ptr(actorID), "customer.stage_changed", "customer", audit.Ptr(id), map[string]any{
			"stageId": c.StageID,
		}, ip, ua)
		if s.timeline != nil {
			_, _ = s.timeline.Record(ctx, timeline.WriteInput{
				EventType: timeline.EventStageChange, Title: "Stage change", Body: "Customer stage updated",
				ActorUserID: audit.Ptr(actorID), Source: "crm", CustomerID: &id,
				Metadata: map[string]any{"stageId": c.StageID},
			})
		}
	}
	if s.hooks != nil {
		_ = s.hooks.Emit(ctx, automation.TriggerCustomerUpdated, "customer", id, map[string]any{
			"fullName": c.FullName, "ownerUserId": c.OwnerUserID, "teamId": c.TeamID,
		})
	}
	if s.customFields != nil && len(in.CustomFields) > 0 {
		_ = s.customFields.SetValues(ctx, actorID, "customer", id, customfields.ValueMap(in.CustomFields))
	}
	return c, nil, nil
}

func (s *Service) ConvertFromLead(ctx context.Context, actorID, leadID string, ip, ua string) (*Customer, error) {
	lead, err := s.leadsRepo.Get(ctx, leadID)
	if err != nil {
		return nil, apperrors.Internal("failed to load lead", err)
	}
	if lead == nil {
		return nil, apperrors.NotFound("lead not found")
	}
	if lead.ConvertedCustomerID != nil {
		return s.Get(ctx, *lead.ConvertedCustomerID)
	}
	if lead.IsArchived {
		return nil, apperrors.Validation("cannot convert an archived lead")
	}

	c, err := s.repo.CreateFromLead(ctx, lead)
	if err != nil {
		return nil, apperrors.Internal("failed to create customer from lead", err)
	}
	if err := s.leadsRepo.MarkConverted(ctx, leadID, c.ID); err != nil {
		return nil, apperrors.Internal("failed to mark lead converted", err)
	}
	if s.referrals != nil {
		_ = s.referrals.MarkLeadConverted(ctx, actorID, leadID, c.ID, ip, ua)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "lead.converted", "lead", audit.Ptr(leadID), map[string]any{
		"customerId": c.ID,
	}, ip, ua)
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "customer.created", "customer", audit.Ptr(c.ID), map[string]any{
		"fromLeadId": leadID,
	}, ip, ua)
	if s.timeline != nil {
		_, _ = s.timeline.Record(ctx, timeline.WriteInput{
			EventType: timeline.EventCustomerCreated, Title: "Customer created", Body: c.FullName + " (converted from lead)",
			ActorUserID: audit.Ptr(actorID), Source: "crm", CustomerID: &c.ID, LeadID: &leadID,
			Metadata: map[string]any{"fromLeadId": leadID},
		})
	}
	return c, nil
}

func ptrStrChanged(a, b *string) bool {
	if a == nil && b == nil {
		return false
	}
	if a == nil || b == nil {
		return true
	}
	return *a != *b
}
