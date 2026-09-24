package forecasting

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/crm/backend/pkg/apperrors"
)

// Methodology identifiers — keep stable so the frontend API can evolve models later.
const (
	MethodologyWeightedPipelineV1 = "weighted_pipeline_v1"
	MinClosedSample               = 10
	InsufficientMessage           = "Not enough historical data for a reliable forecast."
	Disclaimer                    = "Forecasts are estimates based on historical outcomes and current pipeline. They are not guaranteed results."
)

type Request struct {
	PipelineID string
	TeamID     string
	OwnerUserID string
	From       time.Time
	To         time.Time
	HistoryFrom time.Time
	HistoryTo   time.Time
}

type ScenarioValues struct {
	Conservative float64 `json:"conservative"`
	Expected     float64 `json:"expected"`
	HigherCase   float64 `json:"higherCase"`
}

type Transparency struct {
	Period              DateRange      `json:"period"`
	PipelineID          string         `json:"pipelineId"`
	PipelineName        string         `json:"pipelineName"`
	SampleSize          int            `json:"sampleSize"`
	HistoricalDataRange DateRange      `json:"historicalDataRange"`
	Inputs              map[string]any `json:"inputs"`
	Methodology         Methodology    `json:"methodology"`
}

type DateRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type Methodology struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
}

type Result struct {
	Available     bool            `json:"available"`
	Message       string          `json:"message,omitempty"`
	Disclaimer    string          `json:"disclaimer"`
	Scenarios     *ScenarioValues `json:"scenarios,omitempty"`
	OpenDealCount int             `json:"openDealCount"`
	OpenPipeline  float64         `json:"openPipelineValue"`
	Transparency  Transparency    `json:"transparency"`
}

type openDeal struct {
	ID              string
	Value           float64
	StageID         *string
	StageName       *string
	Probability     float64
	ExpectedCloseAt *time.Time
	StageEnteredAt  time.Time
	DaysInStage     float64
}

type histStats struct {
	ClosedCount      int
	WonCount         int
	WinRate          float64
	AvgStageDuration float64
	HistoryFrom      time.Time
	HistoryTo        time.Time
}

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

func (s *Service) Forecast(ctx context.Context, req Request) (*Result, error) {
	if req.PipelineID == "" {
		return nil, apperrors.Validation("pipelineId is required")
	}
	now := time.Now().UTC()
	if req.From.IsZero() {
		req.From = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	if req.To.IsZero() {
		req.To = req.From.AddDate(0, 3, -1)
	}
	if req.HistoryTo.IsZero() {
		req.HistoryTo = now
	}
	if req.HistoryFrom.IsZero() {
		req.HistoryFrom = req.HistoryTo.AddDate(-1, 0, 0)
	}

	pipeName, err := s.pipelineName(ctx, req.PipelineID)
	if err != nil {
		return nil, apperrors.NotFound("pipeline not found")
	}

	stats, err := s.historicalStats(ctx, req)
	if err != nil {
		return nil, apperrors.Internal("failed to load historical outcomes", err)
	}

	deals, err := s.openDeals(ctx, req)
	if err != nil {
		return nil, apperrors.Internal("failed to load open pipeline", err)
	}

	var openValue float64
	for _, d := range deals {
		openValue += d.Value
	}

	method := Methodology{
		Code:    MethodologyWeightedPipelineV1,
		Name:    "Weighted pipeline (v1)",
		Version: "1",
		Description: "Open deal values are weighted by stage probability, adjusted with historical win rate. " +
			"Conservative uses 0.7× expected weight; higher-case uses 1.3× capped at 100%. " +
			"Deals with expected close dates outside the forecast period are excluded from period totals.",
	}

	transparency := Transparency{
		Period: DateRange{From: req.From.Format("2006-01-02"), To: req.To.Format("2006-01-02")},
		PipelineID: req.PipelineID, PipelineName: pipeName,
		SampleSize:          stats.ClosedCount,
		HistoricalDataRange: DateRange{From: req.HistoryFrom.Format("2006-01-02"), To: req.HistoryTo.Format("2006-01-02")},
		Inputs: map[string]any{
			"openDealCount":          len(deals),
			"openPipelineValue":      round2(openValue),
			"historicalWinRate":      round2(stats.WinRate * 100),
			"avgStageDurationSeconds": round2(stats.AvgStageDuration),
			"teamId":                 req.TeamID,
			"ownerUserId":            req.OwnerUserID,
			"minClosedSample":        MinClosedSample,
		},
		Methodology: method,
	}

	result := &Result{
		Disclaimer:    Disclaimer,
		OpenDealCount: len(deals),
		OpenPipeline:  round2(openValue),
		Transparency:  transparency,
	}

	if stats.ClosedCount < MinClosedSample {
		result.Available = false
		result.Message = InsufficientMessage
		return result, nil
	}

	var expected, conservative, higher float64
	included := 0
	for _, d := range deals {
		if d.ExpectedCloseAt != nil {
			closeDay := d.ExpectedCloseAt.UTC().Truncate(24 * time.Hour)
			if closeDay.Before(req.From.Truncate(24*time.Hour)) || closeDay.After(req.To.Truncate(24*time.Hour)) {
				continue
			}
		}
		included++
		stageProb := d.Probability / 100
		if stageProb < 0 {
			stageProb = 0
		}
		if stageProb > 1 {
			stageProb = 1
		}
		// Blend stage probability with historical win rate (equal weight).
		weight := (stageProb + stats.WinRate) / 2
		if weight < 0 {
			weight = 0
		}
		if weight > 1 {
			weight = 1
		}
		base := d.Value * weight
		expected += base
		conservative += d.Value * math.Min(1, weight*0.7)
		higher += d.Value * math.Min(1, weight*1.3)
	}

	transparency.Inputs["dealsIncludedInPeriod"] = included
	result.Transparency = transparency
	result.Available = true
	result.Scenarios = &ScenarioValues{
		Conservative: round2(conservative),
		Expected:     round2(expected),
		HigherCase:   round2(higher),
	}
	return result, nil
}

func (s *Service) pipelineName(ctx context.Context, id string) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `SELECT name FROM pipelines WHERE id=$1`, id).Scan(&name)
	return name, err
}

func (s *Service) historicalStats(ctx context.Context, req Request) (*histStats, error) {
	st := &histStats{HistoryFrom: req.HistoryFrom, HistoryTo: req.HistoryTo}
	err := s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE d.status IN ('won','lost')),
			COUNT(*) FILTER (WHERE d.status='won')
		FROM deals d
		WHERE d.pipeline_id=$1
		  AND d.status IN ('won','lost')
		  AND d.updated_at >= $2 AND d.updated_at <= $3
		  AND ($4='' OR d.team_id::text=$4)
		  AND ($5='' OR d.owner_user_id::text=$5)
	`, req.PipelineID, req.HistoryFrom, req.HistoryTo, req.TeamID, req.OwnerUserID).Scan(&st.ClosedCount, &st.WonCount)
	if err != nil {
		return nil, err
	}
	if st.ClosedCount > 0 {
		st.WinRate = float64(st.WonCount) / float64(st.ClosedCount)
	}
	_ = s.pool.QueryRow(ctx, `
		SELECT COALESCE(AVG(t.duration_seconds),0)::float8
		FROM deal_stage_transitions t
		JOIN deals d ON d.id = t.deal_id
		WHERE d.pipeline_id=$1
		  AND t.from_stage_id IS NOT NULL
		  AND t.duration_seconds > 0
		  AND t.exited_at >= $2 AND t.exited_at <= $3
		  AND ($4='' OR d.team_id::text=$4)
		  AND ($5='' OR d.owner_user_id::text=$5)
	`, req.PipelineID, req.HistoryFrom, req.HistoryTo, req.TeamID, req.OwnerUserID).Scan(&st.AvgStageDuration)
	return st, nil
}

func (s *Service) openDeals(ctx context.Context, req Request) ([]openDeal, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d.id::text, COALESCE(d.value,0)::float8, d.stage_id::text, ps.name,
			COALESCE(ps.probability,0)::float8, d.expected_close_at, d.stage_entered_at,
			EXTRACT(EPOCH FROM (NOW() - d.stage_entered_at))::float8
		FROM deals d
		LEFT JOIN pipeline_stages ps ON ps.id = d.stage_id
		WHERE d.status='open' AND d.pipeline_id=$1
		  AND ($2='' OR d.team_id::text=$2)
		  AND ($3='' OR d.owner_user_id::text=$3)
	`, req.PipelineID, req.TeamID, req.OwnerUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []openDeal
	for rows.Next() {
		var d openDeal
		if err := rows.Scan(&d.ID, &d.Value, &d.StageID, &d.StageName, &d.Probability,
			&d.ExpectedCloseAt, &d.StageEnteredAt, &d.DaysInStage); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func ParseRequest(q map[string]string) (Request, error) {
	req := Request{
		PipelineID:  strings.TrimSpace(q["pipelineId"]),
		TeamID:      strings.TrimSpace(q["teamId"]),
		OwnerUserID: strings.TrimSpace(q["ownerUserId"]),
	}
	var err error
	if v := strings.TrimSpace(q["from"]); v != "" {
		req.From, err = parseDay(v)
		if err != nil {
			return req, apperrors.Validation("invalid from")
		}
	}
	if v := strings.TrimSpace(q["to"]); v != "" {
		req.To, err = parseDay(v)
		if err != nil {
			return req, apperrors.Validation("invalid to")
		}
	}
	if v := strings.TrimSpace(q["historyFrom"]); v != "" {
		req.HistoryFrom, err = parseDay(v)
		if err != nil {
			return req, apperrors.Validation("invalid historyFrom")
		}
	}
	if v := strings.TrimSpace(q["historyTo"]); v != "" {
		req.HistoryTo, err = parseDay(v)
		if err != nil {
			return req, apperrors.Validation("invalid historyTo")
		}
	}
	return req, nil
}

func parseDay(v string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UTC(), nil
	}
	return time.Parse("2006-01-02", v)
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
