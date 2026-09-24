// Package predictions implements explainable lead scoring and predictive insights.
//
// Architecture:
//
//	PredictionService → FeatureBuilder → ScoringStrategy → PredictionResult → Explanation
//
// Default strategies are rule-based. ScoringStrategy is an interface so a future ML
// model can replace rules without changing the HTTP contract. Predictions are
// estimates based on CRM data — never guaranteed outcomes.
package predictions
