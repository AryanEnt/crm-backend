package predictions

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

type CRMFeatureBuilder struct {
	pool *pgxpool.Pool
}

func NewCRMFeatureBuilder(pool *pgxpool.Pool) *CRMFeatureBuilder {
	return &CRMFeatureBuilder{pool: pool}
}

func (b *CRMFeatureBuilder) BuildLeadFeatures(ctx context.Context, leadID string) (FeatureSet, error) {
	var (
		fullName, email, phone, source, priority, status, notes                                   string
		occupation, jobTitle, employer, qualification                                             string
		stageName, stageID, pipelineID, anzscoID, anzscoCode                                      *string
		skills                                                                                    []string
		lastActivity, nextActivity, createdAt                                                     time.Time
		hasLast, hasNext                                                                          bool
		ageDays                                                                                   int
	)
	var lastActNull, nextActNull *time.Time
	err := b.pool.QueryRow(ctx, `
		SELECT l.full_name, COALESCE(l.email,''), COALESCE(l.phone,''), l.source, l.priority, l.status,
			l.notes, l.occupation, l.job_title, l.employer, l.qualification, COALESCE(l.skills,'{}'),
			l.stage_id::text, ps.name, l.pipeline_id::text, l.anzsco_id::text, ao.code,
			l.last_activity_at, l.next_activity_at, l.created_at,
			EXTRACT(DAY FROM NOW() - l.created_at)::int
		FROM leads l
		LEFT JOIN pipeline_stages ps ON ps.id = l.stage_id
		LEFT JOIN anzsco_occupations ao ON ao.id = l.anzsco_id
		WHERE l.id=$1
	`, leadID).Scan(
		&fullName, &email, &phone, &source, &priority, &status,
		&notes, &occupation, &jobTitle, &employer, &qualification, &skills,
		&stageID, &stageName, &pipelineID, &anzscoID, &anzscoCode,
		&lastActNull, &nextActNull, &createdAt, &ageDays,
	)
	if err != nil {
		return FeatureSet{}, err
	}
	if lastActNull != nil {
		lastActivity = *lastActNull
		hasLast = true
	}
	if nextActNull != nil {
		nextActivity = *nextActNull
		hasNext = true
	}

	var activityCount7, activityCount30, completedCount, overdueFollowups int
	_ = b.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE created_at >= NOW() - INTERVAL '7 days'),
			COUNT(*) FILTER (WHERE created_at >= NOW() - INTERVAL '30 days'),
			COUNT(*) FILTER (WHERE status='completed'),
			COUNT(*) FILTER (WHERE status='overdue' OR (status IN ('upcoming','due') AND due_at IS NOT NULL AND due_at < NOW()))
		FROM activities WHERE lead_id=$1
	`, leadID).Scan(&activityCount7, &activityCount30, &completedCount, &overdueFollowups)

	var docsUploaded, docsVerified, docsRequested, docsRejected int
	_ = b.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status IN ('uploaded','verified')),
			COUNT(*) FILTER (WHERE status='verified'),
			COUNT(*) FILTER (WHERE status IN ('requested','missing')),
			COUNT(*) FILTER (WHERE status='rejected')
		FROM documents WHERE lead_id=$1 OR customer_id IN (
			SELECT converted_customer_id FROM leads WHERE id=$1 AND converted_customer_id IS NOT NULL
		)
	`, leadID).Scan(&docsUploaded, &docsVerified, &docsRequested, &docsRejected)

	// Historical conversion by source (sample size exposed; strategies decide sufficiency)
	var sourceClosed, sourceConverted int
	_ = b.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status IN ('converted','unqualified','archived') OR converted_at IS NOT NULL),
			COUNT(*) FILTER (WHERE status='converted')
		FROM leads
		WHERE lower(source)=lower($1) AND created_at >= NOW() - INTERVAL '365 days'
	`, source).Scan(&sourceClosed, &sourceConverted)

	completeness := 0.0
	fields := []bool{
		strings.TrimSpace(email) != "",
		strings.TrimSpace(phone) != "",
		strings.TrimSpace(occupation) != "",
		strings.TrimSpace(jobTitle) != "",
		anzscoID != nil && *anzscoID != "",
		strings.TrimSpace(employer) != "",
		len(skills) > 0,
	}
	for _, ok := range fields {
		if ok {
			completeness++
		}
	}
	completeness = completeness / float64(len(fields)) * 100

	daysSinceActivity := 999.0
	if hasLast {
		daysSinceActivity = time.Since(lastActivity).Hours() / 24
	} else {
		daysSinceActivity = time.Since(createdAt).Hours() / 24
	}

	features := map[string]any{
		"fullName": fullName, "source": source, "priority": priority, "status": status,
		"stageId": stageID, "stageName": deref(stageName), "pipelineId": pipelineID,
		"hasEmail": strings.TrimSpace(email) != "", "hasPhone": strings.TrimSpace(phone) != "",
		"hasAnzsco": anzscoID != nil && *anzscoID != "", "anzscoCode": deref(anzscoCode),
		"profileCompletenessPct": round2(completeness),
		"ageDays": ageDays, "daysSinceActivity": round2(daysSinceActivity),
		"activityCount7d": activityCount7, "activityCount30d": activityCount30,
		"completedActivities": completedCount, "overdueFollowups": overdueFollowups,
		"hasNextActivity": hasNext, "nextActivityAt": nullTime(hasNext, nextActivity),
		"docsUploaded": docsUploaded, "docsVerified": docsVerified,
		"docsRequested": docsRequested, "docsRejected": docsRejected,
		"sourceClosedSample": sourceClosed, "sourceConverted": sourceConverted,
		"sourceConversionRate": rate(sourceConverted, sourceClosed),
	}
	_ = notes
	return FeatureSet{
		EntityType: "lead", EntityID: leadID, Features: features,
		BuiltAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (b *CRMFeatureBuilder) BuildDealFeatures(ctx context.Context, dealID string) (FeatureSet, error) {
	var (
		title, status, source                                                                   string
		value                                                                                   *float64
		stageName, stageID, pipelineID                                                          *string
		probability                                                                             float64
		stageEnteredAt, createdAt                                                               time.Time
		expectedClose                                                                           *time.Time
		daysInStage, ageDays                                                                    int
		slaHours                                                                                *int
	)
	var lastActNull, nextActNull *time.Time
	err := b.pool.QueryRow(ctx, `
		SELECT d.title, d.status, d.source, d.value, d.stage_id::text, ps.name, d.pipeline_id::text,
			COALESCE(ps.probability,0), d.stage_entered_at, d.created_at, d.expected_close_at,
			EXTRACT(DAY FROM NOW() - d.stage_entered_at)::int,
			EXTRACT(DAY FROM NOW() - d.created_at)::int,
			ps.sla_hours, d.last_activity_at, d.next_activity_at
		FROM deals d
		LEFT JOIN pipeline_stages ps ON ps.id = d.stage_id
		WHERE d.id=$1
	`, dealID).Scan(
		&title, &status, &source, &value, &stageID, &stageName, &pipelineID,
		&probability, &stageEnteredAt, &createdAt, &expectedClose,
		&daysInStage, &ageDays, &slaHours, &lastActNull, &nextActNull,
	)
	if err != nil {
		return FeatureSet{}, err
	}

	var activityCount7, activityCount30, overdue int
	_ = b.pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE created_at >= NOW() - INTERVAL '7 days'),
			COUNT(*) FILTER (WHERE created_at >= NOW() - INTERVAL '30 days'),
			COUNT(*) FILTER (WHERE status='overdue' OR (status IN ('upcoming','due') AND due_at IS NOT NULL AND due_at < NOW()))
		FROM activities WHERE deal_id=$1
	`, dealID).Scan(&activityCount7, &activityCount30, &overdue)

	daysSinceActivity := time.Since(createdAt).Hours() / 24
	if lastActNull != nil {
		daysSinceActivity = time.Since(*lastActNull).Hours() / 24
	}

	var docsUploaded, docsVerified int
	_ = b.pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE status IN ('uploaded','verified')),
			COUNT(*) FILTER (WHERE status='verified')
		FROM documents WHERE deal_id=$1
	`, dealID).Scan(&docsUploaded, &docsVerified)

	var closedSample, wonSample int
	if pipelineID != nil {
		_ = b.pool.QueryRow(ctx, `
			SELECT COUNT(*) FILTER (WHERE status IN ('won','lost')),
				COUNT(*) FILTER (WHERE status='won')
			FROM deals WHERE pipeline_id=$1 AND updated_at >= NOW() - INTERVAL '365 days'
		`, *pipelineID).Scan(&closedSample, &wonSample)
	}

	sla := 0
	if slaHours != nil {
		sla = *slaHours
	}
	features := map[string]any{
		"title": title, "status": status, "source": source,
		"value": value, "stageId": stageID, "stageName": deref(stageName),
		"pipelineId": pipelineID, "stageProbability": probability,
		"daysInStage": daysInStage, "ageDays": ageDays, "slaHours": sla,
		"activityCount7d": activityCount7, "activityCount30d": activityCount30,
		"overdueFollowups": overdue, "daysSinceActivity": round2(daysSinceActivity),
		"hasNextActivity": nextActNull != nil,
		"docsUploaded": docsUploaded, "docsVerified": docsVerified,
		"expectedCloseAt": expectedClose,
		"pipelineClosedSample": closedSample, "pipelineWonSample": wonSample,
		"pipelineWinRate": rate(wonSample, closedSample),
	}
	return FeatureSet{
		EntityType: "deal", EntityID: dealID, Features: features,
		BuiltAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (b *CRMFeatureBuilder) BuildOwnerWorkloadFeatures(ctx context.Context, ownerUserID string) (FeatureSet, error) {
	var openLeads, openDeals, overdueActs, due7, completed7 int
	err := b.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM leads WHERE owner_user_id=$1 AND is_archived=FALSE AND status='open'),
			(SELECT COUNT(*) FROM deals WHERE owner_user_id=$1 AND status='open'),
			(SELECT COUNT(*) FROM activities WHERE COALESCE(owner_user_id, actor_user_id)=$1
				AND (status='overdue' OR (status IN ('upcoming','due') AND due_at IS NOT NULL AND due_at < NOW()))),
			(SELECT COUNT(*) FROM activities WHERE COALESCE(owner_user_id, actor_user_id)=$1
				AND due_at >= NOW() AND due_at < NOW() + INTERVAL '7 days'
				AND status IN ('upcoming','due')),
			(SELECT COUNT(*) FROM activities WHERE COALESCE(owner_user_id, actor_user_id)=$1
				AND status='completed' AND completed_at >= NOW() - INTERVAL '7 days')
	`, ownerUserID).Scan(&openLeads, &openDeals, &overdueActs, &due7, &completed7)
	if err != nil {
		return FeatureSet{}, err
	}
	var name string
	_ = b.pool.QueryRow(ctx, `SELECT full_name FROM users WHERE id=$1`, ownerUserID).Scan(&name)

	features := map[string]any{
		"ownerUserId": ownerUserID, "ownerName": name,
		"openLeads": openLeads, "openDeals": openDeals,
		"overdueActivities": overdueActs, "dueNext7Days": due7,
		"completedLast7Days": completed7,
		"workloadIndex": openLeads + openDeals*2 + overdueActs*3 + due7,
	}
	return FeatureSet{
		EntityType: "user", EntityID: ownerUserID, Features: features,
		BuiltAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (b *CRMFeatureBuilder) BuildPipelineRiskFeatures(ctx context.Context, pipelineID string) (FeatureSet, error) {
	var name string
	if err := b.pool.QueryRow(ctx, `SELECT name FROM pipelines WHERE id=$1`, pipelineID).Scan(&name); err != nil {
		return FeatureSet{}, err
	}
	var openCount int
	var openValue float64
	var stalled, overSLA, noActivity14 int
	_ = b.pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(value),0),
			COUNT(*) FILTER (WHERE EXTRACT(DAY FROM NOW() - stage_entered_at) >= 14),
			COUNT(*) FILTER (WHERE ps.sla_hours IS NOT NULL
				AND EXTRACT(EPOCH FROM (NOW() - stage_entered_at))/3600 > ps.sla_hours),
			COUNT(*) FILTER (WHERE last_activity_at IS NULL OR last_activity_at < NOW() - INTERVAL '14 days')
		FROM deals d
		LEFT JOIN pipeline_stages ps ON ps.id = d.stage_id
		WHERE d.pipeline_id=$1 AND d.status='open'
	`, pipelineID).Scan(&openCount, &openValue, &stalled, &overSLA, &noActivity14)

	var closedSample, wonSample int
	_ = b.pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE status IN ('won','lost')),
			COUNT(*) FILTER (WHERE status='won')
		FROM deals WHERE pipeline_id=$1 AND updated_at >= NOW() - INTERVAL '365 days'
	`, pipelineID).Scan(&closedSample, &wonSample)

	features := map[string]any{
		"pipelineId": pipelineID, "pipelineName": name,
		"openDealCount": openCount, "openPipelineValue": openValue,
		"stalledDeals14d": stalled, "overSLADeals": overSLA,
		"noActivity14d": noActivity14,
		"closedSample": closedSample, "wonSample": wonSample,
		"winRate": rate(wonSample, closedSample),
	}
	return FeatureSet{
		EntityType: "pipeline", EntityID: pipelineID, Features: features,
		BuiltAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (r *Repository) SaveLeadScore(ctx context.Context, actorID string, res *PredictionResult) (string, error) {
	id := uuid.NewString()
	feat, _ := json.Marshal(res.Features)
	pos, _ := json.Marshal(res.Explanation.PositiveSignals)
	neg, _ := json.Marshal(res.Explanation.NegativeSignals)
	expl, _ := json.Marshal(res.Explanation)
	score := 0.0
	if res.Score != nil {
		score = *res.Score
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO lead_score_snapshots (
			id, lead_id, score, strategy_code, strategy_version, features,
			positive_signals, negative_signals, explanation, insufficient_hist, computed_by
		) VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8::jsonb,$9::jsonb,$10,$11)
	`, id, res.EntityID, score, res.StrategyCode, res.StrategyVersion, string(feat),
		string(pos), string(neg), string(expl), res.InsufficientHist, emptyUUID(actorID))
	return id, err
}

func (r *Repository) ListLeadScoreHistory(ctx context.Context, leadID string, limit int) ([]map[string]any, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, score::float8, strategy_code, strategy_version,
			positive_signals, negative_signals, explanation, insufficient_hist, computed_at
		FROM lead_score_snapshots
		WHERE lead_id=$1
		ORDER BY computed_at DESC
		LIMIT $2
	`, leadID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, code, ver string
		var score float64
		var pos, neg, expl []byte
		var insuff bool
		var at time.Time
		if err := rows.Scan(&id, &score, &code, &ver, &pos, &neg, &expl, &insuff, &at); err != nil {
			return nil, err
		}
		item := map[string]any{
			"id": id, "score": score, "strategyCode": code, "strategyVersion": ver,
			"insufficientHistoricalData": insuff, "computedAt": at.UTC().Format(time.RFC3339),
			"disclaimer": Disclaimer,
		}
		var posS, negS []Signal
		var explanation Explanation
		_ = json.Unmarshal(pos, &posS)
		_ = json.Unmarshal(neg, &negS)
		_ = json.Unmarshal(expl, &explanation)
		item["positiveSignals"] = posS
		item["negativeSignals"] = negS
		item["explanation"] = explanation
		out = append(out, item)
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, rows.Err()
}

func (r *Repository) SavePrediction(ctx context.Context, actorID string, res *PredictionResult) (string, error) {
	id := uuid.NewString()
	feat, _ := json.Marshal(res.Features)
	pos, _ := json.Marshal(res.Explanation.PositiveSignals)
	neg, _ := json.Marshal(res.Explanation.NegativeSignals)
	expl, _ := json.Marshal(res.Explanation)
	payload, _ := json.Marshal(res.Payload)
	_, err := r.pool.Exec(ctx, `
		INSERT INTO prediction_results (
			id, entity_type, entity_id, insight_type, strategy_code, strategy_version,
			available, message, score, label, features, positive_signals, negative_signals,
			explanation, payload, computed_by
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12::jsonb,$13::jsonb,$14::jsonb,$15::jsonb,$16)
	`, id, res.EntityType, res.EntityID, res.InsightType, res.StrategyCode, res.StrategyVersion,
		res.Available, res.Message, res.Score, res.Label, string(feat), string(pos), string(neg),
		string(expl), string(payload), emptyUUID(actorID))
	return id, err
}

func (r *Repository) LeadExists(ctx context.Context, id string) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM leads WHERE id=$1)`, id).Scan(&ok)
	return ok, err
}

func (r *Repository) DealExists(ctx context.Context, id string) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM deals WHERE id=$1)`, id).Scan(&ok)
	return ok, err
}

func emptyUUID(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func nullTime(ok bool, t time.Time) any {
	if !ok {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func rate(num, den int) float64 {
	if den <= 0 {
		return 0
	}
	return round2(float64(num) / float64(den) * 100)
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func asFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case *float64:
		if t == nil {
			return 0
		}
		return *t
	case *int:
		if t == nil {
			return 0
		}
		return float64(*t)
	case json.Number:
		f, _ := t.Float64()
		return f
	default:
		return 0
	}
}

func asInt(v any) int {
	return int(asFloat(v))
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func asBool(v any) bool {
	b, _ := v.(bool)
	return b
}

var _ = pgx.ErrNoRows
