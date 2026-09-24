package analytics

import (
	"context"
	"math"
)

func (r *Repository) Funnel(ctx context.Context, f Filter, pipelineID, pipelineName, pipelineKind string) (*FunnelResult, error) {
	stages, err := r.ListStages(ctx, pipelineID)
	if err != nil {
		return nil, err
	}
	result := &FunnelResult{
		PipelineID: pipelineID, PipelineName: pipelineName, PipelineKind: pipelineKind,
		From: f.From, To: f.To, Stages: []FunnelStage{},
	}
	if pipelineKind == "leads" {
		return r.funnelLeads(ctx, f, result, stages)
	}
	return r.funnelDeals(ctx, f, result, stages)
}

func (r *Repository) funnelDeals(ctx context.Context, f Filter, result *FunnelResult, stages []stageRow) (*FunnelResult, error) {
	dealFilter := func(b *sqlBuilder) {
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
	}

	for _, st := range stages {
		fs := FunnelStage{
			StageID: st.ID, StageName: st.Name, Position: st.Position,
			IsWon: st.IsWon, IsLost: st.IsLost,
		}

		// Entered: transitions into stage in range
		bEnter := &sqlBuilder{}
		bEnter.add("t.to_stage_id::text = ?", st.ID)
		bEnter.add("t.to_pipeline_id::text = ?", result.PipelineID)
		bEnter.add("t.exited_at >= ?", f.From)
		bEnter.add("t.exited_at <= ?", f.To)
		dealFilter(bEnter)
		enterSQL := `
			SELECT COUNT(DISTINCT t.deal_id), COALESCE(SUM(d.value),0)
			FROM deal_stage_transitions t
			JOIN deals d ON d.id = t.deal_id
			LEFT JOIN customers c ON c.id = d.customer_id
			WHERE ` + bEnter.sql()
		if err := r.pool.QueryRow(ctx, enterSQL, bEnter.args...).Scan(&fs.Entered, &fs.EnteredValue); err != nil {
			return nil, err
		}

		// Left: transitions out of stage in range
		bLeft := &sqlBuilder{}
		bLeft.add("t.from_stage_id::text = ?", st.ID)
		bLeft.add("COALESCE(t.from_pipeline_id::text, t.to_pipeline_id::text) = ?", result.PipelineID)
		bLeft.add("t.exited_at >= ?", f.From)
		bLeft.add("t.exited_at <= ?", f.To)
		dealFilter(bLeft)
		leftSQL := `
			SELECT COUNT(DISTINCT t.deal_id), AVG(t.duration_seconds)::float8
			FROM deal_stage_transitions t
			JOIN deals d ON d.id = t.deal_id
			LEFT JOIN customers c ON c.id = d.customer_id
			WHERE ` + bLeft.sql()
		var avgSecs *float64
		if err := r.pool.QueryRow(ctx, leftSQL, bLeft.args...).Scan(&fs.Left, &avgSecs); err != nil {
			return nil, err
		}
		fs.AvgTimeInStageSecs = avgSecs

		// Current open value in stage
		bCur := &sqlBuilder{}
		bCur.add("d.stage_id::text = ?", st.ID)
		bCur.add("d.pipeline_id::text = ?", result.PipelineID)
		bCur.add("d.status = ?", "open")
		if f.TeamID != "" {
			bCur.add("d.team_id::text = ?", f.TeamID)
		}
		if f.OwnerUserID != "" {
			bCur.add("d.owner_user_id::text = ?", f.OwnerUserID)
		}
		if f.Source != "" {
			bCur.add("d.source = ?", f.Source)
		}
		if f.AnzscoID != "" {
			bCur.add("c.anzsco_id::text = ?", f.AnzscoID)
		}
		curSQL := `
			SELECT COUNT(*), COALESCE(SUM(d.value),0)
			FROM deals d
			LEFT JOIN customers c ON c.id = d.customer_id
			WHERE ` + bCur.sql()
		var totalVal float64
		if err := r.pool.QueryRow(ctx, curSQL, bCur.args...).Scan(&fs.CurrentlyInStage, &totalVal); err != nil {
			return nil, err
		}
		fs.TotalValue = totalVal
		if fs.CurrentlyInStage > 0 {
			fs.AvgDealValue = totalVal / float64(fs.CurrentlyInStage)
		}

		fs.ConversionRate = round2(pct(fs.Left, fs.Entered))
		fs.DropOffRate = round2(100 - fs.ConversionRate)
		if fs.Entered == 0 {
			fs.DropOffRate = 0
		}
		if st.IsWon || st.IsLost {
			// Terminal stages: conversion is not "left"; drop-off N/A as leave rate
			fs.ConversionRate = 0
			fs.DropOffRate = 0
		}

		result.Stages = append(result.Stages, fs)
	}

	// Totals
	bTot := &sqlBuilder{}
	bTot.add("d.pipeline_id::text = ?", result.PipelineID)
	bTot.add("d.created_at >= ?", f.From)
	bTot.add("d.created_at <= ?", f.To)
	dealFilter(bTot)
	totSQL := `
		SELECT
			COUNT(*) FILTER (WHERE TRUE),
			COUNT(*) FILTER (WHERE d.status='won'),
			COUNT(*) FILTER (WHERE d.status='lost'),
			COUNT(*) FILTER (WHERE d.status='open'),
			COALESCE(SUM(d.value) FILTER (WHERE d.status='open'),0),
			COALESCE(AVG(d.value),0)
		FROM deals d
		LEFT JOIN customers c ON c.id = d.customer_id
		WHERE ` + bTot.sql()
	if err := r.pool.QueryRow(ctx, totSQL, bTot.args...).Scan(
		&result.Totals.Entered, &result.Totals.Won, &result.Totals.Lost, &result.Totals.Open,
		&result.Totals.TotalValue, &result.Totals.AvgDealValue,
	); err != nil {
		return nil, err
	}
	closed := result.Totals.Won + result.Totals.Lost
	result.Totals.OverallConversion = round2(pct(result.Totals.Won, closed))
	if len(result.Stages) > 0 {
		result.Totals.Entered = result.Stages[0].Entered
	}
	return result, nil
}

func (r *Repository) funnelLeads(ctx context.Context, f Filter, result *FunnelResult, stages []stageRow) (*FunnelResult, error) {
	// Without lead stage history: snapshot + created-in-stage as entered proxy.
	for _, st := range stages {
		fs := FunnelStage{
			StageID: st.ID, StageName: st.Name, Position: st.Position,
			IsWon: st.IsWon, IsLost: st.IsLost,
		}
		b := &sqlBuilder{}
		b.applyCommonLead("l", f)
		b.add("l.stage_id::text = ?", st.ID)
		b.add("l.pipeline_id::text = ?", result.PipelineID)
		q := `SELECT COUNT(*), COALESCE(SUM(l.potential_value),0) FROM leads l WHERE ` + b.sql()
		var val float64
		if err := r.pool.QueryRow(ctx, q, b.args...).Scan(&fs.Entered, &val); err != nil {
			return nil, err
		}
		fs.CurrentlyInStage = fs.Entered
		fs.TotalValue = val
		fs.EnteredValue = val
		if fs.Entered > 0 {
			fs.AvgDealValue = val / float64(fs.Entered)
		}
		result.Stages = append(result.Stages, fs)
	}
	// Approximate conversion between consecutive stages using counts
	for i := range result.Stages {
		if i+1 < len(result.Stages) && !result.Stages[i].IsLost && !result.Stages[i].IsWon {
			next := result.Stages[i+1].Entered
			result.Stages[i].Left = next
			result.Stages[i].ConversionRate = round2(pct(next, result.Stages[i].Entered))
			result.Stages[i].DropOffRate = round2(100 - result.Stages[i].ConversionRate)
			if result.Stages[i].Entered == 0 {
				result.Stages[i].DropOffRate = 0
			}
		}
	}
	if len(result.Stages) > 0 {
		result.Totals.Entered = result.Stages[0].Entered
	}
	var openVal float64
	for _, s := range result.Stages {
		if !s.IsWon && !s.IsLost {
			result.Totals.Open += s.CurrentlyInStage
			openVal += s.TotalValue
		}
		if s.IsWon {
			result.Totals.Won += s.Entered
		}
		if s.IsLost {
			result.Totals.Lost += s.Entered
		}
	}
	result.Totals.TotalValue = openVal
	result.Totals.AvgDealValue = div(openVal, float64(max(result.Totals.Open, 1)))
	result.Totals.OverallConversion = round2(pct(result.Totals.Won, result.Totals.Won+result.Totals.Lost))
	return result, nil
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
