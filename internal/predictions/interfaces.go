package predictions

import "context"

// FeatureBuilder loads CRM facts into a FeatureSet.
type FeatureBuilder interface {
	BuildLeadFeatures(ctx context.Context, leadID string) (FeatureSet, error)
	BuildDealFeatures(ctx context.Context, dealID string) (FeatureSet, error)
	BuildOwnerWorkloadFeatures(ctx context.Context, ownerUserID string) (FeatureSet, error)
	BuildPipelineRiskFeatures(ctx context.Context, pipelineID string) (FeatureSet, error)
}

// ScoringStrategy computes a PredictionResult from features.
// Future ML models implement this interface without changing the HTTP contract.
type ScoringStrategy interface {
	Code() string
	Version() string
	InsightType() string
	Score(features FeatureSet) (*PredictionResult, error)
}

// Ensure default implementations satisfy the interfaces.
var (
	_ FeatureBuilder  = (*CRMFeatureBuilder)(nil)
	_ ScoringStrategy = LeadScoreStrategy{}
	_ ScoringStrategy = ConversionLikelihoodStrategy{}
	_ ScoringStrategy = StalledOpportunityStrategy{}
	_ ScoringStrategy = FollowUpRiskStrategy{}
	_ ScoringStrategy = ExpectedOutcomeStrategy{}
	_ ScoringStrategy = WorkloadForecastStrategy{}
	_ ScoringStrategy = PipelineRiskStrategy{}
)
