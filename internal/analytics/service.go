package analytics

import (
	"context"
	"strings"

	"github.com/crm/backend/pkg/apperrors"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Funnel(ctx context.Context, f Filter) (*FunnelResult, error) {
	pipelineID := f.PipelineID
	var name, kind string
	var err error
	if pipelineID == "" {
		pipelineID, name, kind, err = s.repo.DefaultSalesPipeline(ctx)
		if err != nil {
			return nil, apperrors.Validation("pipelineId is required (no default sales pipeline found)")
		}
	} else {
		name, kind, err = s.repo.PipelineMeta(ctx, pipelineID)
		if err != nil {
			return nil, apperrors.NotFound("pipeline not found")
		}
	}
	res, err := s.repo.Funnel(ctx, f, pipelineID, name, kind)
	if err != nil {
		return nil, apperrors.Internal("failed to compute funnel", err)
	}
	return res, nil
}

func (s *Service) Summary(ctx context.Context, f Filter) (*Summary, error) {
	res, err := s.repo.Summary(ctx, f)
	if err != nil {
		return nil, apperrors.Internal("failed to load summary", err)
	}
	return res, nil
}

func (s *Service) Leads(ctx context.Context, f Filter) (*LeadAnalytics, error) {
	res, err := s.repo.LeadAnalytics(ctx, f)
	if err != nil {
		return nil, apperrors.Internal("failed to load lead analytics", err)
	}
	return res, nil
}

func (s *Service) Pipeline(ctx context.Context, f Filter) (*PipelineAnalytics, error) {
	res, err := s.repo.PipelineAnalytics(ctx, f)
	if err != nil {
		return nil, apperrors.Internal("failed to load pipeline analytics", err)
	}
	return res, nil
}

func (s *Service) Activities(ctx context.Context, f Filter) (*ActivityAnalytics, error) {
	res, err := s.repo.ActivityAnalytics(ctx, f)
	if err != nil {
		return nil, apperrors.Internal("failed to load activity analytics", err)
	}
	return res, nil
}

func (s *Service) Conversions(ctx context.Context, f Filter) (*ConversionAnalytics, error) {
	res, err := s.repo.ConversionAnalytics(ctx, f)
	if err != nil {
		return nil, apperrors.Internal("failed to load conversion analytics", err)
	}
	return res, nil
}

func (s *Service) Teams(ctx context.Context, f Filter) (*TeamAnalytics, error) {
	switch strings.ToLower(f.GroupBy) {
	case "team", "user", "period", "pipeline":
	default:
		f.GroupBy = "user"
	}
	res, err := s.repo.TeamAnalytics(ctx, f)
	if err != nil {
		return nil, apperrors.Internal("failed to load team analytics", err)
	}
	return res, nil
}

func (s *Service) Sources(ctx context.Context, f Filter) (*SourceAnalytics, error) {
	res, err := s.repo.SourceAnalytics(ctx, f)
	if err != nil {
		return nil, apperrors.Internal("failed to load source analytics", err)
	}
	return res, nil
}

func (s *Service) Organization(ctx context.Context, f Filter) (*OrganizationAnalytics, error) {
	res, err := s.repo.Organization(ctx, f)
	if err != nil {
		return nil, apperrors.Internal("failed to load organization analytics", err)
	}
	return res, nil
}
