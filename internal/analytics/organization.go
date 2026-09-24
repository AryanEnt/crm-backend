package analytics

import (
	"context"
	"fmt"
	"time"
)

type OrganizationTopMetrics struct {
	TotalUsers            int     `json:"totalUsers"`
	ActiveUsers           int     `json:"activeUsers"`
	TotalTeams            int     `json:"totalTeams"`
	TotalLeads            int     `json:"totalLeads"`
	TotalCustomers        int     `json:"totalCustomers"`
	TotalDeals            int     `json:"totalDeals"`
	ActiveDeals           int     `json:"activeDeals"`
	CompletedActivities   int     `json:"completedActivities"`
	PipelineValue         float64 `json:"pipelineValue"`
}

type UserAdoptionMetrics struct {
	ActiveUsers              int `json:"activeUsers"`
	UsersWithActivity        int `json:"usersWithActivity"`
	UsersWithNoRecentActivity int `json:"usersWithNoRecentActivity"`
	ActivitiesCreated        int `json:"activitiesCreated"`
	ActivitiesCompleted      int `json:"activitiesCompleted"`
}

type GrowthSeries struct {
	Leads      []MetricPoint `json:"leads"`
	Customers  []MetricPoint `json:"customers"`
	Deals      []MetricPoint `json:"deals"`
	Activities []MetricPoint `json:"activities"`
}

type OrgPipelineOverview struct {
	ActivePipelineValue float64       `json:"activePipelineValue"`
	DealsByPipeline     []MetricPoint `json:"dealsByPipeline"`
	DealsByStage        []MetricPoint `json:"dealsByStage"`
	AvgDealValue        float64       `json:"avgDealValue"`
}

type OrgConversionOverview struct {
	Leads            int     `json:"leads"`
	QualifiedLeads   int     `json:"qualifiedLeads"`
	Deals            int     `json:"deals"`
	Conversions      int     `json:"conversions"`
	ConversionRate   float64 `json:"conversionRate"`
}

type ReferralOverview struct {
	Total          int           `json:"total"`
	Active         int           `json:"active"`
	Converted      int           `json:"converted"`
	PipelineValue  float64       `json:"pipelineValue"`
	Trend          []MetricPoint `json:"trend"`
}

type OrganizationAnalytics struct {
	TopMetrics          OrganizationTopMetrics `json:"topMetrics"`
	UserAdoption        UserAdoptionMetrics    `json:"userAdoption"`
	Growth              GrowthSeries           `json:"growth"`
	PipelineOverview    OrgPipelineOverview    `json:"pipelineOverview"`
	ConversionOverview  OrgConversionOverview  `json:"conversionOverview"`
	ReferralOverview    ReferralOverview       `json:"referralOverview"`
	From                time.Time              `json:"from"`
	To                  time.Time              `json:"to"`
}

func growthTrunc(f Filter) string {
	switch f.Period {
	case "week":
		return "week"
	case "month":
		return "month"
	default:
		return "day"
	}
}

func (r *Repository) Organization(ctx context.Context, f Filter) (*OrganizationAnalytics, error) {
	out := &OrganizationAnalytics{
		From: f.From, To: f.To,
		Growth: GrowthSeries{
			Leads: []MetricPoint{}, Customers: []MetricPoint{},
			Deals: []MetricPoint{}, Activities: []MetricPoint{},
		},
		PipelineOverview: OrgPipelineOverview{
			DealsByPipeline: []MetricPoint{}, DealsByStage: []MetricPoint{},
		},
		ReferralOverview: ReferralOverview{Trend: []MetricPoint{}},
	}

	if err := r.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*)::int FROM users),
			(SELECT COUNT(*)::int FROM users WHERE is_active = TRUE),
			(SELECT COUNT(*)::int FROM teams WHERE is_active = TRUE),
			(SELECT COUNT(*)::int FROM leads WHERE is_archived = FALSE),
			(SELECT COUNT(*)::int FROM customers WHERE is_archived = FALSE),
			(SELECT COUNT(*)::int FROM deals),
			(SELECT COUNT(*)::int FROM deals WHERE status = 'open'),
			(SELECT COUNT(*)::int FROM activities WHERE status = 'completed'),
			COALESCE((SELECT SUM(value) FROM deals WHERE status = 'open'), 0)::float8
	`).Scan(
		&out.TopMetrics.TotalUsers, &out.TopMetrics.ActiveUsers, &out.TopMetrics.TotalTeams,
		&out.TopMetrics.TotalLeads, &out.TopMetrics.TotalCustomers, &out.TopMetrics.TotalDeals,
		&out.TopMetrics.ActiveDeals, &out.TopMetrics.CompletedActivities, &out.TopMetrics.PipelineValue,
	); err != nil {
		return nil, err
	}

	ab := &sqlBuilder{}
	ab.add("a.created_at >= ?", f.From)
	ab.add("a.created_at <= ?", f.To)
	if f.OwnerUserID != "" {
		ab.add("COALESCE(a.owner_user_id, a.actor_user_id)::text = ?", f.OwnerUserID)
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*)::int,
			COUNT(*) FILTER (WHERE a.status = 'completed')::int
		FROM activities a WHERE `+ab.sql(), ab.args...).Scan(
		&out.UserAdoption.ActivitiesCreated, &out.UserAdoption.ActivitiesCompleted,
	); err != nil {
		return nil, err
	}

	out.UserAdoption.ActiveUsers = out.TopMetrics.ActiveUsers
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT COALESCE(a.owner_user_id, a.actor_user_id))::int
		FROM activities a
		WHERE a.created_at >= $1 AND a.created_at <= $2
		  AND COALESCE(a.owner_user_id, a.actor_user_id) IS NOT NULL
	`, f.From, f.To).Scan(&out.UserAdoption.UsersWithActivity); err != nil {
		return nil, err
	}
	out.UserAdoption.UsersWithNoRecentActivity = out.UserAdoption.ActiveUsers - out.UserAdoption.UsersWithActivity
	if out.UserAdoption.UsersWithNoRecentActivity < 0 {
		out.UserAdoption.UsersWithNoRecentActivity = 0
	}

	trunc := growthTrunc(f)
	truncFmt := fmt.Sprintf(`
		SELECT to_char(date_trunc('%s', created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD'), COUNT(*)::float8
		FROM %%s WHERE created_at >= $1 AND created_at <= $2 AND is_archived = FALSE
		GROUP BY 1 ORDER BY 1 ASC`, trunc)

	if err := r.scanGrowth(ctx, fmt.Sprintf(truncFmt, "leads"), f.From, f.To, &out.Growth.Leads); err != nil {
		return nil, err
	}
	if err := r.scanGrowth(ctx, fmt.Sprintf(truncFmt, "customers"), f.From, f.To, &out.Growth.Customers); err != nil {
		return nil, err
	}

	db := &sqlBuilder{}
	db.add("d.created_at >= ?", f.From)
	db.add("d.created_at <= ?", f.To)
	if f.PipelineID != "" {
		db.add("d.pipeline_id::text = ?", f.PipelineID)
	}
	dealGrowthSQL := fmt.Sprintf(`
		SELECT to_char(date_trunc('%s', d.created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD'), COUNT(*)::float8
		FROM deals d WHERE `+db.sql()+` GROUP BY 1 ORDER BY 1 ASC`, trunc)
	rows, err := r.pool.Query(ctx, dealGrowthSQL, db.args...)
	if err != nil {
		return nil, err
	}
	out.Growth.Deals, err = scanMetricRows(rows)
	if err != nil {
		return nil, err
	}

	actGrowthSQL := fmt.Sprintf(`
		SELECT to_char(date_trunc('%s', a.created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD'), COUNT(*)::float8
		FROM activities a WHERE `+ab.sql()+` GROUP BY 1 ORDER BY 1 ASC`, trunc)
	rows2, err := r.pool.Query(ctx, actGrowthSQL, ab.args...)
	if err != nil {
		return nil, err
	}
	out.Growth.Activities, err = scanMetricRows(rows2)
	if err != nil {
		return nil, err
	}

	pb := &sqlBuilder{}
	pb.add("d.status = ?", "open")
	if f.PipelineID != "" {
		pb.add("d.pipeline_id::text = ?", f.PipelineID)
	}
	if f.TeamID != "" {
		pb.add("d.team_id::text = ?", f.TeamID)
	}
	if f.OwnerUserID != "" {
		pb.add("d.owner_user_id::text = ?", f.OwnerUserID)
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(d.value),0)::float8, COALESCE(AVG(d.value),0)::float8
		FROM deals d WHERE `+pb.sql(), pb.args...).Scan(
		&out.PipelineOverview.ActivePipelineValue, &out.PipelineOverview.AvgDealValue,
	); err != nil {
		return nil, err
	}
	out.PipelineOverview.AvgDealValue = round2(out.PipelineOverview.AvgDealValue)

	rows3, err := r.pool.Query(ctx, `
		SELECT COALESCE(p.id::text,'none'), COALESCE(p.name,'No pipeline'), COUNT(*)::float8
		FROM deals d
		LEFT JOIN pipelines p ON p.id = d.pipeline_id
		WHERE d.status = 'open'
		GROUP BY p.id, p.name
		ORDER BY 3 DESC
	`)
	if err != nil {
		return nil, err
	}
	out.PipelineOverview.DealsByPipeline, err = scanLabeledMetricRows(rows3)
	if err != nil {
		return nil, err
	}

	rows4, err := r.pool.Query(ctx, `
		SELECT COALESCE(ps.id::text,'none'), COALESCE(ps.name,'Unstaged'), COUNT(*)::float8
		FROM deals d
		LEFT JOIN pipeline_stages ps ON ps.id = d.stage_id
		WHERE d.status = 'open'
		GROUP BY ps.id, ps.name, ps.position
		ORDER BY COALESCE(ps.position, 999)
	`)
	if err != nil {
		return nil, err
	}
	out.PipelineOverview.DealsByStage, err = scanLabeledMetricRows(rows4)
	if err != nil {
		return nil, err
	}

	lb2 := &sqlBuilder{}
	lb2.applyCommonLead("l", f)
	if err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*)::int,
			COUNT(*) FILTER (WHERE l.status = 'converted'
				OR EXISTS (
					SELECT 1 FROM pipeline_stages ps
					WHERE ps.id = l.stage_id AND (ps.is_won = TRUE OR lower(ps.name) LIKE '%qualif%')
				))::int
		FROM leads l WHERE `+lb2.sql(), lb2.args...).Scan(
		&out.ConversionOverview.Leads, &out.ConversionOverview.QualifiedLeads,
	); err != nil {
		return nil, err
	}

	db2 := &sqlBuilder{}
	db2.add("d.created_at >= ?", f.From)
	db2.add("d.created_at <= ?", f.To)
	if f.PipelineID != "" {
		db2.add("d.pipeline_id::text = ?", f.PipelineID)
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*)::int,
			COUNT(*) FILTER (WHERE d.status = 'won' OR EXISTS (
				SELECT 1 FROM pipeline_stages ps WHERE ps.id = d.stage_id AND ps.is_won = TRUE
			))::int
		FROM deals d WHERE `+db2.sql(), db2.args...).Scan(
		&out.ConversionOverview.Deals, &out.ConversionOverview.Conversions,
	); err != nil {
		return nil, err
	}
	out.ConversionOverview.ConversionRate = round2(pct(out.ConversionOverview.Conversions, out.ConversionOverview.Leads))

	refWhere := `r.created_at >= $1 AND r.created_at <= $2`
	refArgs := []any{f.From, f.To}
	if err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*)::int,
			COUNT(*) FILTER (WHERE rs.code = 'active')::int,
			COUNT(*) FILTER (WHERE rs.is_converted = TRUE)::int,
			COALESCE(SUM(COALESCE(c.potential_value, l.potential_value)) FILTER (
				WHERE rs.is_converted = FALSE AND rs.code <> 'cancelled'
			), 0)::float8
		FROM referrals r
		JOIN referral_statuses rs ON rs.id = r.status_id
		LEFT JOIN leads l ON l.id = r.lead_id
		LEFT JOIN customers c ON c.id = r.customer_id
		WHERE `+refWhere, refArgs...).Scan(
		&out.ReferralOverview.Total, &out.ReferralOverview.Active,
		&out.ReferralOverview.Converted, &out.ReferralOverview.PipelineValue,
	); err != nil {
		return nil, err
	}

	refTrendSQL := fmt.Sprintf(`
		SELECT to_char(date_trunc('%s', r.created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD'), COUNT(*)::float8
		FROM referrals r
		WHERE r.created_at >= $1 AND r.created_at <= $2
		GROUP BY 1 ORDER BY 1 ASC`, trunc)
	rows5, err := r.pool.Query(ctx, refTrendSQL, f.From, f.To)
	if err != nil {
		return nil, err
	}
	out.ReferralOverview.Trend, err = scanMetricRows(rows5)
	if err != nil {
		return nil, err
	}

	return out, nil
}

func (r *Repository) scanGrowth(ctx context.Context, sql string, from, to time.Time, dest *[]MetricPoint) error {
	rows, err := r.pool.Query(ctx, sql, from, to)
	if err != nil {
		return err
	}
	*dest, err = scanMetricRows(rows)
	return err
}

func scanLabeledMetricRows(rows interface {
	Next() bool
	Scan(dest ...any) error
	Close()
	Err() error
}) ([]MetricPoint, error) {
	defer rows.Close()
	var out []MetricPoint
	for rows.Next() {
		var p MetricPoint
		if err := rows.Scan(&p.Key, &p.Label, &p.Value); err != nil {
			return nil, err
		}
		if p.Label == "" {
			p.Label = p.Key
		}
		out = append(out, p)
	}
	if out == nil {
		out = []MetricPoint{}
	}
	return out, rows.Err()
}

func scanMetricRows(rows interface {
	Next() bool
	Scan(dest ...any) error
	Close()
	Err() error
}) ([]MetricPoint, error) {
	defer rows.Close()
	var out []MetricPoint
	for rows.Next() {
		var p MetricPoint
		if err := rows.Scan(&p.Key, &p.Value); err != nil {
			return nil, err
		}
		p.Label = p.Key
		out = append(out, p)
	}
	if out == nil {
		out = []MetricPoint{}
	}
	return out, rows.Err()
}
