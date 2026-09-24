package analytics

import (
	"context"
	"fmt"
	"strings"
)

func (r *Repository) LeadAnalytics(ctx context.Context, f Filter) (*LeadAnalytics, error) {
	out := &LeadAnalytics{
		Question: "How many leads entered the CRM in this period, and how many progressed to qualified/converted?",
		BySource: []MetricPoint{}, ByDay: []MetricPoint{},
	}
	b := &sqlBuilder{}
	b.applyCommonLead("l", f)
	q := `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE l.status IN ('qualified','converted')),
			COUNT(*) FILTER (WHERE l.status = 'converted')
		FROM leads l WHERE ` + b.sql()
	if err := r.pool.QueryRow(ctx, q, b.args...).Scan(&out.LeadVolume, &out.QualifiedLeads, &out.ConvertedLeads); err != nil {
		return nil, err
	}
	out.ConversionRate = round2(pct(out.ConvertedLeads, out.LeadVolume))

	// Avg response: first activity after lead create
	b2 := &sqlBuilder{}
	b2.applyCommonLead("l", f)
	var avg *float64
	err := r.pool.QueryRow(ctx, `
		SELECT AVG(EXTRACT(EPOCH FROM (a.first_at - l.created_at)))::float8
		FROM leads l
		JOIN LATERAL (
			SELECT MIN(created_at) AS first_at FROM activities
			WHERE lead_id = l.id OR customer_id IN (
				SELECT id FROM customers WHERE converted_from_lead_id = l.id
			)
		) a ON a.first_at IS NOT NULL
		WHERE `+b2.sql(), b2.args...).Scan(&avg)
	if err != nil {
		return nil, err
	}
	out.AvgResponseSecs = avg

	b3 := &sqlBuilder{}
	b3.applyCommonLead("l", f)
	rows, err := r.pool.Query(ctx, `
		SELECT COALESCE(NULLIF(l.source,''),'(none)'), COUNT(*)
		FROM leads l WHERE `+b3.sql()+`
		GROUP BY 1 ORDER BY 2 DESC LIMIT 20
	`, b3.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p MetricPoint
		if err := rows.Scan(&p.Key, &p.Value); err != nil {
			return nil, err
		}
		p.Label = p.Key
		out.BySource = append(out.BySource, p)
	}

	b4 := &sqlBuilder{}
	b4.applyCommonLead("l", f)
	rows2, err := r.pool.Query(ctx, `
		SELECT to_char(date_trunc('day', l.created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD'), COUNT(*)
		FROM leads l WHERE `+b4.sql()+`
		GROUP BY 1 ORDER BY 1 ASC
	`, b4.args...)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var p MetricPoint
		if err := rows2.Scan(&p.Key, &p.Value); err != nil {
			return nil, err
		}
		p.Label = p.Key
		out.ByDay = append(out.ByDay, p)
	}
	return out, rows2.Err()
}

func (r *Repository) PipelineAnalytics(ctx context.Context, f Filter) (*PipelineAnalytics, error) {
	out := &PipelineAnalytics{
		Question: "What is the current open pipeline value and how is it distributed across stages?",
		ByStage:  []MetricPoint{},
	}
	b := &sqlBuilder{}
	b.add("d.created_at >= ?", f.From)
	b.add("d.created_at <= ?", f.To)
	if f.PipelineID != "" {
		b.add("d.pipeline_id::text = ?", f.PipelineID)
	}
	if f.TeamID != "" {
		b.add("d.team_id::text = ?", f.TeamID)
	}
	if f.OwnerUserID != "" {
		b.add("d.owner_user_id::text = ?", f.OwnerUserID)
	}
	if f.Source != "" {
		b.add("d.source = ?", f.Source)
	}
	if f.AnzscoID != "" {
		b.add("c.anzsco_id::text = ?", f.AnzscoID)
	}
	q := `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE d.status='open'),
			COUNT(*) FILTER (WHERE d.status='won'),
			COUNT(*) FILTER (WHERE d.status='lost'),
			COALESCE(SUM(d.value) FILTER (WHERE d.status='open'),0),
			COALESCE(AVG(d.value),0)
		FROM deals d
		LEFT JOIN customers c ON c.id = d.customer_id
		WHERE ` + b.sql()
	if err := r.pool.QueryRow(ctx, q, b.args...).Scan(
		&out.Deals, &out.OpenDeals, &out.WonDeals, &out.LostDeals, &out.PipelineValue, &out.AvgDealValue,
	); err != nil {
		return nil, err
	}

	var avgStage *float64
	args := []any{f.From, f.To}
	where := "t.exited_at >= $1 AND t.exited_at <= $2 AND t.from_stage_id IS NOT NULL AND t.duration_seconds > 0"
	n := 2
	joinExtra := ""
	if f.PipelineID != "" {
		n++
		args = append(args, f.PipelineID)
		where += fmt.Sprintf(" AND t.from_pipeline_id::text = $%d", n)
	}
	if f.TeamID != "" || f.OwnerUserID != "" || f.Source != "" || f.AnzscoID != "" {
		joinExtra = " JOIN deals d ON d.id = t.deal_id LEFT JOIN customers c ON c.id = d.customer_id "
		if f.TeamID != "" {
			n++
			args = append(args, f.TeamID)
			where += fmt.Sprintf(" AND d.team_id::text = $%d", n)
		}
		if f.OwnerUserID != "" {
			n++
			args = append(args, f.OwnerUserID)
			where += fmt.Sprintf(" AND d.owner_user_id::text = $%d", n)
		}
		if f.Source != "" {
			n++
			args = append(args, f.Source)
			where += fmt.Sprintf(" AND d.source = $%d", n)
		}
		if f.AnzscoID != "" {
			n++
			args = append(args, f.AnzscoID)
			where += fmt.Sprintf(" AND c.anzsco_id::text = $%d", n)
		}
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT AVG(t.duration_seconds)::float8
		FROM deal_stage_transitions t`+joinExtra+` WHERE `+where, args...).Scan(&avgStage); err != nil {
		return nil, err
	}
	out.AvgStageDuration = avgStage

	b3 := &sqlBuilder{}
	if f.PipelineID != "" {
		b3.add("d.pipeline_id::text = ?", f.PipelineID)
	}
	b3.add("d.status = ?", "open")
	if f.TeamID != "" {
		b3.add("d.team_id::text = ?", f.TeamID)
	}
	if f.OwnerUserID != "" {
		b3.add("d.owner_user_id::text = ?", f.OwnerUserID)
	}
	if f.Source != "" {
		b3.add("d.source = ?", f.Source)
	}
	if f.AnzscoID != "" {
		b3.add("c.anzsco_id::text = ?", f.AnzscoID)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT COALESCE(ps.id::text,'(none)'), COALESCE(ps.name,'Unstaged'), COALESCE(SUM(d.value),0)
		FROM deals d
		LEFT JOIN pipeline_stages ps ON ps.id = d.stage_id
		LEFT JOIN customers c ON c.id = d.customer_id
		WHERE `+b3.sql()+`
		GROUP BY ps.id, ps.name, ps.position
		ORDER BY COALESCE(ps.position, 999), ps.name
	`, b3.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p MetricPoint
		if err := rows.Scan(&p.Key, &p.Label, &p.Value); err != nil {
			return nil, err
		}
		out.ByStage = append(out.ByStage, p)
	}
	return out, rows.Err()
}

func (r *Repository) ActivityAnalytics(ctx context.Context, f Filter) (*ActivityAnalytics, error) {
	out := &ActivityAnalytics{
		Question: "Which activity types are teams executing, and is follow-up volume healthy?",
		ByType:   []MetricPoint{}, ByDay: []MetricPoint{},
	}
	b := &sqlBuilder{}
	b.add("a.created_at >= ?", f.From)
	b.add("a.created_at <= ?", f.To)
	if f.OwnerUserID != "" {
		b.add("COALESCE(a.owner_user_id, a.actor_user_id)::text = ?", f.OwnerUserID)
	}
	extra := ""
	if f.TeamID != "" {
		b.args = append(b.args, f.TeamID, f.TeamID, f.TeamID)
		i := len(b.args)
		extra = fmt.Sprintf(` AND (
			EXISTS (SELECT 1 FROM deals d WHERE d.id = a.deal_id AND d.team_id::text = $%d)
			OR EXISTS (SELECT 1 FROM leads l WHERE l.id = a.lead_id AND l.team_id::text = $%d)
			OR EXISTS (SELECT 1 FROM customers c WHERE c.id = a.customer_id AND c.team_id::text = $%d)
		)`, i-2, i-1, i)
	}
	if f.PipelineID != "" {
		b.args = append(b.args, f.PipelineID, f.PipelineID)
		i := len(b.args)
		extra += fmt.Sprintf(` AND (
			EXISTS (SELECT 1 FROM deals d WHERE d.id = a.deal_id AND d.pipeline_id::text = $%d)
			OR EXISTS (SELECT 1 FROM leads l WHERE l.id = a.lead_id AND l.pipeline_id::text = $%d)
		)`, i-1, i)
	}

	where := b.sql() + extra
	if err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE a.status='completed'),
			COUNT(*) FILTER (WHERE a.status='overdue' OR (a.status IN ('upcoming','due','planned') AND a.due_at < NOW()))
		FROM activities a WHERE `+where, b.args...).Scan(&out.Activities, &out.Completed, &out.Overdue); err != nil {
		return nil, err
	}

	var dealCount int
	db := &sqlBuilder{}
	db.add("d.created_at >= ?", f.From)
	db.add("d.created_at <= ?", f.To)
	if f.PipelineID != "" {
		db.add("d.pipeline_id::text = ?", f.PipelineID)
	}
	if f.TeamID != "" {
		db.add("d.team_id::text = ?", f.TeamID)
	}
	if f.OwnerUserID != "" {
		db.add("d.owner_user_id::text = ?", f.OwnerUserID)
	}
	_ = r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM deals d WHERE `+db.sql(), db.args...).Scan(&dealCount)
	out.AvgPerDeal = round2(div(float64(out.Activities), float64(max(dealCount, 1))))

	rows, err := r.pool.Query(ctx, `
		SELECT a.kind, COUNT(*) FROM activities a WHERE `+where+`
		GROUP BY a.kind ORDER BY 2 DESC LIMIT 20
	`, b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p MetricPoint
		if err := rows.Scan(&p.Key, &p.Value); err != nil {
			return nil, err
		}
		p.Label = p.Key
		out.ByType = append(out.ByType, p)
	}

	rows2, err := r.pool.Query(ctx, `
		SELECT to_char(date_trunc('day', a.created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD'), COUNT(*)
		FROM activities a WHERE `+where+`
		GROUP BY 1 ORDER BY 1 ASC
	`, b.args...)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var p MetricPoint
		if err := rows2.Scan(&p.Key, &p.Value); err != nil {
			return nil, err
		}
		p.Label = p.Key
		out.ByDay = append(out.ByDay, p)
	}
	return out, rows2.Err()
}

func (r *Repository) ConversionAnalytics(ctx context.Context, f Filter) (*ConversionAnalytics, error) {
	out := &ConversionAnalytics{
		Question: "Where do opportunities convert or stall from lead to won deal?",
		Funnel:   []MetricPoint{},
	}
	lb := &sqlBuilder{}
	lb.applyCommonLead("l", f)
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE l.status='converted')
		FROM leads l WHERE `+lb.sql(), lb.args...).Scan(&out.LeadsCreated, &out.LeadsConverted); err != nil {
		return nil, err
	}
	out.LeadToCustomerRate = round2(pct(out.LeadsConverted, out.LeadsCreated))

	db := &sqlBuilder{}
	db.add("d.created_at >= ?", f.From)
	db.add("d.created_at <= ?", f.To)
	if f.PipelineID != "" {
		db.add("d.pipeline_id::text = ?", f.PipelineID)
	}
	if f.TeamID != "" {
		db.add("d.team_id::text = ?", f.TeamID)
	}
	if f.OwnerUserID != "" {
		db.add("d.owner_user_id::text = ?", f.OwnerUserID)
	}
	if f.Source != "" {
		db.add("d.source = ?", f.Source)
	}
	if f.AnzscoID != "" {
		db.add("c.anzsco_id::text = ?", f.AnzscoID)
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE d.status='won'),
			COUNT(*) FILTER (WHERE d.status='lost'),
			COUNT(*) FILTER (WHERE d.status='won'),
			COUNT(*) FILTER (
				WHERE EXISTS (
					SELECT 1 FROM deal_stage_transitions t
					JOIN pipeline_stages ps ON ps.id = t.to_stage_id
					WHERE t.deal_id = d.id
					  AND (lower(ps.name) LIKE '%negotiat%' OR lower(ps.name) LIKE '%submit%' OR lower(ps.name) LIKE '%proposal%')
				)
			)
		FROM deals d
		LEFT JOIN customers c ON c.id = d.customer_id
		WHERE `+db.sql(), db.args...).Scan(
		&out.DealsCreated, &out.DealsWon, &out.DealsLost, &out.PositiveOutcomes, &out.Submissions,
	); err != nil {
		return nil, err
	}
	out.DealWinRate = round2(pct(out.DealsWon, out.DealsWon+out.DealsLost))

	out.Funnel = []MetricPoint{
		{Key: "leads", Label: "Leads created", Value: float64(out.LeadsCreated)},
		{Key: "converted", Label: "Converted to customers", Value: float64(out.LeadsConverted)},
		{Key: "deals", Label: "Deals created", Value: float64(out.DealsCreated)},
		{Key: "submissions", Label: "Reached proposal/negotiation", Value: float64(out.Submissions)},
		{Key: "won", Label: "Won deals", Value: float64(out.DealsWon)},
	}
	return out, nil
}

func (r *Repository) TeamAnalytics(ctx context.Context, f Filter) (*TeamAnalytics, error) {
	out := &TeamAnalytics{
		GroupBy:  f.GroupBy,
		SortBy:   f.SortBy,
		Rows:     []TeamRow{},
		Question: "How do teams and owners compare on volume, pipeline value, and conversion for this period?",
	}
	sortCol := map[string]string{
		"leadVolume": "lead_volume", "deals": "deals", "wonDeals": "won_deals",
		"activities": "activities", "pipelineValue": "pipeline_value",
		"conversionRate": "conversion_rate", "averageStageDurationSeconds": "avg_stage",
	}
	orderExpr := sortCol[f.SortBy]
	if orderExpr == "" {
		orderExpr = "pipeline_value"
		out.SortBy = "pipelineValue"
	}
	dir := "DESC"
	if f.SortDir == "asc" {
		dir = "ASC"
	}

	switch f.GroupBy {
	case "team":
		q := fmt.Sprintf(`
			WITH base AS (
				SELECT
					COALESCE(t.id::text, 'none') AS entity_id,
					COALESCE(t.name, 'Unassigned team') AS entity_name,
					(SELECT COUNT(*) FROM leads l WHERE COALESCE(l.team_id::text,'none') = COALESCE(t.id::text,'none')
						AND l.created_at >= $1 AND l.created_at <= $2 AND l.is_archived=FALSE
						AND ($3 = '' OR l.pipeline_id::text = $3)
						AND ($4 = '' OR l.source = $4)
						AND ($5 = '' OR l.anzsco_id::text = $5)
						AND ($6 = '' OR l.owner_user_id::text = $6)
					) AS lead_volume,
					COUNT(d.*) AS deals,
					COUNT(*) FILTER (WHERE d.status='won') AS won_deals,
					COALESCE(SUM(d.value) FILTER (WHERE d.status='open'),0) AS pipeline_value,
					(SELECT COUNT(*) FROM activities a
						WHERE a.created_at >= $1 AND a.created_at <= $2
						  AND (
							EXISTS (SELECT 1 FROM deals dx WHERE dx.id=a.deal_id AND COALESCE(dx.team_id::text,'none')=COALESCE(t.id::text,'none'))
							OR EXISTS (SELECT 1 FROM leads lx WHERE lx.id=a.lead_id AND COALESCE(lx.team_id::text,'none')=COALESCE(t.id::text,'none'))
						  )
					) AS activities,
					(SELECT AVG(tr.duration_seconds)::float8 FROM deal_stage_transitions tr
						JOIN deals dx ON dx.id = tr.deal_id
						WHERE COALESCE(dx.team_id::text,'none') = COALESCE(t.id::text,'none')
						  AND tr.from_stage_id IS NOT NULL AND tr.duration_seconds > 0
						  AND tr.exited_at >= $1 AND tr.exited_at <= $2
						  AND ($3 = '' OR dx.pipeline_id::text = $3)
					) AS avg_stage
				FROM deals d
				FULL OUTER JOIN teams t ON t.id = d.team_id
				WHERE (d.id IS NULL OR (
					d.created_at >= $1 AND d.created_at <= $2
					AND ($3 = '' OR d.pipeline_id::text = $3)
					AND ($4 = '' OR d.source = $4)
					AND ($6 = '' OR d.owner_user_id::text = $6)
					AND ($5 = '' OR EXISTS (SELECT 1 FROM customers c WHERE c.id=d.customer_id AND c.anzsco_id::text=$5))
				))
				GROUP BY t.id, t.name
			)
			SELECT entity_id, entity_name, lead_volume, deals, won_deals, activities, pipeline_value,
				CASE WHEN (won_deals + (SELECT COUNT(*) FROM deals dx WHERE dx.status='lost' AND COALESCE(dx.team_id::text,'none')=entity_id
					AND dx.created_at >= $1 AND dx.created_at <= $2)) > 0
				THEN won_deals::float8 * 100 / NULLIF((won_deals + (SELECT COUNT(*) FROM deals dx WHERE dx.status='lost'
					AND COALESCE(dx.team_id::text,'none')=entity_id AND dx.created_at >= $1 AND dx.created_at <= $2)),0)
				ELSE 0 END AS conversion_rate,
				avg_stage
			FROM base
			ORDER BY %s %s NULLS LAST
			LIMIT 50
		`, orderExpr, dir)
		rows, err := r.pool.Query(ctx, q, f.From, f.To, f.PipelineID, f.Source, f.AnzscoID, f.OwnerUserID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var row TeamRow
			row.EntityType = "team"
			if err := rows.Scan(&row.EntityID, &row.EntityName, &row.LeadVolume, &row.Deals, &row.WonDeals,
				&row.Activities, &row.PipelineValue, &row.ConversionRate, &row.AvgStageDuration); err != nil {
				return nil, err
			}
			row.ConversionRate = round2(row.ConversionRate)
			out.Rows = append(out.Rows, row)
		}
		return out, rows.Err()

	case "period":
		trunc := "month"
		switch f.Period {
		case "day":
			trunc = "day"
		case "week":
			trunc = "week"
		}
		q := fmt.Sprintf(`
			SELECT to_char(date_trunc('%s', d.created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD') AS entity_id,
				to_char(date_trunc('%s', d.created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD') AS entity_name,
				0, COUNT(*), COUNT(*) FILTER (WHERE d.status='won'),
				0, COALESCE(SUM(d.value) FILTER (WHERE d.status='open'),0),
				CASE WHEN COUNT(*) FILTER (WHERE d.status IN ('won','lost')) > 0
					THEN COUNT(*) FILTER (WHERE d.status='won')::float8 * 100
						/ COUNT(*) FILTER (WHERE d.status IN ('won','lost'))
					ELSE 0 END,
				NULL::float8
			FROM deals d
			LEFT JOIN customers c ON c.id = d.customer_id
			WHERE d.created_at >= $1 AND d.created_at <= $2
			  AND ($3 = '' OR d.pipeline_id::text = $3)
			  AND ($4 = '' OR d.team_id::text = $4)
			  AND ($5 = '' OR d.owner_user_id::text = $5)
			  AND ($6 = '' OR d.source = $6)
			  AND ($7 = '' OR c.anzsco_id::text = $7)
			GROUP BY 1, 2
			ORDER BY 1 ASC
		`, trunc, trunc)
		rows, err := r.pool.Query(ctx, q, f.From, f.To, f.PipelineID, f.TeamID, f.OwnerUserID, f.Source, f.AnzscoID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var row TeamRow
			row.EntityType = "period"
			if err := rows.Scan(&row.EntityID, &row.EntityName, &row.LeadVolume, &row.Deals, &row.WonDeals,
				&row.Activities, &row.PipelineValue, &row.ConversionRate, &row.AvgStageDuration); err != nil {
				return nil, err
			}
			row.ConversionRate = round2(row.ConversionRate)
			out.Rows = append(out.Rows, row)
		}
		return out, rows.Err()

	case "pipeline":
		q := fmt.Sprintf(`
			SELECT COALESCE(p.id::text,'none'), COALESCE(p.name,'No pipeline'),
				0, COUNT(d.*), COUNT(*) FILTER (WHERE d.status='won'),
				0, COALESCE(SUM(d.value) FILTER (WHERE d.status='open'),0),
				CASE WHEN COUNT(*) FILTER (WHERE d.status IN ('won','lost')) > 0
					THEN COUNT(*) FILTER (WHERE d.status='won')::float8 * 100
						/ COUNT(*) FILTER (WHERE d.status IN ('won','lost'))
					ELSE 0 END,
				(SELECT AVG(tr.duration_seconds)::float8 FROM deal_stage_transitions tr
					JOIN deals dx ON dx.id=tr.deal_id
					WHERE dx.pipeline_id = p.id AND tr.from_stage_id IS NOT NULL
					  AND tr.exited_at >= $1 AND tr.exited_at <= $2 AND tr.duration_seconds > 0)
			FROM deals d
			LEFT JOIN pipelines p ON p.id = d.pipeline_id
			LEFT JOIN customers c ON c.id = d.customer_id
			WHERE d.created_at >= $1 AND d.created_at <= $2
			  AND ($3 = '' OR d.team_id::text = $3)
			  AND ($4 = '' OR d.owner_user_id::text = $4)
			  AND ($5 = '' OR d.source = $5)
			  AND ($6 = '' OR c.anzsco_id::text = $6)
			GROUP BY p.id, p.name
			ORDER BY %s %s NULLS LAST
			LIMIT 50
		`, orderExpr, dir)
		// Map order columns for this simpler select - use positional aliases via subquery
		_ = orderExpr
		q = strings.Replace(q, orderExpr, "COALESCE(SUM(d.value) FILTER (WHERE d.status='open'),0)", 1)
		if f.SortBy == "deals" {
			q = strings.Replace(q, "COALESCE(SUM(d.value) FILTER (WHERE d.status='open'),0) "+dir, "COUNT(d.*) "+dir, 1)
		} else if f.SortBy == "wonDeals" {
			q = strings.Replace(q, "COALESCE(SUM(d.value) FILTER (WHERE d.status='open'),0) "+dir, "COUNT(*) FILTER (WHERE d.status='won') "+dir, 1)
		}
		rows, err := r.pool.Query(ctx, q, f.From, f.To, f.TeamID, f.OwnerUserID, f.Source, f.AnzscoID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var row TeamRow
			row.EntityType = "pipeline"
			if err := rows.Scan(&row.EntityID, &row.EntityName, &row.LeadVolume, &row.Deals, &row.WonDeals,
				&row.Activities, &row.PipelineValue, &row.ConversionRate, &row.AvgStageDuration); err != nil {
				return nil, err
			}
			row.ConversionRate = round2(row.ConversionRate)
			out.Rows = append(out.Rows, row)
		}
		return out, rows.Err()

	default: // user
		q := fmt.Sprintf(`
			SELECT COALESCE(u.id::text,'none'), COALESCE(u.full_name,'Unassigned'),
				(SELECT COUNT(*) FROM leads l WHERE COALESCE(l.owner_user_id::text,'none') = COALESCE(u.id::text,'none')
					AND l.created_at >= $1 AND l.created_at <= $2 AND l.is_archived=FALSE
					AND ($3='' OR l.pipeline_id::text=$3) AND ($4='' OR l.team_id::text=$4)
					AND ($5='' OR l.source=$5) AND ($6='' OR l.anzsco_id::text=$6)),
				COUNT(d.*),
				COUNT(*) FILTER (WHERE d.status='won'),
				(SELECT COUNT(*) FROM activities a
					WHERE a.created_at >= $1 AND a.created_at <= $2
					  AND COALESCE(a.owner_user_id, a.actor_user_id) = u.id),
				COALESCE(SUM(d.value) FILTER (WHERE d.status='open'),0),
				CASE WHEN COUNT(*) FILTER (WHERE d.status IN ('won','lost')) > 0
					THEN COUNT(*) FILTER (WHERE d.status='won')::float8 * 100
						/ COUNT(*) FILTER (WHERE d.status IN ('won','lost'))
					ELSE 0 END,
				(SELECT AVG(tr.duration_seconds)::float8 FROM deal_stage_transitions tr
					JOIN deals dx ON dx.id=tr.deal_id
					WHERE dx.owner_user_id = u.id AND tr.from_stage_id IS NOT NULL
					  AND tr.duration_seconds > 0 AND tr.exited_at >= $1 AND tr.exited_at <= $2)
			FROM deals d
			FULL OUTER JOIN users u ON u.id = d.owner_user_id
			LEFT JOIN customers c ON c.id = d.customer_id
			WHERE (d.id IS NULL OR (
				d.created_at >= $1 AND d.created_at <= $2
				AND ($3='' OR d.pipeline_id::text=$3)
				AND ($4='' OR d.team_id::text=$4)
				AND ($5='' OR d.source=$5)
				AND ($6='' OR c.anzsco_id::text=$6)
			))
			AND (u.id IS NULL OR u.is_active = TRUE)
			GROUP BY u.id, u.full_name
			HAVING COUNT(d.*) > 0 OR (
				SELECT COUNT(*) FROM leads l WHERE l.owner_user_id = u.id
				AND l.created_at >= $1 AND l.created_at <= $2 AND l.is_archived=FALSE
			) > 0
			ORDER BY %s %s NULLS LAST
			LIMIT 50
		`, mapUserOrder(orderExpr), dir)
		rows, err := r.pool.Query(ctx, q, f.From, f.To, f.PipelineID, f.TeamID, f.Source, f.AnzscoID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var row TeamRow
			row.EntityType = "user"
			if err := rows.Scan(&row.EntityID, &row.EntityName, &row.LeadVolume, &row.Deals, &row.WonDeals,
				&row.Activities, &row.PipelineValue, &row.ConversionRate, &row.AvgStageDuration); err != nil {
				return nil, err
			}
			row.ConversionRate = round2(row.ConversionRate)
			out.Rows = append(out.Rows, row)
		}
		return out, rows.Err()
	}
}

func mapUserOrder(expr string) string {
	switch expr {
	case "lead_volume":
		return "3"
	case "deals":
		return "4"
	case "won_deals":
		return "5"
	case "activities":
		return "6"
	case "conversion_rate":
		return "8"
	case "avg_stage":
		return "9"
	default:
		return "7" // pipeline_value
	}
}

func (r *Repository) SourceAnalytics(ctx context.Context, f Filter) (*SourceAnalytics, error) {
	out := &SourceAnalytics{
		Question: "Which lead/deal sources produce volume and conversions?",
		Sources:  []SourceRow{},
	}
	rows, err := r.pool.Query(ctx, `
		WITH lead_src AS (
			SELECT COALESCE(NULLIF(source,''),'(none)') AS source, COUNT(*) AS leads,
				COUNT(*) FILTER (WHERE status='converted') AS converted
			FROM leads
			WHERE created_at >= $1 AND created_at <= $2 AND is_archived=FALSE
			  AND ($3='' OR pipeline_id::text=$3)
			  AND ($4='' OR team_id::text=$4)
			  AND ($5='' OR owner_user_id::text=$5)
			  AND ($6='' OR anzsco_id::text=$6)
			GROUP BY 1
		),
		deal_src AS (
			SELECT COALESCE(NULLIF(d.source,''),'(none)') AS source,
				COUNT(*) AS deals,
				COUNT(*) FILTER (WHERE d.status='won') AS won,
				COALESCE(SUM(d.value) FILTER (WHERE d.status='open'),0) AS pipeline_value
			FROM deals d
			LEFT JOIN customers c ON c.id = d.customer_id
			WHERE d.created_at >= $1 AND d.created_at <= $2
			  AND ($3='' OR d.pipeline_id::text=$3)
			  AND ($4='' OR d.team_id::text=$4)
			  AND ($5='' OR d.owner_user_id::text=$5)
			  AND ($6='' OR c.anzsco_id::text=$6)
			GROUP BY 1
		),
		cust_src AS (
			SELECT COALESCE(NULLIF(source,''),'(none)') AS source, COUNT(*) AS customers
			FROM customers
			WHERE created_at >= $1 AND created_at <= $2 AND is_archived=FALSE
			  AND ($4='' OR team_id::text=$4)
			  AND ($5='' OR owner_user_id::text=$5)
			  AND ($6='' OR anzsco_id::text=$6)
			GROUP BY 1
		)
		SELECT COALESCE(l.source, d.source, c.source),
			COALESCE(l.leads,0), COALESCE(c.customers,0), COALESCE(d.deals,0), COALESCE(d.won,0),
			COALESCE(d.pipeline_value,0),
			CASE WHEN COALESCE(l.leads,0) > 0 THEN COALESCE(l.converted,0)::float8 * 100 / l.leads ELSE 0 END
		FROM lead_src l
		FULL OUTER JOIN deal_src d ON d.source = l.source
		FULL OUTER JOIN cust_src c ON c.source = COALESCE(l.source, d.source)
		ORDER BY COALESCE(d.pipeline_value,0) DESC, COALESCE(l.leads,0) DESC
		LIMIT 40
	`, f.From, f.To, f.PipelineID, f.TeamID, f.OwnerUserID, f.AnzscoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var row SourceRow
		if err := rows.Scan(&row.Source, &row.Leads, &row.Customers, &row.Deals, &row.WonDeals,
			&row.PipelineValue, &row.ConversionRate); err != nil {
			return nil, err
		}
		row.ConversionRate = round2(row.ConversionRate)
		out.Sources = append(out.Sources, row)
	}
	return out, rows.Err()
}

func (r *Repository) Summary(ctx context.Context, f Filter) (*Summary, error) {
	leads, err := r.LeadAnalytics(ctx, f)
	if err != nil {
		return nil, err
	}
	pipe, err := r.PipelineAnalytics(ctx, f)
	if err != nil {
		return nil, err
	}
	acts, err := r.ActivityAnalytics(ctx, f)
	if err != nil {
		return nil, err
	}
	conv, err := r.ConversionAnalytics(ctx, f)
	if err != nil {
		return nil, err
	}
	return &Summary{
		LeadVolume: leads.LeadVolume, QualifiedLeads: leads.QualifiedLeads,
		Deals: pipe.Deals, Submissions: conv.Submissions, PositiveOutcomes: conv.PositiveOutcomes,
		Conversions: leads.ConvertedLeads, ConversionRate: leads.ConversionRate,
		AvgResponseSecs: leads.AvgResponseSecs, AvgStageSecs: pipe.AvgStageDuration,
		Activities: acts.Activities, PipelineValue: pipe.PipelineValue,
		From: f.From, To: f.To,
	}, nil
}
