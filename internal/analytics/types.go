package analytics

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

type FunnelStage struct {
	StageID            string   `json:"stageId"`
	StageName          string   `json:"stageName"`
	Position           int      `json:"position"`
	IsWon              bool     `json:"isWon"`
	IsLost             bool     `json:"isLost"`
	Entered            int      `json:"entered"`
	Left               int      `json:"left"`
	CurrentlyInStage   int      `json:"currentlyInStage"`
	ConversionRate     float64  `json:"conversionRate"`
	DropOffRate        float64  `json:"dropOffRate"`
	AvgTimeInStageSecs *float64 `json:"avgTimeInStageSeconds"`
	TotalValue         float64  `json:"totalPipelineValue"`
	AvgDealValue       float64  `json:"averageDealValue"`
	EnteredValue       float64  `json:"enteredValue"`
}

type FunnelResult struct {
	PipelineID   string        `json:"pipelineId"`
	PipelineName string        `json:"pipelineName"`
	PipelineKind string        `json:"pipelineKind"`
	From         time.Time     `json:"from"`
	To           time.Time     `json:"to"`
	Stages       []FunnelStage `json:"stages"`
	Totals       FunnelTotals  `json:"totals"`
}

type FunnelTotals struct {
	Entered          int     `json:"entered"`
	Won              int     `json:"won"`
	Lost             int     `json:"lost"`
	Open             int     `json:"open"`
	TotalValue       float64 `json:"totalPipelineValue"`
	AvgDealValue     float64 `json:"averageDealValue"`
	OverallConversion float64 `json:"overallConversionRate"`
}

type MetricPoint struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

type LeadAnalytics struct {
	LeadVolume       int           `json:"leadVolume"`
	QualifiedLeads   int           `json:"qualifiedLeads"`
	ConvertedLeads   int           `json:"convertedLeads"`
	ConversionRate   float64       `json:"conversionRate"`
	AvgResponseSecs  *float64      `json:"averageResponseTimeSeconds"`
	BySource         []MetricPoint `json:"bySource"`
	ByDay            []MetricPoint `json:"volumeByDay"`
	Question         string        `json:"question"`
}

type PipelineAnalytics struct {
	Deals            int           `json:"deals"`
	OpenDeals        int           `json:"openDeals"`
	WonDeals         int           `json:"wonDeals"`
	LostDeals        int           `json:"lostDeals"`
	PipelineValue    float64       `json:"pipelineValue"`
	AvgDealValue     float64       `json:"averageDealValue"`
	AvgStageDuration *float64      `json:"averageStageDurationSeconds"`
	ByStage          []MetricPoint `json:"openValueByStage"`
	Question         string        `json:"question"`
}

type ActivityAnalytics struct {
	Activities      int           `json:"activities"`
	Completed       int           `json:"completed"`
	Overdue         int           `json:"overdue"`
	ByType          []MetricPoint `json:"byType"`
	ByDay           []MetricPoint `json:"byDay"`
	AvgPerDeal      float64       `json:"averagePerDeal"`
	Question        string        `json:"question"`
}

type ConversionAnalytics struct {
	LeadToCustomerRate float64       `json:"leadToCustomerRate"`
	DealWinRate        float64       `json:"dealWinRate"`
	LeadsCreated       int           `json:"leadsCreated"`
	LeadsConverted     int           `json:"leadsConverted"`
	DealsCreated       int           `json:"dealsCreated"`
	DealsWon           int           `json:"dealsWon"`
	DealsLost          int           `json:"dealsLost"`
	PositiveOutcomes   int           `json:"positiveOutcomes"`
	Submissions        int           `json:"submissions"`
	Funnel             []MetricPoint `json:"conversionFunnel"`
	Question           string        `json:"question"`
}

type TeamRow struct {
	EntityID         string  `json:"entityId"`
	EntityName       string  `json:"entityName"`
	EntityType       string  `json:"entityType"` // team | user | period | pipeline
	LeadVolume       int     `json:"leadVolume"`
	Deals            int     `json:"deals"`
	WonDeals         int     `json:"wonDeals"`
	Activities       int     `json:"activities"`
	PipelineValue    float64 `json:"pipelineValue"`
	ConversionRate   float64 `json:"conversionRate"`
	AvgStageDuration *float64 `json:"averageStageDurationSeconds"`
}

type TeamAnalytics struct {
	GroupBy  string    `json:"groupBy"`
	SortBy   string    `json:"sortBy"`
	Rows     []TeamRow `json:"rows"`
	Question string    `json:"question"`
}

type SourceAnalytics struct {
	Sources  []SourceRow `json:"sources"`
	Question string      `json:"question"`
}

type SourceRow struct {
	Source         string  `json:"source"`
	Leads          int     `json:"leads"`
	Customers      int     `json:"customers"`
	Deals          int     `json:"deals"`
	WonDeals       int     `json:"wonDeals"`
	PipelineValue  float64 `json:"pipelineValue"`
	ConversionRate float64 `json:"conversionRate"`
}

type Summary struct {
	LeadVolume      int      `json:"leadVolume"`
	QualifiedLeads  int      `json:"qualifiedLeads"`
	Deals           int      `json:"deals"`
	Submissions     int      `json:"submissions"`
	PositiveOutcomes int     `json:"positiveOutcomes"`
	Conversions     int      `json:"conversions"`
	ConversionRate  float64  `json:"conversionRate"`
	AvgResponseSecs *float64 `json:"averageResponseTimeSeconds"`
	AvgStageSecs    *float64 `json:"averageStageDurationSeconds"`
	Activities      int      `json:"activities"`
	PipelineValue   float64  `json:"pipelineValue"`
	From            time.Time `json:"from"`
	To              time.Time `json:"to"`
}

func (r *Repository) PipelineMeta(ctx context.Context, id string) (name, kind string, err error) {
	err = r.pool.QueryRow(ctx, `SELECT name, kind FROM pipelines WHERE id=$1`, id).Scan(&name, &kind)
	return
}

func (r *Repository) DefaultSalesPipeline(ctx context.Context) (id, name, kind string, err error) {
	err = r.pool.QueryRow(ctx, `
		SELECT id::text, name, kind FROM pipelines
		WHERE kind='sales' AND is_active=TRUE
		ORDER BY is_default DESC, name ASC
		LIMIT 1
	`).Scan(&id, &name, &kind)
	return
}

type stageRow struct {
	ID       string
	Name     string
	Position int
	IsWon    bool
	IsLost   bool
}

func (r *Repository) ListStages(ctx context.Context, pipelineID string) ([]stageRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, name, position, is_won, is_lost
		FROM pipeline_stages
		WHERE pipeline_id=$1 AND is_active=TRUE
		ORDER BY position ASC
	`, pipelineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []stageRow
	for rows.Next() {
		var s stageRow
		if err := rows.Scan(&s.ID, &s.Name, &s.Position, &s.IsWon, &s.IsLost); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
