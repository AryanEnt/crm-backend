package targets

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	MetricLeads             = "leads"
	MetricQualifiedLeads    = "qualified_leads"
	MetricSubmissions       = "submissions"
	MetricPositiveOutcomes  = "positive_outcomes"
	MetricConversions       = "conversions"
	MetricPipelineValue     = "pipeline_value"
	MetricActivities        = "activities"

	PeriodMonthly   = "monthly"
	PeriodQuarterly = "quarterly"

	ScopeOrganization = "organization"
	ScopeTeam         = "team"
	ScopeUser         = "user"
)

var ValidMetrics = []string{
	MetricLeads, MetricQualifiedLeads, MetricSubmissions, MetricPositiveOutcomes,
	MetricConversions, MetricPipelineValue, MetricActivities,
}

type Target struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Metric      string    `json:"metric"`
	PeriodType  string    `json:"periodType"`
	PeriodStart string    `json:"periodStart"` // YYYY-MM-DD
	PeriodEnd   string    `json:"periodEnd"`
	ScopeType   string    `json:"scopeType"`
	TeamID      *string   `json:"teamId"`
	TeamName    *string   `json:"teamName"`
	UserID      *string   `json:"userId"`
	UserName    *string   `json:"userName"`
	PipelineID  *string   `json:"pipelineId"`
	PipelineName *string  `json:"pipelineName"`
	TargetValue float64   `json:"targetValue"`
	Notes       string    `json:"notes"`
	CreatedBy   *string   `json:"createdBy"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Progress struct {
	Target           *Target   `json:"target"`
	Actual           float64   `json:"actual"`
	Remaining        float64   `json:"remaining"`
	ProgressPct      *float64  `json:"progressPct"` // null when target is zero
	ProgressLabel    string    `json:"progressLabel"`
	DaysRemaining    int       `json:"daysRemaining"`
	PeriodElapsedPct *float64  `json:"periodElapsedPct"`
	AsOf             time.Time `json:"asOf"`
}

type CreateInput struct {
	Name        string  `json:"name"`
	Metric      string  `json:"metric"`
	PeriodType  string  `json:"periodType"`
	PeriodStart string  `json:"periodStart"`
	PeriodEnd   string  `json:"periodEnd"`
	ScopeType   string  `json:"scopeType"`
	TeamID      *string `json:"teamId"`
	UserID      *string `json:"userId"`
	PipelineID  *string `json:"pipelineId"`
	TargetValue float64 `json:"targetValue"`
	Notes       string  `json:"notes"`
}

type UpdateInput struct {
	Name        *string  `json:"name"`
	Metric      *string  `json:"metric"`
	PeriodType  *string  `json:"periodType"`
	PeriodStart *string  `json:"periodStart"`
	PeriodEnd   *string  `json:"periodEnd"`
	ScopeType   *string  `json:"scopeType"`
	TeamID      *string  `json:"teamId"`
	UserID      *string  `json:"userId"`
	PipelineID  *string  `json:"pipelineId"`
	TargetValue *float64 `json:"targetValue"`
	Notes       *string  `json:"notes"`
}

type ListFilter struct {
	PeriodType string
	Metric     string
	ScopeType  string
	TeamID     string
	UserID     string
	PipelineID string
	From       string
	To         string
	Limit      int
	Offset     int
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const targetSelect = `
	SELECT t.id::text, t.name, t.metric, t.period_type,
		t.period_start::text, t.period_end::text, t.scope_type,
		t.team_id::text, tm.name, t.user_id::text, u.full_name,
		t.pipeline_id::text, p.name, t.target_value::float8, t.notes,
		t.created_by::text, t.created_at, t.updated_at
	FROM targets t
	LEFT JOIN teams tm ON tm.id = t.team_id
	LEFT JOIN users u ON u.id = t.user_id
	LEFT JOIN pipelines p ON p.id = t.pipeline_id
`

func (r *Repository) Get(ctx context.Context, id string) (*Target, error) {
	row := r.pool.QueryRow(ctx, targetSelect+` WHERE t.id=$1`, id)
	return scanTarget(row)
}

func (r *Repository) List(ctx context.Context, f ListFilter) ([]Target, int, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 50
	}
	args := []any{}
	where := []string{"1=1"}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.Replace(cond, "?", "$"+itoa(len(args)), 1))
	}
	if f.PeriodType != "" {
		add("t.period_type = ?", f.PeriodType)
	}
	if f.Metric != "" {
		add("t.metric = ?", f.Metric)
	}
	if f.ScopeType != "" {
		add("t.scope_type = ?", f.ScopeType)
	}
	if f.TeamID != "" {
		add("t.team_id::text = ?", f.TeamID)
	}
	if f.UserID != "" {
		add("t.user_id::text = ?", f.UserID)
	}
	if f.PipelineID != "" {
		add("t.pipeline_id::text = ?", f.PipelineID)
	}
	if f.From != "" {
		add("t.period_end >= ?::date", f.From)
	}
	if f.To != "" {
		add("t.period_start <= ?::date", f.To)
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM targets t WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, f.Limit, f.Offset)
	rows, err := r.pool.Query(ctx, targetSelect+`
		WHERE `+whereSQL+`
		ORDER BY t.period_start DESC, t.created_at DESC
		LIMIT $`+itoa(len(args)-1)+` OFFSET $`+itoa(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var items []Target
	for rows.Next() {
		t, err := scanTarget(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *t)
	}
	if items == nil {
		items = []Target{}
	}
	return items, total, rows.Err()
}

func (r *Repository) Create(ctx context.Context, actorID string, in CreateInput) (*Target, error) {
	id := uuid.NewString()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO targets (
			id, name, metric, period_type, period_start, period_end,
			scope_type, team_id, user_id, pipeline_id, target_value, notes, created_by
		) VALUES ($1,$2,$3,$4,$5::date,$6::date,$7,$8,$9,$10,$11,$12,$13)
	`, id, strings.TrimSpace(in.Name), in.Metric, in.PeriodType, in.PeriodStart, in.PeriodEnd,
		in.ScopeType, emptyToNil(in.TeamID), emptyToNil(in.UserID), emptyToNil(in.PipelineID),
		in.TargetValue, in.Notes, emptyToNil(&actorID))
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *Repository) Update(ctx context.Context, id string, current *Target, in UpdateInput) (*Target, error) {
	name := current.Name
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	metric := current.Metric
	if in.Metric != nil {
		metric = *in.Metric
	}
	periodType := current.PeriodType
	if in.PeriodType != nil {
		periodType = *in.PeriodType
	}
	periodStart := current.PeriodStart
	if in.PeriodStart != nil {
		periodStart = *in.PeriodStart
	}
	periodEnd := current.PeriodEnd
	if in.PeriodEnd != nil {
		periodEnd = *in.PeriodEnd
	}
	scopeType := current.ScopeType
	if in.ScopeType != nil {
		scopeType = *in.ScopeType
	}
	teamID := current.TeamID
	if in.TeamID != nil {
		teamID = emptyToNil(in.TeamID)
	}
	userID := current.UserID
	if in.UserID != nil {
		userID = emptyToNil(in.UserID)
	}
	pipelineID := current.PipelineID
	if in.PipelineID != nil {
		pipelineID = emptyToNil(in.PipelineID)
	}
	value := current.TargetValue
	if in.TargetValue != nil {
		value = *in.TargetValue
	}
	notes := current.Notes
	if in.Notes != nil {
		notes = *in.Notes
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE targets SET
			name=$2, metric=$3, period_type=$4, period_start=$5::date, period_end=$6::date,
			scope_type=$7, team_id=$8, user_id=$9, pipeline_id=$10, target_value=$11, notes=$12
		WHERE id=$1
	`, id, name, metric, periodType, periodStart, periodEnd, scopeType, teamID, userID, pipelineID, value, notes)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM targets WHERE id=$1`, id)
	return err
}

// ComputeActual calculates the live actual for a target metric/scope/period.
func (r *Repository) ComputeActual(ctx context.Context, t *Target) (float64, error) {
	from := t.PeriodStart
	to := t.PeriodEnd
	teamID := ""
	userID := ""
	if t.TeamID != nil {
		teamID = *t.TeamID
	}
	if t.UserID != nil {
		userID = *t.UserID
	}
	pipelineID := ""
	if t.PipelineID != nil {
		pipelineID = *t.PipelineID
	}

	switch t.Metric {
	case MetricLeads:
		return r.scalar(ctx, `
			SELECT COUNT(*)::float8 FROM leads l
			WHERE l.is_archived=FALSE
			  AND l.created_at::date >= $1::date AND l.created_at::date <= $2::date
			  AND ($3='' OR l.team_id::text=$3)
			  AND ($4='' OR l.owner_user_id::text=$4)
			  AND ($5='' OR l.pipeline_id::text=$5)
		`, from, to, teamID, userID, pipelineID)
	case MetricQualifiedLeads:
		return r.scalar(ctx, `
			SELECT COUNT(*)::float8 FROM leads l
			WHERE l.is_archived=FALSE
			  AND l.status IN ('qualified','converted')
			  AND l.created_at::date >= $1::date AND l.created_at::date <= $2::date
			  AND ($3='' OR l.team_id::text=$3)
			  AND ($4='' OR l.owner_user_id::text=$4)
			  AND ($5='' OR l.pipeline_id::text=$5)
		`, from, to, teamID, userID, pipelineID)
	case MetricConversions:
		return r.scalar(ctx, `
			SELECT COUNT(*)::float8 FROM leads l
			WHERE l.is_archived=FALSE AND l.status='converted'
			  AND COALESCE(l.converted_at::date, l.updated_at::date) >= $1::date
			  AND COALESCE(l.converted_at::date, l.updated_at::date) <= $2::date
			  AND ($3='' OR l.team_id::text=$3)
			  AND ($4='' OR l.owner_user_id::text=$4)
			  AND ($5='' OR l.pipeline_id::text=$5)
		`, from, to, teamID, userID, pipelineID)
	case MetricPositiveOutcomes:
		return r.scalar(ctx, `
			SELECT COUNT(*)::float8 FROM deals d
			WHERE d.status='won'
			  AND d.updated_at::date >= $1::date AND d.updated_at::date <= $2::date
			  AND ($3='' OR d.team_id::text=$3)
			  AND ($4='' OR d.owner_user_id::text=$4)
			  AND ($5='' OR d.pipeline_id::text=$5)
		`, from, to, teamID, userID, pipelineID)
	case MetricSubmissions:
		return r.scalar(ctx, `
			SELECT COUNT(DISTINCT d.id)::float8 FROM deals d
			WHERE d.created_at::date >= $1::date AND d.created_at::date <= $2::date
			  AND ($3='' OR d.team_id::text=$3)
			  AND ($4='' OR d.owner_user_id::text=$4)
			  AND ($5='' OR d.pipeline_id::text=$5)
			  AND EXISTS (
				SELECT 1 FROM deal_stage_transitions t
				JOIN pipeline_stages ps ON ps.id = t.to_stage_id
				WHERE t.deal_id = d.id
				  AND (lower(ps.name) LIKE '%negotiat%' OR lower(ps.name) LIKE '%submit%' OR lower(ps.name) LIKE '%proposal%')
			  )
		`, from, to, teamID, userID, pipelineID)
	case MetricPipelineValue:
		// Snapshot of open pipeline value for the scope (not fabricated over time).
		return r.scalar(ctx, `
			SELECT COALESCE(SUM(d.value),0)::float8 FROM deals d
			WHERE d.status='open'
			  AND ($1='' OR d.team_id::text=$1)
			  AND ($2='' OR d.owner_user_id::text=$2)
			  AND ($3='' OR d.pipeline_id::text=$3)
		`, teamID, userID, pipelineID)
	case MetricActivities:
		return r.scalar(ctx, `
			SELECT COUNT(*)::float8 FROM activities a
			WHERE a.created_at::date >= $1::date AND a.created_at::date <= $2::date
			  AND ($3='' OR EXISTS (
					SELECT 1 FROM deals d WHERE d.id=a.deal_id AND d.team_id::text=$3
				) OR EXISTS (
					SELECT 1 FROM leads l WHERE l.id=a.lead_id AND l.team_id::text=$3
				) OR EXISTS (
					SELECT 1 FROM customers c WHERE c.id=a.customer_id AND c.team_id::text=$3
				))
			  AND ($4='' OR COALESCE(a.owner_user_id, a.actor_user_id)::text=$4)
			  AND ($5='' OR EXISTS (
					SELECT 1 FROM deals d WHERE d.id=a.deal_id AND d.pipeline_id::text=$5
				) OR EXISTS (
					SELECT 1 FROM leads l WHERE l.id=a.lead_id AND l.pipeline_id::text=$5
				))
		`, from, to, teamID, userID, pipelineID)
	default:
		return 0, nil
	}
}

func (r *Repository) scalar(ctx context.Context, q string, args ...any) (float64, error) {
	var v float64
	err := r.pool.QueryRow(ctx, q, args...).Scan(&v)
	return v, err
}

type scannable interface {
	Scan(dest ...any) error
}

func scanTarget(row scannable) (*Target, error) {
	var t Target
	err := row.Scan(
		&t.ID, &t.Name, &t.Metric, &t.PeriodType, &t.PeriodStart, &t.PeriodEnd, &t.ScopeType,
		&t.TeamID, &t.TeamName, &t.UserID, &t.UserName, &t.PipelineID, &t.PipelineName,
		&t.TargetValue, &t.Notes, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

func emptyToNil(v *string) *string {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil
	}
	return v
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
