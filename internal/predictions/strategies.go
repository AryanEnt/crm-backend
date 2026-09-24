package predictions

import (
	"fmt"
	"strings"
	"time"
)

type LeadScoreStrategy struct{}

func (s LeadScoreStrategy) Code() string        { return StrategyLeadScoreV1 }
func (s LeadScoreStrategy) Version() string     { return "1" }
func (s LeadScoreStrategy) InsightType() string { return "lead_score" }

func (s LeadScoreStrategy) Score(fs FeatureSet) (*PredictionResult, error) {
	f := fs.Features
	score := 45.0
	pos := []Signal{}
	neg := []Signal{}

	add := func(list *[]Signal, code, label string, impact float64, detail string) {
		pol := "positive"
		if impact < 0 {
			pol = "negative"
		} else if impact == 0 {
			pol = "neutral"
		}
		*list = append(*list, Signal{Code: code, Label: label, Impact: impact, Polarity: pol, Detail: detail})
		score += impact
	}

	stage := strings.ToLower(asString(f["stageName"]))
	status := strings.ToLower(asString(f["status"]))
	switch {
	case status == "qualified" || strings.Contains(stage, "qualif"):
		add(&pos, "stage_qualified", "Advanced / qualified stage", 15, stage)
	case strings.Contains(stage, "contact"):
		add(&pos, "stage_contacted", "Lead has been contacted", 8, stage)
	case strings.Contains(stage, "unqualif") || status == "unqualified":
		add(&neg, "stage_unqualified", "Unqualified stage", -20, stage)
	case stage != "":
		add(&pos, "stage_known", "Stage assigned", 3, stage)
	}

	source := asString(f["source"])
	if source != "" {
		add(&pos, "source_known", "Source captured", 4, source)
	}
	sample := asInt(f["sourceClosedSample"])
	insuffHist := sample < MinHistoricalSample
	if !insuffHist {
		rate := asFloat(f["sourceConversionRate"])
		if rate >= 40 {
			add(&pos, "hist_source_conversion", "Historical conversion behavior for source", 10,
				fmt.Sprintf("%.0f%% of %d similar leads converted", rate, sample))
		} else if rate > 0 && rate < 15 {
			add(&neg, "hist_source_low", "Low historical conversion for source", -8,
				fmt.Sprintf("%.0f%% of %d similar leads converted", rate, sample))
		}
	}

	daysInactive := asFloat(f["daysSinceActivity"])
	act7 := asInt(f["activityCount7d"])
	if act7 > 0 || daysInactive <= 3 {
		add(&pos, "recently_contacted", "Recently contacted", 12,
			fmt.Sprintf("%.0f days since last activity", daysInactive))
	} else if daysInactive <= 7 {
		add(&pos, "recent_activity", "Activity within a week", 6,
			fmt.Sprintf("%.0f days since last activity", daysInactive))
	} else if daysInactive > 14 {
		add(&neg, "inactivity", "Inactivity", -15,
			fmt.Sprintf("No activity for %.0f days", daysInactive))
	} else if daysInactive > 7 {
		add(&neg, "cooling", "Cooling engagement", -8,
			fmt.Sprintf("%.0f days since last activity", daysInactive))
	}

	completed := asInt(f["completedActivities"])
	if completed >= 3 {
		add(&pos, "high_engagement", "High-engagement activity", 10,
			fmt.Sprintf("%d completed activities", completed))
	} else if completed >= 1 {
		add(&pos, "response_behavior", "Response behavior recorded", 6,
			fmt.Sprintf("%d completed activities", completed))
	}

	overdue := asInt(f["overdueFollowups"])
	if overdue > 0 {
		add(&neg, "follow_up_overdue", "Follow-up overdue", -12,
			fmt.Sprintf("%d overdue follow-ups", overdue))
	}

	completeness := asFloat(f["profileCompletenessPct"])
	if completeness >= 70 {
		add(&pos, "profile_complete", "Strong profile completeness", 12,
			fmt.Sprintf("%.0f%% complete", completeness))
	} else if completeness >= 40 {
		add(&pos, "profile_partial", "Partial profile completeness", 5,
			fmt.Sprintf("%.0f%% complete", completeness))
	} else {
		add(&neg, "profile_thin", "Thin profile", -6,
			fmt.Sprintf("%.0f%% complete", completeness))
	}

	if asBool(f["hasAnzsco"]) {
		add(&pos, "anzsco", "ANZSCO occupation linked", 5, asString(f["anzscoCode"]))
	}

	docsVerified := asInt(f["docsVerified"])
	docsUploaded := asInt(f["docsUploaded"])
	docsRequested := asInt(f["docsRequested"])
	if docsVerified > 0 {
		add(&pos, "documents_verified", "Documents verified", 12,
			fmt.Sprintf("%d verified", docsVerified))
	} else if docsUploaded > 0 {
		add(&pos, "documents_received", "Documents received", 8,
			fmt.Sprintf("%d uploaded", docsUploaded))
	} else if docsRequested > 0 {
		add(&neg, "documents_pending", "Documents still outstanding", -5,
			fmt.Sprintf("%d requested/missing", docsRequested))
	}

	age := asInt(f["ageDays"])
	if age > 60 {
		add(&neg, "deal_age", "Lead age elevated", -10, fmt.Sprintf("%d days old", age))
	} else if age > 30 {
		add(&neg, "deal_age_moderate", "Lead aging", -5, fmt.Sprintf("%d days old", age))
	}

	score = clamp(round2(score), 0, 100)
	label := "Warm"
	switch {
	case score >= 75:
		label = "Hot"
	case score >= 50:
		label = "Warm"
	case score >= 30:
		label = "Cool"
	default:
		label = "Cold"
	}

	notes := []string{}
	if insuffHist {
		notes = append(notes, InsufficientHistoricalData+" Historical source conversion was not applied as a scored signal.")
	}

	return &PredictionResult{
		Available: true, Disclaimer: Disclaimer,
		InsightType: s.InsightType(), EntityType: fs.EntityType, EntityID: fs.EntityID,
		Score: &score, Label: label,
		StrategyCode: s.Code(), StrategyVersion: s.Version(),
		Features: f,
		Explanation: Explanation{
			Summary:         fmt.Sprintf("Score: %.0f — %s lead based on configurable CRM signals.", score, strings.ToLower(label)),
			PositiveSignals: pos, NegativeSignals: neg, NeutralNotes: notes,
		},
		Payload: map[string]any{"band": label},
		ComputedAt: time.Now().UTC().Format(time.RFC3339),
		InsufficientHist: insuffHist,
	}, nil
}

type ConversionLikelihoodStrategy struct{}

func (s ConversionLikelihoodStrategy) Code() string        { return StrategyConversionLikelihoodV1 }
func (s ConversionLikelihoodStrategy) Version() string     { return "1" }
func (s ConversionLikelihoodStrategy) InsightType() string { return "conversion_likelihood" }

func (s ConversionLikelihoodStrategy) Score(fs FeatureSet) (*PredictionResult, error) {
	f := fs.Features
	sample := asInt(f["sourceClosedSample"])
	if sample == 0 {
		// deal features use pipelineClosedSample
		sample = asInt(f["pipelineClosedSample"])
	}
	res := baseResult(s, fs)
	if sample < MinHistoricalSample {
		res.Available = false
		res.Message = InsufficientHistoricalData
		res.InsufficientHist = true
		res.Explanation.Summary = InsufficientHistoricalData
		res.Explanation.NeutralNotes = []string{
			fmt.Sprintf("Need at least %d closed historical records; found %d.", MinHistoricalSample, sample),
		}
		return res, nil
	}

	baseRate := asFloat(f["sourceConversionRate"])
	if baseRate == 0 {
		baseRate = asFloat(f["pipelineWinRate"])
	}
	likelihood := baseRate
	var pos, neg []Signal
	if asInt(f["activityCount7d"]) > 0 || asFloat(f["daysSinceActivity"]) <= 7 {
		likelihood += 8
		pos = append(pos, Signal{Code: "recent_activity", Label: "Recent engagement", Impact: 8, Polarity: "positive"})
	}
	if asInt(f["overdueFollowups"]) > 0 {
		likelihood -= 10
		neg = append(neg, Signal{Code: "overdue", Label: "Overdue follow-up", Impact: -10, Polarity: "negative"})
	}
	if asFloat(f["profileCompletenessPct"]) >= 70 || asInt(f["docsVerified"]) > 0 {
		likelihood += 6
		pos = append(pos, Signal{Code: "readiness", Label: "Profile/docs readiness", Impact: 6, Polarity: "positive"})
	}
	likelihood = clamp(round2(likelihood), 0, 100)
	res.Available = true
	res.Score = &likelihood
	res.Label = fmt.Sprintf("%.0f%% estimated conversion likelihood", likelihood)
	res.Explanation = Explanation{
		Summary:         res.Label + " (estimate from historical rates + current signals).",
		PositiveSignals: pos, NegativeSignals: neg,
		NeutralNotes: []string{
			fmt.Sprintf("Historical sample size: %d", sample),
			fmt.Sprintf("Baseline rate: %.1f%%", baseRate),
		},
	}
	res.Payload = map[string]any{"baselineRate": baseRate, "sampleSize": sample}
	return res, nil
}

type StalledOpportunityStrategy struct{}

func (s StalledOpportunityStrategy) Code() string        { return StrategyStalledOpportunityV1 }
func (s StalledOpportunityStrategy) Version() string     { return "1" }
func (s StalledOpportunityStrategy) InsightType() string { return "stalled_opportunity" }

func (s StalledOpportunityStrategy) Score(fs FeatureSet) (*PredictionResult, error) {
	f := fs.Features
	res := baseResult(s, fs)
	days := asInt(f["daysInStage"])
	if days == 0 {
		days = asInt(f["ageDays"])
	}
	sla := asInt(f["slaHours"])
	inactive := asFloat(f["daysSinceActivity"])
	if inactive == 0 && asInt(f["activityCount30d"]) == 0 {
		inactive = float64(days)
	}
	risk := 0.0
	var pos, neg []Signal
	if days >= 14 {
		risk += 35
		neg = append(neg, Signal{Code: "long_stage", Label: "Long time in stage", Impact: -35, Polarity: "negative",
			Detail: fmt.Sprintf("%d days in current stage", days)})
	}
	if sla > 0 && days*24 > sla {
		risk += 25
		neg = append(neg, Signal{Code: "over_sla", Label: "Past stage SLA", Impact: -25, Polarity: "negative"})
	}
	if inactive >= 14 || asInt(f["activityCount30d"]) == 0 {
		risk += 25
		neg = append(neg, Signal{Code: "no_recent_activity", Label: "No recent activity", Impact: -25, Polarity: "negative"})
	}
	if asInt(f["overdueFollowups"]) > 0 {
		risk += 15
		neg = append(neg, Signal{Code: "overdue", Label: "Follow-up overdue", Impact: -15, Polarity: "negative"})
	}
	if asInt(f["activityCount30d"]) >= 3 {
		risk -= 15
		pos = append(pos, Signal{Code: "active", Label: "Active engagement", Impact: 15, Polarity: "positive"})
	}
	risk = clamp(round2(risk), 0, 100)
	label := "Healthy pace"
	if risk >= 70 {
		label = "Likely stalled"
	} else if risk >= 40 {
		label = "At risk of stalling"
	}
	res.Available = true
	res.Score = &risk
	res.Label = label
	res.Explanation = Explanation{
		Summary:         fmt.Sprintf("%s (stall risk score %.0f).", label, risk),
		PositiveSignals: pos, NegativeSignals: neg,
	}
	res.Payload = map[string]any{"daysInStage": days, "stallRisk": risk}
	return res, nil
}

type FollowUpRiskStrategy struct{}

func (s FollowUpRiskStrategy) Code() string        { return StrategyFollowUpRiskV1 }
func (s FollowUpRiskStrategy) Version() string     { return "1" }
func (s FollowUpRiskStrategy) InsightType() string { return "follow_up_risk" }

func (s FollowUpRiskStrategy) Score(fs FeatureSet) (*PredictionResult, error) {
	f := fs.Features
	res := baseResult(s, fs)
	risk := 20.0
	var pos, neg []Signal
	overdue := asInt(f["overdueFollowups"])
	if overdue > 0 {
		risk += 40
		neg = append(neg, Signal{Code: "follow_up_overdue", Label: "Follow-up overdue", Impact: -40, Polarity: "negative",
			Detail: fmt.Sprintf("%d overdue", overdue)})
	}
	days := asFloat(f["daysSinceActivity"])
	if days > 10 {
		risk += 25
		neg = append(neg, Signal{Code: "inactivity", Label: "Inactivity", Impact: -25, Polarity: "negative",
			Detail: fmt.Sprintf("%.0f days quiet", days)})
	}
	if asBool(f["hasNextActivity"]) {
		risk -= 20
		pos = append(pos, Signal{Code: "next_planned", Label: "Next activity planned", Impact: 20, Polarity: "positive"})
	}
	if asInt(f["activityCount7d"]) > 0 {
		risk -= 15
		pos = append(pos, Signal{Code: "recent", Label: "Recently contacted", Impact: 15, Polarity: "positive"})
	}
	risk = clamp(round2(risk), 0, 100)
	label := "Follow-up on track"
	if risk >= 60 {
		label = "High follow-up risk"
	} else if risk >= 35 {
		label = "Elevated follow-up risk"
	}
	res.Available = true
	res.Score = &risk
	res.Label = label
	res.Explanation = Explanation{
		Summary: label, PositiveSignals: pos, NegativeSignals: neg,
	}
	return res, nil
}

type ExpectedOutcomeStrategy struct{}

func (s ExpectedOutcomeStrategy) Code() string        { return StrategyExpectedOutcomeV1 }
func (s ExpectedOutcomeStrategy) Version() string     { return "1" }
func (s ExpectedOutcomeStrategy) InsightType() string { return "expected_outcome" }

func (s ExpectedOutcomeStrategy) Score(fs FeatureSet) (*PredictionResult, error) {
	f := fs.Features
	res := baseResult(s, fs)
	sample := asInt(f["pipelineClosedSample"])
	if sample == 0 {
		sample = asInt(f["sourceClosedSample"])
	}
	if sample < MinHistoricalSample {
		res.Available = false
		res.Message = InsufficientHistoricalData
		res.InsufficientHist = true
		res.Explanation.Summary = InsufficientHistoricalData
		res.Explanation.NeutralNotes = []string{
			fmt.Sprintf("Closed historical sample: %d (minimum %d).", sample, MinHistoricalSample),
		}
		return res, nil
	}
	win := asFloat(f["pipelineWinRate"])
	if win == 0 {
		win = asFloat(f["sourceConversionRate"])
	}
	stageProb := asFloat(f["stageProbability"])
	blended := win
	if stageProb > 0 {
		blended = (win + stageProb) / 2
	}
	label := "Uncertain"
	switch {
	case blended >= 60:
		label = "Positive outcome more likely"
	case blended >= 35:
		label = "Mixed / depends on execution"
	default:
		label = "Lower likelihood of positive outcome"
	}
	var pos, neg []Signal
	if stageProb >= 50 {
		pos = append(pos, Signal{Code: "stage_prob", Label: "Favorable stage probability", Impact: stageProb, Polarity: "positive",
			Detail: fmt.Sprintf("%.0f%% stage probability", stageProb)})
	}
	if win < 30 {
		neg = append(neg, Signal{Code: "hist_win", Label: "Modest historical win rate", Impact: -win, Polarity: "negative",
			Detail: fmt.Sprintf("%.0f%% historical", win)})
	} else {
		pos = append(pos, Signal{Code: "hist_win", Label: "Supportive historical outcomes", Impact: win, Polarity: "positive",
			Detail: fmt.Sprintf("%.0f%% historical", win)})
	}
	score := round2(blended)
	res.Available = true
	res.Score = &score
	res.Label = label
	res.Explanation = Explanation{
		Summary:         fmt.Sprintf("%s (blended estimate %.0f%%).", label, blended),
		PositiveSignals: pos, NegativeSignals: neg,
		NeutralNotes: []string{fmt.Sprintf("Sample size: %d", sample)},
	}
	res.Payload = map[string]any{"blendedLikelihood": blended, "sampleSize": sample}
	return res, nil
}

type WorkloadForecastStrategy struct{}

func (s WorkloadForecastStrategy) Code() string        { return StrategyWorkloadForecastV1 }
func (s WorkloadForecastStrategy) Version() string     { return "1" }
func (s WorkloadForecastStrategy) InsightType() string { return "workload_forecast" }

func (s WorkloadForecastStrategy) Score(fs FeatureSet) (*PredictionResult, error) {
	f := fs.Features
	res := baseResult(s, fs)
	idx := asFloat(f["workloadIndex"])
	due := asInt(f["dueNext7Days"])
	overdue := asInt(f["overdueActivities"])
	openDeals := asInt(f["openDeals"])
	openLeads := asInt(f["openLeads"])
	var pos, neg []Signal
	if overdue > 0 {
		neg = append(neg, Signal{Code: "overdue_load", Label: "Overdue backlog", Impact: -float64(overdue * 5), Polarity: "negative",
			Detail: fmt.Sprintf("%d overdue activities", overdue)})
	}
	if due >= 8 {
		neg = append(neg, Signal{Code: "dense_week", Label: "Dense upcoming week", Impact: -15, Polarity: "negative",
			Detail: fmt.Sprintf("%d due in 7 days", due)})
	} else if due > 0 {
		pos = append(pos, Signal{Code: "planned_week", Label: "Planned follow-ups ahead", Impact: 5, Polarity: "positive"})
	}
	if asInt(f["completedLast7Days"]) >= 5 {
		pos = append(pos, Signal{Code: "throughput", Label: "Healthy recent throughput", Impact: 8, Polarity: "positive"})
	}
	label := "Manageable workload"
	if idx >= 40 || overdue >= 5 {
		label = "Heavy workload forecast"
	} else if idx >= 20 {
		label = "Elevated workload"
	}
	score := clamp(round2(idx), 0, 100)
	res.Available = true
	res.Score = &score
	res.Label = label
	res.Explanation = Explanation{
		Summary: fmt.Sprintf("%s — open leads %d, open deals %d, due next 7d %d.", label, openLeads, openDeals, due),
		PositiveSignals: pos, NegativeSignals: neg,
	}
	res.Payload = map[string]any{
		"openLeads": openLeads, "openDeals": openDeals, "dueNext7Days": due, "overdueActivities": overdue,
	}
	return res, nil
}

type PipelineRiskStrategy struct{}

func (s PipelineRiskStrategy) Code() string        { return StrategyPipelineRiskV1 }
func (s PipelineRiskStrategy) Version() string     { return "1" }
func (s PipelineRiskStrategy) InsightType() string { return "pipeline_risk" }

func (s PipelineRiskStrategy) Score(fs FeatureSet) (*PredictionResult, error) {
	f := fs.Features
	res := baseResult(s, fs)
	open := asInt(f["openDealCount"])
	if open == 0 {
		res.Available = true
		res.Label = "No open pipeline"
		zero := 0.0
		res.Score = &zero
		res.Explanation.Summary = "No open deals in this pipeline."
		return res, nil
	}
	stalled := asInt(f["stalledDeals14d"])
	overSLA := asInt(f["overSLADeals"])
	quiet := asInt(f["noActivity14d"])
	risk := float64(stalled+overSLA+quiet) / float64(open) * 100
	pos := []Signal{}
	neg := []Signal{}
	if stalled > 0 {
		neg = append(neg, Signal{Code: "stalled", Label: "Stalled opportunities", Impact: -float64(stalled), Polarity: "negative",
			Detail: fmt.Sprintf("%d deals >=14 days in stage", stalled)})
	}
	if overSLA > 0 {
		neg = append(neg, Signal{Code: "sla", Label: "Over SLA", Impact: -float64(overSLA), Polarity: "negative",
			Detail: fmt.Sprintf("%d deals past SLA", overSLA)})
	}
	if quiet > 0 {
		neg = append(neg, Signal{Code: "quiet", Label: "No recent activity", Impact: -float64(quiet), Polarity: "negative",
			Detail: fmt.Sprintf("%d deals quiet 14d+", quiet)})
	}
	activeShare := open - quiet
	if activeShare > open/2 {
		pos = append(pos, Signal{Code: "active_majority", Label: "Majority still active", Impact: 10, Polarity: "positive"})
	}
	sample := asInt(f["closedSample"])
	notes := []string{}
	if sample < MinHistoricalSample {
		res.InsufficientHist = true
		notes = append(notes, InsufficientHistoricalData+" Historical win rate is informational only.")
	}
	risk = clamp(round2(risk), 0, 100)
	label := "Pipeline healthy"
	if risk >= 60 {
		label = "Elevated pipeline risk"
	} else if risk >= 30 {
		label = "Moderate pipeline risk"
	}
	res.Available = true
	res.Score = &risk
	res.Label = label
	res.Explanation = Explanation{
		Summary: fmt.Sprintf("%s (%.0f%% of open deals show risk signals).", label, risk),
		PositiveSignals: pos, NegativeSignals: neg, NeutralNotes: notes,
	}
	res.Payload = map[string]any{
		"openDealCount": open, "stalledDeals14d": stalled, "overSLADeals": overSLA, "noActivity14d": quiet,
		"openPipelineValue": f["openPipelineValue"],
	}
	return res, nil
}

func baseResult(s ScoringStrategy, fs FeatureSet) *PredictionResult {
	return &PredictionResult{
		Disclaimer: Disclaimer,
		InsightType: s.InsightType(), EntityType: fs.EntityType, EntityID: fs.EntityID,
		StrategyCode: s.Code(), StrategyVersion: s.Version(),
		Features: fs.Features,
		Explanation: Explanation{
			PositiveSignals: []Signal{},
			NegativeSignals: []Signal{},
		},
		Payload: map[string]any{}, ComputedAt: time.Now().UTC().Format(time.RFC3339),
	}
}
