package analytics

import (
	"context"
	"fmt"
	"time"
)

type OrganizationTopMetrics struct {
	TotalUsers          int     `json:"totalUsers"`
	ActiveUsers         int     `json:"activeUsers"`
	TotalTeams          int     `json:"totalTeams"`
	TotalLeads          int     `json:"totalLeads"`
	TotalCustomers      int     `json:"totalCustomers"`
	TotalDeals          int     `json:"totalDeals"`
	ActiveDeals         int     `json:"activeDeals"`
	CompletedActivities int     `json:"completedActivities"`
	PipelineValue       float64 `json:"pipelineValue"`
}

type UserAdoptionMetrics struct {
	ActiveUsers               int `json:"activeUsers"`
	UsersWithActivity         int `json:"usersWithActivity"`
	UsersWithNoRecentActivity int `json:"usersWithNoRecentActivity"`
	ActivitiesCreated         int `json:"activitiesCreated"`
	ActivitiesCompleted       int `json:"activitiesCompleted"`
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
	Leads          int     `json:"leads"`
	QualifiedLeads int     `json:"qualifiedLeads"`
	Deals          int     `json:"deals"`
	Conversions    int     `json:"conversions"`
	ConversionRate float64 `json:"conversionRate"`
}

type ReferralOverview struct {
	Total         int           `json:"total"`
	Active        int           `json:"active"`
	Converted     int           `json:"converted"`
	PipelineValue float64       `json:"pipelineValue"`
	Trend         []MetricPoint `json:"trend"`
}

type OrganizationAnalytics struct {
	TopMetrics         OrganizationTopMetrics `json:"topMetrics"`
	UserAdoption       UserAdoptionMetrics    `json:"userAdoption"`
	Growth             GrowthSeries           `json:"growth"`
	PipelineOverview   OrgPipelineOverview    `json:"pipelineOverview"`
	ConversionOverview OrgConversionOverview  `json:"conversionOverview"`
	ReferralOverview   ReferralOverview       `json:"referralOverview"`
	From               time.Time              `json:"from"`
	To                 time.Time              `json:"to"`
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

// recordDims narrows a leads, customers or deals alias by pipeline, team and owner.
func (b *sqlBuilder) recordDims(alias string, f Filter) {
	if f.PipelineID != "" {
		b.add(alias+".pipeline_id::text = ?", f.PipelineID)
	}
	if f.TeamID != "" {
		b.add(alias+".team_id::text = ?", f.TeamID)
	}
	if f.OwnerUserID != "" {
		b.add(alias+".owner_user_id::text = ?", f.OwnerUserID)
	}
}

// Activities carry no team or pipeline; both come from the linked deal, lead or customer.
const (
	activityTeamSQL = `(EXISTS (SELECT 1 FROM deals d WHERE d.id = a.deal_id AND d.team_id::text = ?)
		OR EXISTS (SELECT 1 FROM leads l WHERE l.id = a.lead_id AND l.team_id::text = ?)
		OR EXISTS (SELECT 1 FROM customers c WHERE c.id = a.customer_id AND c.team_id::text = ?))`
	activityPipelineSQL = `(EXISTS (SELECT 1 FROM deals d WHERE d.id = a.deal_id AND d.pipeline_id::text = ?)
		OR EXISTS (SELECT 1 FROM leads l WHERE l.id = a.lead_id AND l.pipeline_id::text = ?)
		OR EXISTS (SELECT 1 FROM customers c WHERE c.id = a.customer_id AND c.pipeline_id::text = ?))`
)

// activityDims narrows activities (alias "a") by owner, team and pipeline.
func (b *sqlBuilder) activityDims(f Filter) {
	if f.OwnerUserID != "" {
		b.add("COALESCE(a.owner_user_id, a.actor_user_id)::text = ?", f.OwnerUserID)
	}
	if f.TeamID != "" {
		b.addAll(activityTeamSQL, f.TeamID, f.TeamID, f.TeamID)
	}
	if f.PipelineID != "" {
		b.addAll(activityPipelineSQL, f.PipelineID, f.PipelineID, f.PipelineID)
	}
}

// userDims narrows users (alias "u") to the selected team's members and/or the selected owner.
func (b *sqlBuilder) userDims(f Filter) {
	if f.TeamID != "" {
		b.add("EXISTS (SELECT 1 FROM team_members tm WHERE tm.user_id = u.id AND tm.team_id::text = ?)", f.TeamID)
	}
	if f.OwnerUserID != "" {
		b.add("u.id::text = ?", f.OwnerUserID)
	}
}

// referralDims narrows referrals through the lead or customer they point to.
func (b *sqlBuilder) referralDims(f Filter) {
	if f.PipelineID != "" {
		b.add("COALESCE(c.pipeline_id, l.pipeline_id)::text = ?", f.PipelineID)
	}
	if f.TeamID != "" {
		b.add("COALESCE(c.team_id, l.team_id)::text = ?", f.TeamID)
	}
	if f.OwnerUserID != "" {
		b.add("COALESCE(c.owner_user_id, l.owner_user_id)::text = ?", f.OwnerUserID)
	}
}

func (b *sqlBuilder) inRange(column string, f Filter) {
	b.add(column+" >= ?", f.From)
	b.add(column+" <= ?", f.To)
}

// Organization reports totals, adoption, growth, pipeline, conversion and referrals.
// Totals are current snapshots narrowed by pipeline/team/owner; everything else is
// additionally limited to the From–To range.
func (r *Repository) Organization(ctx context.Context, f Filter) (*OrganizationAnalytics, error) {
	out := &OrganizationAnalytics{From: f.From, To: f.To}
	if err := r.orgTopMetrics(ctx, f, &out.TopMetrics); err != nil {
		return nil, err
	}
	if err := r.orgAdoption(ctx, f, &out.UserAdoption); err != nil {
		return nil, err
	}
	if err := r.orgGrowth(ctx, f, &out.Growth); err != nil {
		return nil, err
	}
	if err := r.orgPipeline(ctx, f, &out.PipelineOverview); err != nil {
		return nil, err
	}
	if err := r.orgConversion(ctx, f, &out.ConversionOverview); err != nil {
		return nil, err
	}
	if err := r.orgReferrals(ctx, f, &out.ReferralOverview); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) orgTopMetrics(ctx context.Context, f Filter, m *OrganizationTopMetrics) error {
	ub := &sqlBuilder{}
	ub.userDims(f)
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int, COUNT(*) FILTER (WHERE u.is_active)::int
		FROM users u WHERE `+ub.sql(), ub.args...).Scan(&m.TotalUsers, &m.ActiveUsers); err != nil {
		return err
	}

	tb := &sqlBuilder{}
	tb.add("t.is_active = ?", true)
	if f.TeamID != "" {
		tb.add("t.id::text = ?", f.TeamID)
	}
	if f.OwnerUserID != "" {
		tb.add("EXISTS (SELECT 1 FROM team_members tm WHERE tm.team_id = t.id AND tm.user_id::text = ?)", f.OwnerUserID)
	}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM teams t WHERE `+tb.sql(), tb.args...).
		Scan(&m.TotalTeams); err != nil {
		return err
	}

	lb := &sqlBuilder{}
	lb.add("l.is_archived = ?", false)
	lb.recordDims("l", f)
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM leads l WHERE `+lb.sql(), lb.args...).
		Scan(&m.TotalLeads); err != nil {
		return err
	}

	cb := &sqlBuilder{}
	cb.add("c.is_archived = ?", false)
	cb.recordDims("c", f)
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM customers c WHERE `+cb.sql(), cb.args...).
		Scan(&m.TotalCustomers); err != nil {
		return err
	}

	db := &sqlBuilder{}
	db.recordDims("d", f)
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int,
			COUNT(*) FILTER (WHERE d.status = 'open')::int,
			COALESCE(SUM(d.value) FILTER (WHERE d.status = 'open'), 0)::float8
		FROM deals d WHERE `+db.sql(), db.args...).Scan(&m.TotalDeals, &m.ActiveDeals, &m.PipelineValue); err != nil {
		return err
	}

	ab := &sqlBuilder{}
	ab.add("a.status = ?", "completed")
	ab.activityDims(f)
	return r.pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM activities a WHERE `+ab.sql(), ab.args...).
		Scan(&m.CompletedActivities)
}

func (r *Repository) orgAdoption(ctx context.Context, f Filter, m *UserAdoptionMetrics) error {
	ab := &sqlBuilder{}
	ab.inRange("a.created_at", f)
	ab.activityDims(f)
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int, COUNT(*) FILTER (WHERE a.status = 'completed')::int
		FROM activities a WHERE `+ab.sql(), ab.args...).Scan(&m.ActivitiesCreated, &m.ActivitiesCompleted); err != nil {
		return err
	}

	// Both counts use the same user set so "no recent activity" can't go negative.
	activity := `EXISTS (SELECT 1 FROM activities a
		WHERE COALESCE(a.owner_user_id, a.actor_user_id) = u.id AND a.created_at >= ? AND a.created_at <= ?`
	vals := []any{f.From, f.To}
	if f.PipelineID != "" {
		activity += " AND " + activityPipelineSQL
		vals = append(vals, f.PipelineID, f.PipelineID, f.PipelineID)
	}
	activity += ")"

	ub := &sqlBuilder{}
	ub.add("u.is_active = ?", true)
	ub.userDims(f)
	withActivity := &sqlBuilder{args: append([]any{}, ub.args...), where: append([]string{}, ub.where...)}
	withActivity.addAll(activity, vals...)

	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM users u WHERE `+ub.sql(), ub.args...).
		Scan(&m.ActiveUsers); err != nil {
		return err
	}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM users u WHERE `+withActivity.sql(), withActivity.args...).
		Scan(&m.UsersWithActivity); err != nil {
		return err
	}
	m.UsersWithNoRecentActivity = max(m.ActiveUsers-m.UsersWithActivity, 0)
	return nil
}

func (r *Repository) orgGrowth(ctx context.Context, f Filter, g *GrowthSeries) error {
	trunc := growthTrunc(f)
	series := func(table, alias string, b *sqlBuilder, dest *[]MetricPoint) error {
		q := fmt.Sprintf(`
			SELECT to_char(date_trunc('%s', %s.created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD'), COUNT(*)::float8
			FROM %s %s WHERE %s GROUP BY 1 ORDER BY 1 ASC`, trunc, alias, table, alias, b.sql())
		rows, err := r.pool.Query(ctx, q, b.args...)
		if err != nil {
			return err
		}
		*dest, err = scanMetricRows(rows)
		return err
	}

	lb := &sqlBuilder{}
	lb.inRange("l.created_at", f)
	lb.add("l.is_archived = ?", false)
	lb.recordDims("l", f)
	if err := series("leads", "l", lb, &g.Leads); err != nil {
		return err
	}

	cb := &sqlBuilder{}
	cb.inRange("c.created_at", f)
	cb.add("c.is_archived = ?", false)
	cb.recordDims("c", f)
	if err := series("customers", "c", cb, &g.Customers); err != nil {
		return err
	}

	db := &sqlBuilder{}
	db.inRange("d.created_at", f)
	db.recordDims("d", f)
	if err := series("deals", "d", db, &g.Deals); err != nil {
		return err
	}

	ab := &sqlBuilder{}
	ab.inRange("a.created_at", f)
	ab.activityDims(f)
	return series("activities", "a", ab, &g.Activities)
}

func (r *Repository) orgPipeline(ctx context.Context, f Filter, p *OrgPipelineOverview) error {
	pb := &sqlBuilder{}
	pb.add("d.status = ?", "open")
	pb.recordDims("d", f)
	if err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(d.value),0)::float8, COALESCE(AVG(d.value),0)::float8
		FROM deals d WHERE `+pb.sql(), pb.args...).Scan(&p.ActivePipelineValue, &p.AvgDealValue); err != nil {
		return err
	}
	p.AvgDealValue = round2(p.AvgDealValue)

	rows, err := r.pool.Query(ctx, `
		SELECT COALESCE(p.id::text,'none'), COALESCE(p.name,'No pipeline'), COUNT(*)::float8
		FROM deals d
		LEFT JOIN pipelines p ON p.id = d.pipeline_id
		WHERE `+pb.sql()+`
		GROUP BY p.id, p.name
		ORDER BY 3 DESC`, pb.args...)
	if err != nil {
		return err
	}
	if p.DealsByPipeline, err = scanLabeledMetricRows(rows); err != nil {
		return err
	}

	rows, err = r.pool.Query(ctx, `
		SELECT COALESCE(ps.id::text,'none'), COALESCE(ps.name,'Unstaged'), COUNT(*)::float8
		FROM deals d
		LEFT JOIN pipeline_stages ps ON ps.id = d.stage_id
		WHERE `+pb.sql()+`
		GROUP BY ps.id, ps.name, ps.position
		ORDER BY COALESCE(ps.position, 999)`, pb.args...)
	if err != nil {
		return err
	}
	p.DealsByStage, err = scanLabeledMetricRows(rows)
	return err
}

func (r *Repository) orgConversion(ctx context.Context, f Filter, c *OrgConversionOverview) error {
	lb := &sqlBuilder{}
	lb.applyCommonLead("l", f)
	if err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*)::int,
			COUNT(*) FILTER (WHERE l.status = 'converted'
				OR EXISTS (
					SELECT 1 FROM pipeline_stages ps
					WHERE ps.id = l.stage_id AND (ps.is_won = TRUE OR lower(ps.name) LIKE '%qualif%')
				))::int
		FROM leads l WHERE `+lb.sql(), lb.args...).Scan(&c.Leads, &c.QualifiedLeads); err != nil {
		return err
	}

	db := &sqlBuilder{}
	db.inRange("d.created_at", f)
	db.recordDims("d", f)
	if err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*)::int,
			COUNT(*) FILTER (WHERE d.status = 'won' OR EXISTS (
				SELECT 1 FROM pipeline_stages ps WHERE ps.id = d.stage_id AND ps.is_won = TRUE
			))::int
		FROM deals d WHERE `+db.sql(), db.args...).Scan(&c.Deals, &c.Conversions); err != nil {
		return err
	}
	c.ConversionRate = round2(pct(c.Conversions, c.Leads))
	return nil
}

func (r *Repository) orgReferrals(ctx context.Context, f Filter, ro *ReferralOverview) error {
	const from = `
		FROM referrals r
		JOIN referral_statuses rs ON rs.id = r.status_id
		LEFT JOIN leads l ON l.id = r.lead_id
		LEFT JOIN customers c ON c.id = r.customer_id`
	rb := &sqlBuilder{}
	rb.inRange("r.created_at", f)
	rb.referralDims(f)
	if err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*)::int,
			COUNT(*) FILTER (WHERE rs.code = 'active')::int,
			COUNT(*) FILTER (WHERE rs.is_converted = TRUE)::int,
			COALESCE(SUM(COALESCE(c.potential_value, l.potential_value)) FILTER (
				WHERE rs.is_converted = FALSE AND rs.code <> 'cancelled'
			), 0)::float8`+from+` WHERE `+rb.sql(), rb.args...).Scan(
		&ro.Total, &ro.Active, &ro.Converted, &ro.PipelineValue,
	); err != nil {
		return err
	}

	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT to_char(date_trunc('%s', r.created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD'), COUNT(*)::float8`,
		growthTrunc(f))+from+` WHERE `+rb.sql()+` GROUP BY 1 ORDER BY 1 ASC`, rb.args...)
	if err != nil {
		return err
	}
	ro.Trend, err = scanMetricRows(rows)
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
