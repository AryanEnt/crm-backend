package activities

import (
	"context"
	"strings"
	"time"

	"github.com/crm/backend/internal/audit"
	"github.com/crm/backend/internal/automation"
	"github.com/crm/backend/internal/systemactivity"
	"github.com/crm/backend/internal/timeline"
	"github.com/crm/backend/pkg/apperrors"
)

func (s *Service) recordSystemActivity(ctx context.Context, in systemactivity.WriteInput) {
	if s.sysActivity != nil {
		s.sysActivity.Record(ctx, in)
	}
}

func (s *Service) ListTypes(ctx context.Context) ([]ActivityType, error) {
	items, err := s.repo.ListTypes(ctx, false)
	if err != nil {
		return nil, apperrors.Internal("failed to list activity types", err)
	}
	return items, nil
}

func (s *Service) Get(ctx context.Context, id string) (*ActivityDetail, error) {
	a, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, apperrors.Internal("failed to load activity", err)
	}
	if a == nil {
		return nil, apperrors.NotFound("activity not found")
	}
	ctxInfo := &CRMContext{
		CustomerID: a.CustomerID, CustomerName: a.CustomerName,
		DealID: a.DealID, DealTitle: a.DealTitle,
		LeadID: a.LeadID, LeadName: a.LeadName,
	}
	switch {
	case a.CustomerID != nil:
		ctxInfo.TimelineHref = "/customers/" + *a.CustomerID
	case a.DealID != nil:
		ctxInfo.TimelineHref = "/deals/" + *a.DealID
	case a.LeadID != nil:
		ctxInfo.TimelineHref = "/leads"
	}
	return &ActivityDetail{Activity: a, Context: ctxInfo}, nil
}

func (s *Service) List(ctx context.Context, f ListFilter) ([]Activity, int, error) {
	items, total, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, 0, apperrors.Internal("failed to list activities", err)
	}
	return items, total, nil
}

func (s *Service) Create(ctx context.Context, actorID string, in CreateInput, ip, ua string) (*Activity, error) {
	code := strings.TrimSpace(in.TypeCode)
	if code == "" {
		code = strings.TrimSpace(in.Kind)
	}
	if code == "" {
		return nil, apperrors.Validation("activity type is required")
	}
	typ, err := s.repo.TypeByCode(ctx, code)
	if err != nil {
		return nil, apperrors.Internal("failed to resolve activity type", err)
	}
	if typ == nil || !typ.IsActive {
		return nil, apperrors.Validation("unknown or inactive activity type")
	}
	if emptyToNil(in.LeadID) == nil && emptyToNil(in.CustomerID) == nil && emptyToNil(in.DealID) == nil {
		return nil, apperrors.Validation("activity must belong to a lead, customer, or deal")
	}
	start, err := parseOptionalTime(in.StartAt)
	if err != nil {
		return nil, apperrors.Validation("invalid startAt")
	}
	end, err := parseOptionalTime(in.EndAt)
	if err != nil {
		return nil, apperrors.Validation("invalid endAt")
	}
	due, err := parseOptionalTime(in.DueAt)
	if err != nil {
		return nil, apperrors.Validation("invalid dueAt")
	}
	if in.Status != "" {
		switch in.Status {
		case "upcoming", "due", "completed", "overdue", "cancelled", "planned":
		default:
			return nil, apperrors.Validation("invalid status")
		}
	}
	a, err := s.repo.Create(ctx, actorID, in, typ, start, end, due)
	if err != nil {
		return nil, apperrors.Internal("failed to create activity", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "activity.created", "activity", audit.Ptr(a.ID), map[string]any{
		"type": a.Kind, "title": a.Title, "status": a.Status,
	}, ip, ua)
	s.recordSystemActivity(ctx, systemactivity.WriteInput{
		EventType: "activity.created", Title: "Activity created",
		ActorUserID: actorID, EntityType: "activity", EntityID: a.ID, EntityLabel: a.Title,
		Metadata: map[string]any{"kind": a.Kind, "status": a.Status},
	})
	s.emitActivityTimeline(ctx, actorID, a, false)
	return a, nil
}

func (s *Service) Update(ctx context.Context, actorID, id string, in UpdateInput, ip, ua string) (*MutationResult, error) {
	current, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, apperrors.Internal("failed to load activity", err)
	}
	if current == nil {
		return nil, apperrors.NotFound("activity not found")
	}
	var typ *ActivityType
	code := ""
	if in.TypeCode != nil {
		code = strings.TrimSpace(*in.TypeCode)
	} else if in.Kind != nil {
		code = strings.TrimSpace(*in.Kind)
	}
	if code != "" {
		typ, err = s.repo.TypeByCode(ctx, code)
		if err != nil {
			return nil, apperrors.Internal("failed to resolve activity type", err)
		}
		if typ == nil {
			return nil, apperrors.Validation("unknown activity type")
		}
	}
	var start, end, due *time.Time
	setStart, setEnd, setDue := false, false, false
	if in.StartAt != nil {
		setStart = true
		start, err = parseOptionalTime(in.StartAt)
		if err != nil {
			return nil, apperrors.Validation("invalid startAt")
		}
	}
	if in.EndAt != nil {
		setEnd = true
		end, err = parseOptionalTime(in.EndAt)
		if err != nil {
			return nil, apperrors.Validation("invalid endAt")
		}
	}
	if in.ClearDueAt {
		setDue = true
		due = nil
	} else if in.DueAt != nil {
		setDue = true
		due, err = parseOptionalTime(in.DueAt)
		if err != nil {
			return nil, apperrors.Validation("invalid dueAt")
		}
	}
	a, err := s.repo.Update(ctx, actorID, current, in, typ, start, end, due, setDue, setStart, setEnd)
	if err != nil {
		return nil, apperrors.Internal("failed to update activity", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "activity.updated", "activity", audit.Ptr(id), map[string]any{
		"status": a.Status, "title": a.Title,
	}, ip, ua)
	if current.Status != "completed" && a.Status == "completed" {
		s.emitActivityTimeline(ctx, actorID, a, true)
		if s.hooks != nil {
			_ = s.hooks.Emit(ctx, automation.TriggerActivityCompleted, "activity", a.ID, map[string]any{
				"title": a.Title, "kind": a.Kind, "leadId": a.LeadID, "dealId": a.DealID, "customerId": a.CustomerID,
			})
		}
	}
	var nextID *string
	completing := current.Status != "completed" && a.Status == "completed"
	if completing && in.NextActivity != nil && strings.TrimSpace(in.NextActivity.DueAt) != "" {
		id, err := s.ScheduleFollowUp(ctx, actorID, FollowUpRequest{
			Next:       *in.NextActivity,
			LeadID:     a.LeadID,
			CustomerID: a.CustomerID,
			DealID:     a.DealID,
		}, ip, ua)
		if err == nil {
			nextID = &id
		}
	}
	needs := false
	if completing {
		needs, _ = s.repo.LinkedNeedsNext(ctx, a.LeadID, a.CustomerID, a.DealID)
	}
	return &MutationResult{Activity: a, NeedsNextActivity: needs, NextActivityID: nextID}, nil
}

func (s *Service) FollowUp(ctx context.Context, leadID, customerID, dealID string) (*FollowUpIntel, error) {
	if leadID == "" && customerID == "" && dealID == "" {
		return nil, apperrors.Validation("leadId, customerId, or dealId is required")
	}
	intel, err := s.repo.FollowUpIntel(ctx, leadID, customerID, dealID)
	if err != nil {
		return nil, apperrors.Internal("failed to load follow-up intelligence", err)
	}
	return intel, nil
}

func (s *Service) GetTimezone(ctx context.Context, userID string) (string, error) {
	tz, err := s.repo.UserTimezone(ctx, userID)
	if err != nil {
		return "UTC", nil
	}
	return tz, nil
}

func (s *Service) SetTimezone(ctx context.Context, userID, tz string) error {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return apperrors.Validation("timezone is required")
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return apperrors.Validation("invalid IANA timezone")
	}
	if err := s.repo.SetUserTimezone(ctx, userID, tz); err != nil {
		return apperrors.Internal("failed to update timezone", err)
	}
	return nil
}

func (s *Service) emitActivityTimeline(ctx context.Context, actorID string, a *Activity, completed bool) {
	if s.timeline == nil || a == nil {
		return
	}
	eventType := timeline.EventActivityCreated
	title := "Activity created"
	if completed {
		eventType = timeline.EventActivityCompleted
		title = "Activity completed"
	} else {
		switch a.Kind {
		case "call":
			eventType = timeline.EventCall
			title = "Call"
		case "whatsapp":
			eventType = timeline.EventWhatsApp
			title = "WhatsApp message"
		case "email":
			eventType = timeline.EventEmail
			title = "Email"
		case "meeting":
			eventType = timeline.EventMeeting
			title = "Meeting"
		case "note":
			eventType = timeline.EventNote
			title = "Note"
		case "stage_change":
			eventType = timeline.EventStageChange
			title = "Stage change"
		case "assignment":
			eventType = timeline.EventAssignmentChange
			title = "Assignment change"
		}
	}
	_, _ = s.timeline.Record(ctx, timeline.WriteInput{
		EventType: eventType, Title: title, Body: a.Title,
		ActorUserID: audit.Ptr(actorID), Source: "crm",
		LeadID: a.LeadID, CustomerID: a.CustomerID, DealID: a.DealID, ActivityID: &a.ID,
		Metadata: map[string]any{"kind": a.Kind, "status": a.Status},
	})
	if !completed && a.Status == "completed" && eventType != timeline.EventActivityCompleted {
		_, _ = s.timeline.Record(ctx, timeline.WriteInput{
			EventType: timeline.EventActivityCompleted, Title: "Activity completed", Body: a.Title,
			ActorUserID: audit.Ptr(actorID), Source: "crm",
			LeadID: a.LeadID, CustomerID: a.CustomerID, DealID: a.DealID, ActivityID: &a.ID,
		})
	}
}

func (s *Service) ScheduleFollowUp(ctx context.Context, actorID string, in FollowUpRequest, ip, ua string) (string, error) {
	title := strings.TrimSpace(in.Next.Title)
	if title == "" {
		title = "Follow up"
	}
	code := strings.TrimSpace(in.Next.TypeCode)
	if code == "" {
		code = "follow_up"
	}
	due := strings.TrimSpace(in.Next.DueAt)
	if due == "" {
		return "", apperrors.Validation("dueAt is required")
	}
	a, err := s.Create(ctx, actorID, CreateInput{
		Title:      title,
		TypeCode:   code,
		Status:     "upcoming",
		DueAt:      &due,
		LeadID:     in.LeadID,
		CustomerID: in.CustomerID,
		DealID:     in.DealID,
	}, ip, ua)
	if err != nil {
		return "", err
	}
	return a.ID, nil
}

func parseOptionalTime(v *string) (*time.Time, error) {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil, nil
	}
	raw := strings.TrimSpace(*v)
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04", raw)
		if err != nil {
			t, err = time.Parse("2006-01-02", raw)
			if err != nil {
				return nil, err
			}
		}
	}
	utc := t.UTC()
	return &utc, nil
}
