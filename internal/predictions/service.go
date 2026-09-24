package predictions

import (
	"context"
	"strings"

	"github.com/crm/backend/pkg/apperrors"
	"github.com/jackc/pgx/v5"
)

// PredictionService orchestrates FeatureBuilder → ScoringStrategy → persist.
type PredictionService struct {
	features FeatureBuilder
	repo     *Repository
}

func NewService(features FeatureBuilder, repo *Repository) *PredictionService {
	return &PredictionService{features: features, repo: repo}
}

func (s *PredictionService) ScoreLead(ctx context.Context, actorID, leadID string) (*PredictionResult, error) {
	ok, err := s.repo.LeadExists(ctx, leadID)
	if err != nil {
		return nil, apperrors.Internal("failed to check lead", err)
	}
	if !ok {
		return nil, apperrors.NotFound("lead not found")
	}
	fs, err := s.features.BuildLeadFeatures(ctx, leadID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperrors.NotFound("lead not found")
		}
		return nil, apperrors.Internal("failed to build lead features", err)
	}
	res, err := LeadScoreStrategy{}.Score(fs)
	if err != nil {
		return nil, apperrors.Internal("lead scoring failed", err)
	}
	if _, err := s.repo.SaveLeadScore(ctx, actorID, res); err != nil {
		return nil, apperrors.Internal("failed to store lead score", err)
	}
	if _, err := s.repo.SavePrediction(ctx, actorID, res); err != nil {
		return nil, apperrors.Internal("failed to store prediction audit", err)
	}
	return res, nil
}

func (s *PredictionService) LeadScoreHistory(ctx context.Context, leadID string, limit int) ([]map[string]any, error) {
	ok, err := s.repo.LeadExists(ctx, leadID)
	if err != nil {
		return nil, apperrors.Internal("failed to check lead", err)
	}
	if !ok {
		return nil, apperrors.NotFound("lead not found")
	}
	return s.repo.ListLeadScoreHistory(ctx, leadID, limit)
}

func (s *PredictionService) LeadInsights(ctx context.Context, actorID, leadID string) (map[string]any, error) {
	ok, err := s.repo.LeadExists(ctx, leadID)
	if err != nil {
		return nil, apperrors.Internal("failed to check lead", err)
	}
	if !ok {
		return nil, apperrors.NotFound("lead not found")
	}
	fs, err := s.features.BuildLeadFeatures(ctx, leadID)
	if err != nil {
		return nil, apperrors.Internal("failed to build lead features", err)
	}
	score, err := LeadScoreStrategy{}.Score(fs)
	if err != nil {
		return nil, err
	}
	_ = actorID // score refresh is separate; insights use live features
	conv, err := ConversionLikelihoodStrategy{}.Score(fs)
	if err != nil {
		return nil, err
	}
	follow, err := FollowUpRiskStrategy{}.Score(fs)
	if err != nil {
		return nil, err
	}
	for _, r := range []*PredictionResult{score, conv, follow} {
		if _, err := s.repo.SavePrediction(ctx, actorID, r); err != nil {
			return nil, apperrors.Internal("failed to store prediction audit", err)
		}
	}
	if _, err := s.repo.SaveLeadScore(ctx, actorID, score); err != nil {
		return nil, apperrors.Internal("failed to store lead score", err)
	}
	return map[string]any{
		"disclaimer": Disclaimer,
		"leadScore":  score,
		"insights": []any{conv, follow},
	}, nil
}

func (s *PredictionService) DealInsights(ctx context.Context, actorID, dealID string) (map[string]any, error) {
	ok, err := s.repo.DealExists(ctx, dealID)
	if err != nil {
		return nil, apperrors.Internal("failed to check deal", err)
	}
	if !ok {
		return nil, apperrors.NotFound("deal not found")
	}
	fs, err := s.features.BuildDealFeatures(ctx, dealID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperrors.NotFound("deal not found")
		}
		return nil, apperrors.Internal("failed to build deal features", err)
	}
	stalled, err := StalledOpportunityStrategy{}.Score(fs)
	if err != nil {
		return nil, err
	}
	follow, err := FollowUpRiskStrategy{}.Score(fs)
	if err != nil {
		return nil, err
	}
	outcome, err := ExpectedOutcomeStrategy{}.Score(fs)
	if err != nil {
		return nil, err
	}
	conv, err := ConversionLikelihoodStrategy{}.Score(fs)
	if err != nil {
		return nil, err
	}
	for _, r := range []*PredictionResult{stalled, follow, outcome, conv} {
		if _, err := s.repo.SavePrediction(ctx, actorID, r); err != nil {
			return nil, apperrors.Internal("failed to store prediction audit", err)
		}
	}
	return map[string]any{
		"disclaimer": Disclaimer,
		"insights":   []any{stalled, follow, outcome, conv},
	}, nil
}

func (s *PredictionService) Workload(ctx context.Context, actorID, ownerUserID string) (*PredictionResult, error) {
	if strings.TrimSpace(ownerUserID) == "" {
		return nil, apperrors.Validation("ownerUserId is required")
	}
	fs, err := s.features.BuildOwnerWorkloadFeatures(ctx, ownerUserID)
	if err != nil {
		return nil, apperrors.Internal("failed to build workload features", err)
	}
	res, err := WorkloadForecastStrategy{}.Score(fs)
	if err != nil {
		return nil, err
	}
	if _, err := s.repo.SavePrediction(ctx, actorID, res); err != nil {
		return nil, apperrors.Internal("failed to store prediction audit", err)
	}
	return res, nil
}

func (s *PredictionService) PipelineRisk(ctx context.Context, actorID, pipelineID string) (*PredictionResult, error) {
	if strings.TrimSpace(pipelineID) == "" {
		return nil, apperrors.Validation("pipelineId is required")
	}
	fs, err := s.features.BuildPipelineRiskFeatures(ctx, pipelineID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperrors.NotFound("pipeline not found")
		}
		return nil, apperrors.Internal("failed to build pipeline features", err)
	}
	res, err := PipelineRiskStrategy{}.Score(fs)
	if err != nil {
		return nil, err
	}
	if _, err := s.repo.SavePrediction(ctx, actorID, res); err != nil {
		return nil, apperrors.Internal("failed to store prediction audit", err)
	}
	return res, nil
}

func (s *PredictionService) Catalog() map[string]any {
	strategies := []ScoringStrategy{
		LeadScoreStrategy{},
		ConversionLikelihoodStrategy{},
		StalledOpportunityStrategy{},
		FollowUpRiskStrategy{},
		ExpectedOutcomeStrategy{},
		WorkloadForecastStrategy{},
		PipelineRiskStrategy{},
	}
	list := make([]map[string]any, 0, len(strategies))
	for _, st := range strategies {
		list = append(list, map[string]any{
			"code": st.Code(), "version": st.Version(), "insightType": st.InsightType(),
		})
	}
	return map[string]any{
		"disclaimer":                 Disclaimer,
		"insufficientHistoricalData": InsufficientHistoricalData,
		"minHistoricalSample":        MinHistoricalSample,
		"architecture":               "PredictionService → FeatureBuilder → ScoringStrategy → PredictionResult → Explanation",
		"strategies":                 list,
	}
}
