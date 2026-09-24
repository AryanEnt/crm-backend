package targets

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/crm/backend/internal/audit"
	"github.com/crm/backend/pkg/apperrors"
)

type Service struct {
	repo  *Repository
	audit *audit.Service
}

func NewService(repo *Repository, auditSvc *audit.Service) *Service {
	return &Service{repo: repo, audit: auditSvc}
}

func (s *Service) List(ctx context.Context, f ListFilter) ([]Target, int, error) {
	items, total, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, 0, apperrors.Internal("failed to list targets", err)
	}
	return items, total, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Target, error) {
	t, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, apperrors.Internal("failed to load target", err)
	}
	if t == nil {
		return nil, apperrors.NotFound("target not found")
	}
	return t, nil
}

func (s *Service) Create(ctx context.Context, actorID string, in CreateInput, ip, ua string) (*Target, error) {
	if err := validateInput(in.Metric, in.PeriodType, in.ScopeType, in.PeriodStart, in.PeriodEnd, in.TeamID, in.UserID, in.TargetValue); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Name) == "" {
		in.Name = strings.TrimSpace(in.PeriodType + " " + in.Metric + " target")
	}
	switch in.ScopeType {
	case ScopeOrganization:
		in.TeamID = nil
		in.UserID = nil
	case ScopeTeam:
		in.UserID = nil
	}
	t, err := s.repo.Create(ctx, actorID, in)
	if err != nil {
		return nil, apperrors.Internal("failed to create target", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "target.created", "target", audit.Ptr(t.ID), map[string]any{
		"metric": t.Metric, "periodType": t.PeriodType, "value": t.TargetValue,
	}, ip, ua)
	return t, nil
}

func (s *Service) Update(ctx context.Context, actorID, id string, in UpdateInput, ip, ua string) (*Target, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	metric := current.Metric
	if in.Metric != nil {
		metric = *in.Metric
	}
	periodType := current.PeriodType
	if in.PeriodType != nil {
		periodType = *in.PeriodType
	}
	scopeType := current.ScopeType
	if in.ScopeType != nil {
		scopeType = *in.ScopeType
	}
	periodStart := current.PeriodStart
	if in.PeriodStart != nil {
		periodStart = *in.PeriodStart
	}
	periodEnd := current.PeriodEnd
	if in.PeriodEnd != nil {
		periodEnd = *in.PeriodEnd
	}
	teamID := current.TeamID
	if in.TeamID != nil {
		teamID = emptyToNil(in.TeamID)
	}
	userID := current.UserID
	if in.UserID != nil {
		userID = emptyToNil(in.UserID)
	}
	value := current.TargetValue
	if in.TargetValue != nil {
		value = *in.TargetValue
	}
	if err := validateInput(metric, periodType, scopeType, periodStart, periodEnd, teamID, userID, value); err != nil {
		return nil, err
	}
	switch scopeType {
	case ScopeOrganization:
		empty := ""
		in.TeamID = &empty
		in.UserID = &empty
		in.ScopeType = &scopeType
	case ScopeTeam:
		empty := ""
		in.UserID = &empty
		in.ScopeType = &scopeType
	}
	t, err := s.repo.Update(ctx, id, current, in)
	if err != nil {
		return nil, apperrors.Internal("failed to update target", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "target.updated", "target", audit.Ptr(id), map[string]any{
		"metric": t.Metric, "value": t.TargetValue,
	}, ip, ua)
	return t, nil
}

func (s *Service) Delete(ctx context.Context, actorID, id string, ip, ua string) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return apperrors.Internal("failed to delete target", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "target.deleted", "target", audit.Ptr(id), nil, ip, ua)
	return nil
}

func (s *Service) Progress(ctx context.Context, id string) (*Progress, error) {
	t, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.buildProgress(ctx, t)
}

func (s *Service) ListProgress(ctx context.Context, f ListFilter) ([]Progress, int, error) {
	items, total, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, 0, apperrors.Internal("failed to list targets", err)
	}
	out := make([]Progress, 0, len(items))
	for i := range items {
		p, err := s.buildProgress(ctx, &items[i])
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *p)
	}
	return out, total, nil
}

func (s *Service) MetricsCatalog(_ context.Context) []map[string]string {
	return []map[string]string{
		{"code": MetricLeads, "label": "Leads"},
		{"code": MetricQualifiedLeads, "label": "Qualified leads"},
		{"code": MetricSubmissions, "label": "Submissions"},
		{"code": MetricPositiveOutcomes, "label": "Positive outcomes"},
		{"code": MetricConversions, "label": "Conversions"},
		{"code": MetricPipelineValue, "label": "Pipeline value"},
		{"code": MetricActivities, "label": "Activities"},
	}
}

func (s *Service) buildProgress(ctx context.Context, t *Target) (*Progress, error) {
	actual, err := s.repo.ComputeActual(ctx, t)
	if err != nil {
		return nil, apperrors.Internal("failed to compute target actual", err)
	}
	now := time.Now().UTC()
	remaining := math.Max(0, t.TargetValue-actual)
	p := &Progress{
		Target: t, Actual: round2(actual), Remaining: round2(remaining),
		AsOf: now, ProgressLabel: "No target set",
	}
	if t.TargetValue > 0 {
		pct := round2(math.Min(100, (actual/t.TargetValue)*100))
		p.ProgressPct = &pct
		p.ProgressLabel = fmt.Sprintf("%.1f%%", pct)
	} else {
		p.ProgressPct = nil
		p.ProgressLabel = "No target set"
	}

	end, errEnd := time.Parse("2006-01-02", t.PeriodEnd)
	start, errStart := time.Parse("2006-01-02", t.PeriodStart)
	if errEnd == nil {
		endInclusive := end.Add(24*time.Hour - time.Nanosecond)
		days := int(math.Ceil(endInclusive.Sub(now).Hours() / 24))
		if days < 0 {
			days = 0
		}
		p.DaysRemaining = days
		if errStart == nil && endInclusive.After(start) {
			total := endInclusive.Sub(start).Hours()
			elapsed := now.Sub(start).Hours()
			if total > 0 {
				ep := round2(math.Max(0, math.Min(100, (elapsed/total)*100)))
				p.PeriodElapsedPct = &ep
			}
		}
	}
	return p, nil
}

func validateInput(metric, periodType, scopeType, periodStart, periodEnd string, teamID, userID *string, value float64) error {
	ok := false
	for _, m := range ValidMetrics {
		if m == metric {
			ok = true
			break
		}
	}
	if !ok {
		return apperrors.Validation("invalid metric")
	}
	if periodType != PeriodMonthly && periodType != PeriodQuarterly {
		return apperrors.Validation("periodType must be monthly or quarterly")
	}
	if scopeType != ScopeOrganization && scopeType != ScopeTeam && scopeType != ScopeUser {
		return apperrors.Validation("invalid scopeType")
	}
	if _, err := time.Parse("2006-01-02", periodStart); err != nil {
		return apperrors.Validation("invalid periodStart")
	}
	if _, err := time.Parse("2006-01-02", periodEnd); err != nil {
		return apperrors.Validation("invalid periodEnd")
	}
	if periodEnd < periodStart {
		return apperrors.Validation("periodEnd must be on or after periodStart")
	}
	if value < 0 {
		return apperrors.Validation("targetValue cannot be negative")
	}
	switch scopeType {
	case ScopeTeam:
		if teamID == nil || strings.TrimSpace(*teamID) == "" {
			return apperrors.Validation("teamId is required for team targets")
		}
	case ScopeUser:
		if userID == nil || strings.TrimSpace(*userID) == "" {
			return apperrors.Validation("userId is required for individual targets")
		}
	}
	return nil
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
