package deals

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/crm/backend/internal/datascope"
	"github.com/crm/backend/internal/pipelines"
)

type Deal struct {
	ID              string         `json:"id"`
	Title           string         `json:"title"`
	CustomerID      string         `json:"customerId"`
	CustomerName    string         `json:"customerName"`
	OwnerUserID     *string        `json:"ownerUserId"`
	OwnerName       *string        `json:"ownerName"`
	TeamID          *string        `json:"teamId"`
	TeamName        *string        `json:"teamName"`
	PipelineID      *string        `json:"pipelineId"`
	PipelineName    *string        `json:"pipelineName"`
	StageID         *string        `json:"stageId"`
	StageName       *string        `json:"stageName"`
	StageAccent     *string        `json:"stageAccent"`
	Value           *float64       `json:"value"`
	Currency        string         `json:"currency"`
	Probability     *float64       `json:"probability"`
	ExpectedCloseAt *string        `json:"expectedCloseAt"`
	Source          string         `json:"source"`
	Priority        string         `json:"priority"`
	Status          string         `json:"status"`
	LostReason      string         `json:"lostReason"`
	Notes           string         `json:"notes"`
	FieldValues     map[string]any `json:"fieldValues"`
	LastActivityAt  *time.Time     `json:"lastActivityAt"`
	NextActivityAt  *time.Time     `json:"nextActivityAt"`
	StageEnteredAt  time.Time      `json:"stageEnteredAt"`
	AgeDays         int            `json:"ageDays"`
	DaysInStage     int            `json:"daysInStage"`
	Attention       string         `json:"attention"` // "", "no_recent_activity", "attention_needed", "over_sla"
	CreatedAt       time.Time      `json:"createdAt"`
	UpdatedAt       time.Time      `json:"updatedAt"`
}

type StageTransition struct {
	ID              string         `json:"id"`
	DealID          string         `json:"dealId"`
	FromPipelineID  *string        `json:"fromPipelineId"`
	FromStageID     *string        `json:"fromStageId"`
	FromStageName   *string        `json:"fromStageName"`
	ToPipelineID    string         `json:"toPipelineId"`
	ToStageID       string         `json:"toStageId"`
	ToStageName     string         `json:"toStageName"`
	ActorUserID     *string        `json:"actorUserId"`
	ActorName       *string        `json:"actorName"`
	EnteredAt       *time.Time     `json:"enteredAt"`
	ExitedAt        time.Time      `json:"exitedAt"`
	DurationSeconds *int           `json:"durationSeconds"`
	Metadata        map[string]any `json:"metadata"`
	CreatedAt       time.Time      `json:"createdAt"`
}

type ActivityItem struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`
	Subject   string     `json:"subject"`
	Body      string     `json:"body"`
	Status    string     `json:"status"`
	DueAt     *time.Time `json:"dueAt"`
	ActorName *string    `json:"actorName"`
	CreatedAt time.Time  `json:"createdAt"`
}

type DocumentItem struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	DocType         string     `json:"type"`
	Category        string     `json:"category"`
	Status          string     `json:"status"`
	MimeType        string     `json:"mimeType"`
	SizeBytes       int64      `json:"sizeBytes"`
	UploadedByName  *string    `json:"uploadedByName"`
	VerifiedByName  *string    `json:"verifiedByName"`
	RequestedAt     *time.Time `json:"requestedDate"`
	UploadedAt      *time.Time `json:"uploadedDate"`
	ExpiresAt       *time.Time `json:"expiryDate"`
	CreatedAt       time.Time  `json:"createdAt"`
}

type DealDetail struct {
	Deal         *Deal             `json:"deal"`
	Customer     map[string]any    `json:"customer"`
	Transitions  []StageTransition `json:"transitions"`
	Activities   []ActivityItem    `json:"activities"`
	Documents    []DocumentItem    `json:"documents"`
	Pipeline     *pipelines.Pipeline `json:"pipeline"`
}

type BoardColumn struct {
	Stage pipelines.Stage `json:"stage"`
	Deals []Deal          `json:"deals"`
}

type Board struct {
	Pipeline *pipelines.Pipeline `json:"pipeline"`
	Columns  []BoardColumn       `json:"columns"`
}

type CreateInput struct {
	CustomerID      string         `json:"customerId"`
	Title           string         `json:"title"`
	OwnerUserID     *string        `json:"ownerUserId"`
	TeamID          *string        `json:"teamId"`
	PipelineID      *string        `json:"pipelineId"`
	StageID         *string        `json:"stageId"`
	Value           *float64       `json:"value"`
	Currency        string         `json:"currency"`
	Probability     *float64       `json:"probability"`
	ExpectedCloseAt *string        `json:"expectedCloseAt"`
	Source          string         `json:"source"`
	Priority        string         `json:"priority"`
	Notes           string         `json:"notes"`
	FieldValues     map[string]any `json:"fieldValues"`
}

type UpdateInput struct {
	Title           *string        `json:"title"`
	OwnerUserID     *string        `json:"ownerUserId"`
	TeamID          *string        `json:"teamId"`
	PipelineID      *string        `json:"pipelineId"`
	StageID         *string        `json:"stageId"`
	Value           *float64       `json:"value"`
	Currency        *string        `json:"currency"`
	Probability     *float64       `json:"probability"`
	ExpectedCloseAt *string        `json:"expectedCloseAt"`
	ClearCloseDate  bool           `json:"clearCloseDate"`
	Source          *string        `json:"source"`
	Priority        *string        `json:"priority"`
	Notes           *string        `json:"notes"`
	FieldValues     map[string]any `json:"fieldValues"`
	Status          *string        `json:"status"`
}

type MoveInput struct {
	StageID    string `json:"stageId"`
	PipelineID string `json:"pipelineId"`
	Force      bool   `json:"force"`
	LostReason string `json:"lostReason"`
}

type ListFilter struct {
	PipelineID    string
	Search        string
	OwnerID       string
	TeamID        string
	Status        string
	Limit         int
	Offset        int
	ScopeUnscoped bool
	ScopeOwnerIDs []string
	ScopeTeamIDs  []string
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const dealSelect = `
	SELECT d.id::text, d.title, d.customer_id::text, c.full_name,
		d.owner_user_id::text, ou.full_name, d.team_id::text, t.name,
		d.pipeline_id::text, p.name, d.stage_id::text, ps.name, ps.visual_accent,
		d.value, d.currency, d.probability,
		CASE WHEN d.expected_close_at IS NULL THEN NULL ELSE to_char(d.expected_close_at, 'YYYY-MM-DD') END,
		d.source, d.priority, d.status, d.lost_reason, d.notes, d.field_values,
		d.last_activity_at, d.next_activity_at, d.stage_entered_at,
		EXTRACT(DAY FROM NOW() - d.created_at)::int,
		EXTRACT(DAY FROM NOW() - d.stage_entered_at)::int,
		ps.sla_hours, d.created_at, d.updated_at
	FROM deals d
	JOIN customers c ON c.id = d.customer_id
	LEFT JOIN users ou ON ou.id = d.owner_user_id
	LEFT JOIN teams t ON t.id = d.team_id
	LEFT JOIN pipelines p ON p.id = d.pipeline_id
	LEFT JOIN pipeline_stages ps ON ps.id = d.stage_id
`

func scanDeal(row pgx.Row) (*Deal, error) {
	var d Deal
	var closeAt *string
	var fieldRaw []byte
	var slaHours *int
	err := row.Scan(
		&d.ID, &d.Title, &d.CustomerID, &d.CustomerName,
		&d.OwnerUserID, &d.OwnerName, &d.TeamID, &d.TeamName,
		&d.PipelineID, &d.PipelineName, &d.StageID, &d.StageName, &d.StageAccent,
		&d.Value, &d.Currency, &d.Probability, &closeAt,
		&d.Source, &d.Priority, &d.Status, &d.LostReason, &d.Notes, &fieldRaw,
		&d.LastActivityAt, &d.NextActivityAt, &d.StageEnteredAt,
		&d.AgeDays, &d.DaysInStage, &slaHours, &d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	d.ExpectedCloseAt = closeAt
	d.FieldValues = map[string]any{}
	if len(fieldRaw) > 0 {
		_ = json.Unmarshal(fieldRaw, &d.FieldValues)
	}
	d.Attention = computeAttention(d, slaHours)
	return &d, nil
}

func computeAttention(d Deal, slaHours *int) string {
	if d.Status == "lost" || d.Status == "won" || d.Status == "archived" {
		return ""
	}
	if d.NextActivityAt == nil {
		return "no_next_activity"
	}
	if slaHours != nil && *slaHours > 0 {
		hoursInStage := time.Since(d.StageEnteredAt).Hours()
		if hoursInStage > float64(*slaHours) {
			return "over_sla"
		}
		if hoursInStage > float64(*slaHours)*0.8 {
			return "attention_needed"
		}
	}
	const inactiveDays = 7
	ref := d.LastActivityAt
	if ref == nil {
		t := d.CreatedAt
		ref = &t
	}
	if time.Since(*ref) > time.Duration(inactiveDays)*24*time.Hour {
		return "no_recent_activity"
	}
	return ""
}

func (r *Repository) Get(ctx context.Context, id string) (*Deal, error) {
	d, err := scanDeal(r.pool.QueryRow(ctx, dealSelect+` WHERE d.id=$1`, id))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return d, err
}

func (r *Repository) List(ctx context.Context, f ListFilter) ([]Deal, int, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	args := []any{}
	where := []string{"1=1"}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.Replace(cond, "?", "$"+itoa(len(args)), 1))
	}
	if f.PipelineID != "" {
		add("d.pipeline_id::text = ?", f.PipelineID)
	}
	if f.OwnerID != "" {
		add("d.owner_user_id::text = ?", f.OwnerID)
	}
	if f.TeamID != "" {
		add("d.team_id::text = ?", f.TeamID)
	}
	if f.Status != "" && f.Status != "all" {
		add("d.status = ?", f.Status)
	} else if f.Status != "all" {
		where = append(where, "d.status = 'open'")
	}
	if f.Search != "" {
		args = append(args, "%"+strings.ToLower(f.Search)+"%")
		n := "$" + itoa(len(args))
		where = append(where, "(lower(d.title) LIKE "+n+" OR lower(c.full_name) LIKE "+n+")")
	}
	vis := datascope.Visibility{Unscoped: f.ScopeUnscoped, OwnerIDs: f.ScopeOwnerIDs, TeamIDs: f.ScopeTeamIDs}
	where, args = datascope.AppendWhere(where, args, vis, datascope.Columns{Owner: "d.owner_user_id", Team: "d.team_id"})
	whereSQL := strings.Join(where, " AND ")
	var total int
	countSQL := `SELECT COUNT(*) FROM deals d JOIN customers c ON c.id = d.customer_id WHERE ` + whereSQL
	if err := r.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, f.Limit, f.Offset)
	limitPh := "$" + itoa(len(args)-1)
	offsetPh := "$" + itoa(len(args))
	rows, err := r.pool.Query(ctx, dealSelect+` WHERE `+whereSQL+` ORDER BY d.updated_at DESC LIMIT `+limitPh+` OFFSET `+offsetPh, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var items []Deal
	for rows.Next() {
		d, err := scanDeal(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *d)
	}
	if items == nil {
		items = []Deal{}
	}
	return items, total, rows.Err()
}

func (r *Repository) Board(ctx context.Context, pipelineID string) ([]Deal, error) {
	rows, err := r.pool.Query(ctx, dealSelect+`
		WHERE d.pipeline_id::text = $1 AND d.status IN ('open', 'won', 'lost')
		ORDER BY d.stage_entered_at ASC, d.created_at ASC
	`, pipelineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Deal
	for rows.Next() {
		d, err := scanDeal(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *d)
	}
	if items == nil {
		items = []Deal{}
	}
	return items, rows.Err()
}

func (r *Repository) Create(ctx context.Context, in CreateInput, closeDate *time.Time, fieldJSON []byte) (*Deal, error) {
	id := uuid.NewString()
	currency := in.Currency
	if currency == "" {
		currency = "AUD"
	}
	priority := in.Priority
	if priority == "" {
		priority = "medium"
	}
	if fieldJSON == nil {
		fieldJSON = []byte("{}")
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO deals (
			id, customer_id, title, owner_user_id, team_id, pipeline_id, stage_id,
			value, currency, probability, expected_close_at, source, priority, notes,
			field_values, stage_entered_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::jsonb, NOW())
	`, id, in.CustomerID, strings.TrimSpace(in.Title), emptyToNil(in.OwnerUserID), emptyToNil(in.TeamID),
		emptyToNil(in.PipelineID), emptyToNil(in.StageID), in.Value, currency, in.Probability, closeDate,
		in.Source, priority, in.Notes, string(fieldJSON))
	if err != nil {
		return nil, err
	}
	if in.StageID != nil && *in.StageID != "" {
		_, _ = r.pool.Exec(ctx, `
			INSERT INTO deal_stage_transitions (
				id, deal_id, from_pipeline_id, from_stage_id, to_pipeline_id, to_stage_id,
				entered_at, exited_at, duration_seconds, metadata
			) VALUES ($1,$2,NULL,NULL,$3,$4,NULL,NOW(),0,'{"kind":"created"}'::jsonb)
		`, uuid.NewString(), id, nullStr(in.PipelineID), *in.StageID)
	}
	return r.Get(ctx, id)
}

func (r *Repository) Update(ctx context.Context, id string, current *Deal, in UpdateInput, closeDate *time.Time, setClose bool, fieldJSON []byte) (*Deal, error) {
	title := current.Title
	if in.Title != nil {
		title = strings.TrimSpace(*in.Title)
	}
	owner := current.OwnerUserID
	if in.OwnerUserID != nil {
		owner = emptyToNil(in.OwnerUserID)
	}
	team := current.TeamID
	if in.TeamID != nil {
		team = emptyToNil(in.TeamID)
	}
	pipeline := current.PipelineID
	if in.PipelineID != nil {
		pipeline = emptyToNil(in.PipelineID)
	}
	stage := current.StageID
	if in.StageID != nil {
		stage = emptyToNil(in.StageID)
	}
	value := current.Value
	if in.Value != nil {
		value = in.Value
	}
	currency := current.Currency
	if in.Currency != nil && *in.Currency != "" {
		currency = *in.Currency
	}
	prob := current.Probability
	if in.Probability != nil {
		prob = in.Probability
	}
	source := current.Source
	if in.Source != nil {
		source = *in.Source
	}
	priority := current.Priority
	if in.Priority != nil && *in.Priority != "" {
		priority = *in.Priority
	}
	notes := current.Notes
	if in.Notes != nil {
		notes = *in.Notes
	}
	status := current.Status
	if in.Status != nil && *in.Status != "" {
		status = *in.Status
	}
	var expected any
	if setClose {
		expected = closeDate
	} else if current.ExpectedCloseAt != nil {
		expected = *current.ExpectedCloseAt
	}
	if fieldJSON == nil {
		b, _ := json.Marshal(current.FieldValues)
		fieldJSON = b
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE deals SET
			title=$2, owner_user_id=$3, team_id=$4, pipeline_id=$5, stage_id=$6,
			value=$7, currency=$8, probability=$9, expected_close_at=$10::date,
			source=$11, priority=$12, notes=$13, status=$14, field_values=$15::jsonb
		WHERE id=$1
	`, id, title, owner, team, pipeline, stage, value, currency, prob, expected, source, priority, notes, status, string(fieldJSON))
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *Repository) MoveTransactional(
	ctx context.Context,
	actorID string,
	deal *Deal,
	toPipelineID, toStageID string,
	toStage *pipelines.Stage,
	prob *float64,
	lostReasonIn string,
) (*Deal, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	now := time.Now().UTC()
	var duration *int
	entered := deal.StageEnteredAt
	secs := int(now.Sub(entered).Seconds())
	duration = &secs

	status := deal.Status
	lostReason := deal.LostReason
	if toStage.IsWon {
		status = "won"
		lostReason = ""
	} else if toStage.IsLost {
		status = "lost"
		if reason := strings.TrimSpace(lostReasonIn); reason != "" {
			lostReason = reason
		}
	} else if status == "won" || status == "lost" {
		status = "open"
		lostReason = ""
	}

	_, err = tx.Exec(ctx, `
		UPDATE deals SET
			pipeline_id=$2, stage_id=$3, probability=$4, status=$5, stage_entered_at=$6, lost_reason=$7
		WHERE id=$1
	`, deal.ID, toPipelineID, toStageID, prob, status, now, lostReason)
	if err != nil {
		return nil, err
	}

	meta, _ := json.Marshal(map[string]any{
		"fromStageId": deal.StageID,
		"toStageId":   toStageID,
		"toStageName": toStage.Name,
		"lostReason":  lostReason,
	})
	_, err = tx.Exec(ctx, `
		INSERT INTO deal_stage_transitions (
			id, deal_id, from_pipeline_id, from_stage_id, to_pipeline_id, to_stage_id,
			actor_user_id, entered_at, exited_at, duration_seconds, metadata
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb)
	`, uuid.NewString(), deal.ID, nullStr(deal.PipelineID), nullStr(deal.StageID),
		toPipelineID, toStageID, actorID, entered, now, duration, string(meta))
	if err != nil {
		return nil, err
	}

	fromName := ""
	if deal.StageName != nil {
		fromName = *deal.StageName
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO activities (id, kind, subject, body, status, completed_at, actor_user_id, deal_id, customer_id, metadata)
		VALUES ($1,'stage_change',$2,$3,'completed',NOW(),$4,$5,$6,$7::jsonb)
	`, uuid.NewString(),
		"Stage changed",
		"Moved from "+fromName+" to "+toStage.Name,
		actorID, deal.ID, deal.CustomerID, string(meta))
	if err != nil {
		return nil, err
	}

	_, _ = tx.Exec(ctx, `UPDATE deals SET last_activity_at = NOW() WHERE id=$1`, deal.ID)

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.Get(ctx, deal.ID)
}

func (r *Repository) ListTransitions(ctx context.Context, dealID string) ([]StageTransition, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT t.id::text, t.deal_id::text,
			t.from_pipeline_id::text, t.from_stage_id::text, fs.name,
			t.to_pipeline_id::text, t.to_stage_id::text, ts.name,
			t.actor_user_id::text, u.full_name,
			t.entered_at, t.exited_at, t.duration_seconds, t.metadata, t.created_at
		FROM deal_stage_transitions t
		LEFT JOIN pipeline_stages fs ON fs.id = t.from_stage_id
		LEFT JOIN pipeline_stages ts ON ts.id = t.to_stage_id
		LEFT JOIN users u ON u.id = t.actor_user_id
		WHERE t.deal_id = $1
		ORDER BY t.created_at DESC
	`, dealID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []StageTransition
	for rows.Next() {
		var t StageTransition
		var meta []byte
		if err := rows.Scan(
			&t.ID, &t.DealID, &t.FromPipelineID, &t.FromStageID, &t.FromStageName,
			&t.ToPipelineID, &t.ToStageID, &t.ToStageName, &t.ActorUserID, &t.ActorName,
			&t.EnteredAt, &t.ExitedAt, &t.DurationSeconds, &meta, &t.CreatedAt,
		); err != nil {
			return nil, err
		}
		t.Metadata = map[string]any{}
		_ = json.Unmarshal(meta, &t.Metadata)
		items = append(items, t)
	}
	if items == nil {
		items = []StageTransition{}
	}
	return items, rows.Err()
}

func (r *Repository) ListActivities(ctx context.Context, dealID string) ([]ActivityItem, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT a.id::text, a.kind, a.subject, a.body, a.status, a.due_at, u.full_name, a.created_at
		FROM activities a
		LEFT JOIN users u ON u.id = a.actor_user_id
		WHERE a.deal_id = $1
		ORDER BY a.created_at DESC
		LIMIT 100
	`, dealID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ActivityItem
	for rows.Next() {
		var a ActivityItem
		if err := rows.Scan(&a.ID, &a.Kind, &a.Subject, &a.Body, &a.Status, &a.DueAt, &a.ActorName, &a.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	if items == nil {
		items = []ActivityItem{}
	}
	return items, rows.Err()
}

func (r *Repository) ListDocuments(ctx context.Context, dealID string) ([]DocumentItem, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT d.id::text, d.name, d.doc_type, d.category, d.status, d.mime_type, d.size_bytes,
			ub.full_name, vb.full_name, d.requested_at, d.uploaded_at, d.expires_at, d.created_at
		FROM documents d
		LEFT JOIN users ub ON ub.id = d.uploaded_by
		LEFT JOIN users vb ON vb.id = d.verified_by
		WHERE d.deal_id = $1
		ORDER BY COALESCE(d.uploaded_at, d.requested_at, d.created_at) DESC
	`, dealID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []DocumentItem
	for rows.Next() {
		var d DocumentItem
		if err := rows.Scan(&d.ID, &d.Name, &d.DocType, &d.Category, &d.Status, &d.MimeType, &d.SizeBytes,
			&d.UploadedByName, &d.VerifiedByName, &d.RequestedAt, &d.UploadedAt, &d.ExpiresAt, &d.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	if items == nil {
		items = []DocumentItem{}
	}
	return items, rows.Err()
}

func (r *Repository) CustomerSnapshot(ctx context.Context, customerID string) (map[string]any, error) {
	var id, name string
	var email, phone *string
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, full_name, email, phone FROM customers WHERE id=$1
	`, customerID).Scan(&id, &name, &email, &phone)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id": id, "fullName": name, "email": email, "phone": phone,
	}, nil
}

func (r *Repository) MissingRequiredActivities(ctx context.Context, dealID string, kinds []string) ([]string, error) {
	missing := []string{}
	for _, kind := range kinds {
		var ok bool
		err := r.pool.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM activities
				WHERE deal_id=$1 AND kind=$2 AND status='completed'
			)
		`, dealID, kind).Scan(&ok)
		if err != nil {
			return nil, err
		}
		if !ok {
			missing = append(missing, kind)
		}
	}
	return missing, nil
}

func (r *Repository) MissingRequiredDocuments(ctx context.Context, dealID string, categories []string) ([]string, error) {
	missing := []string{}
	for _, cat := range categories {
		var ok bool
		err := r.pool.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM documents
				WHERE deal_id=$1
				  AND (lower(category)=lower($2) OR lower(doc_type)=lower($2))
				  AND status IN ('uploaded','verified')
			)
		`, dealID, cat).Scan(&ok)
		if err != nil {
			return nil, err
		}
		if !ok {
			missing = append(missing, cat)
		}
	}
	return missing, nil
}

func (r *Repository) AddDocument(ctx context.Context, dealID, customerID, name, category, actorID string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO documents (id, name, category, uploaded_by, deal_id, customer_id)
		VALUES ($1,$2,$3,$4,$5,$6)
	`, uuid.NewString(), name, category, actorID, dealID, customerID)
	return err
}

func emptyToNil(v *string) *string {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil
	}
	return v
}

func nullStr(v *string) any {
	if v == nil || *v == "" {
		return nil
	}
	return *v
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
