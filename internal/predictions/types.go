package predictions

// Package predictions owns smart lead scoring and predictive insights.
//
// Architecture:
//
//	PredictionService → FeatureBuilder → ScoringStrategy → PredictionResult → Explanation
//
// Rule-based strategies are the default. ScoringStrategy is an interface so a future
// ML model can replace rules without changing the HTTP contract.

const (
	Disclaimer = "Predictions are estimates based on CRM data. They are not guaranteed outcomes."

	InsufficientHistoricalData = "Insufficient historical data."

	StrategyLeadScoreV1           = "lead_score_rules_v1"
	StrategyConversionLikelihoodV1 = "conversion_likelihood_v1"
	StrategyStalledOpportunityV1  = "stalled_opportunity_v1"
	StrategyFollowUpRiskV1        = "follow_up_risk_v1"
	StrategyExpectedOutcomeV1     = "expected_outcome_v1"
	StrategyWorkloadForecastV1    = "workload_forecast_v1"
	StrategyPipelineRiskV1        = "pipeline_risk_v1"

	MinHistoricalSample = 10
)

// Signal is a single explained contribution to a score or insight.
type Signal struct {
	Code    string  `json:"code"`
	Label   string  `json:"label"`
	Impact  float64 `json:"impact"` // positive or negative points
	Polarity string `json:"polarity"` // positive | negative | neutral
	Detail  string  `json:"detail,omitempty"`
}

// Explanation is the human-readable breakdown of a prediction.
type Explanation struct {
	Summary          string   `json:"summary"`
	PositiveSignals  []Signal `json:"positiveSignals"`
	NegativeSignals  []Signal `json:"negativeSignals"`
	NeutralNotes     []string `json:"neutralNotes,omitempty"`
}

// FeatureSet is the structured input bag produced by FeatureBuilder.
type FeatureSet struct {
	EntityType string         `json:"entityType"`
	EntityID   string         `json:"entityId"`
	Features   map[string]any `json:"features"`
	BuiltAt    string         `json:"builtAt"`
}

// PredictionResult is the strategy output persisted for auditability.
type PredictionResult struct {
	Available        bool           `json:"available"`
	Message          string         `json:"message,omitempty"`
	Disclaimer       string         `json:"disclaimer"`
	InsightType      string         `json:"insightType"`
	EntityType       string         `json:"entityType"`
	EntityID         string         `json:"entityId"`
	Score            *float64       `json:"score,omitempty"`
	Label            string         `json:"label"`
	StrategyCode     string         `json:"strategyCode"`
	StrategyVersion  string         `json:"strategyVersion"`
	Features         map[string]any `json:"features"`
	Explanation      Explanation   `json:"explanation"`
	Payload          map[string]any `json:"payload,omitempty"`
	ComputedAt       string         `json:"computedAt"`
	InsufficientHist bool           `json:"insufficientHistoricalData"`
}
