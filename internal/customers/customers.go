package customers

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/crm/backend/internal/datascope"
	"github.com/crm/backend/internal/leads"
	"github.com/crm/backend/internal/referrals"
)

type Customer struct {
	ID                   string     `json:"id"`
	FullName             string     `json:"fullName"`
	Email                *string    `json:"email"`
	Phone                *string    `json:"phone"`
	Country              string     `json:"country"`
	Nationality          string     `json:"nationality"`
	Location             string     `json:"location"`
	OwnerUserID          *string    `json:"ownerUserId"`
	OwnerName            *string    `json:"ownerName"`
	TeamID               *string    `json:"teamId"`
	TeamName             *string    `json:"teamName"`
	Source               string     `json:"source"`
	Priority             string     `json:"priority"`
	Tags                 []string   `json:"tags"`
	AnzscoID             *string    `json:"anzscoId"`
	AnzscoCode           *string    `json:"anzscoCode"`
	AnzscoTitle          *string    `json:"anzscoTitle"`
	Occupation           string     `json:"occupation"`
	JobTitle             string     `json:"jobTitle"`
	Employer             string     `json:"employer"`
	ExperienceYears      *float64   `json:"experienceYears"`
	Qualification        string     `json:"qualification"`
	Skills               []string   `json:"skills"`
	PipelineID           *string    `json:"pipelineId"`
	PipelineName         *string    `json:"pipelineName"`
	StageID              *string    `json:"stageId"`
	StageName            *string    `json:"stageName"`
	PotentialValue       *float64   `json:"potentialValue"`
	ExpectedOutcome      string     `json:"expectedOutcome"`
	LastContactedAt      *time.Time `json:"lastContactedAt"`
	NextFollowUpAt       *time.Time `json:"nextFollowUpAt"`
	Notes                string     `json:"notes"`
	ConvertedFromLeadID  *string    `json:"convertedFromLeadId"`
	IsArchived           bool       `json:"isArchived"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
	DealCount            int        `json:"dealCount,omitempty"`
}

type Profile360 struct {
	Customer   *Customer           `json:"customer"`
	Deals      []DealSummary       `json:"deals"`
	Activities []ActivityItem      `json:"activities"`
	Documents  []DocumentItem      `json:"documents"`
	Referral   *referrals.Referral `json:"referral"`
}

type DealSummary struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Value        *float64  `json:"value"`
	Currency     string    `json:"currency"`
	Status       string    `json:"status"`
	StageName    *string   `json:"stageName"`
	PipelineName *string   `json:"pipelineName"`
	OwnerName    *string   `json:"ownerName"`
	CreatedAt    time.Time `json:"createdAt"`
}

type ActivityItem struct {
	ID          string         `json:"id"`
	Kind        string         `json:"kind"`
	Subject     string         `json:"subject"`
	Body        string         `json:"body"`
	Status      string         `json:"status"`
	DueAt       *time.Time     `json:"dueAt"`
	ActorName   *string        `json:"actorName"`
	Metadata    map[string]any `json:"metadata"`
	CreatedAt   time.Time      `json:"createdAt"`
}

type DocumentItem struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	DocType        string     `json:"type"`
	Category       string     `json:"category"`
	Status         string     `json:"status"`
	MimeType       string     `json:"mimeType"`
	SizeBytes      int64      `json:"sizeBytes"`
	UploadedByName *string    `json:"uploadedByName"`
	VerifiedByName *string    `json:"verifiedByName"`
	RequestedAt    *time.Time `json:"requestedDate"`
	UploadedAt     *time.Time `json:"uploadedDate"`
	ExpiresAt      *time.Time `json:"expiryDate"`
	CreatedAt      time.Time  `json:"createdAt"`
}

type CreateInput struct {
	FullName        string   `json:"fullName"`
	Email           *string  `json:"email"`
	Phone           *string  `json:"phone"`
	Country         string   `json:"country"`
	Nationality     string   `json:"nationality"`
	Location        string   `json:"location"`
	OwnerUserID     *string  `json:"ownerUserId"`
	TeamID          *string  `json:"teamId"`
	Source          string   `json:"source"`
	Priority        string   `json:"priority"`
	Tags            []string `json:"tags"`
	AnzscoID        *string  `json:"anzscoId"`
	Occupation      string   `json:"occupation"`
	JobTitle        string   `json:"jobTitle"`
	Employer        string   `json:"employer"`
	ExperienceYears *float64 `json:"experienceYears"`
	Qualification   string   `json:"qualification"`
	Skills          []string `json:"skills"`
	PipelineID      *string  `json:"pipelineId"`
	StageID         *string  `json:"stageId"`
	PotentialValue  *float64 `json:"potentialValue"`
	ExpectedOutcome string   `json:"expectedOutcome"`
	Notes           string   `json:"notes"`
	ForceCreate     bool             `json:"forceCreate"`
	CustomFields    map[string]any   `json:"customFields"`
	Referral        *referrals.Input `json:"referral"`
}

type UpdateInput struct {
	FullName        *string  `json:"fullName"`
	Email           *string  `json:"email"`
	Phone           *string  `json:"phone"`
	Country         *string  `json:"country"`
	Nationality     *string  `json:"nationality"`
	Location        *string  `json:"location"`
	OwnerUserID     *string  `json:"ownerUserId"`
	TeamID          *string  `json:"teamId"`
	Source          *string  `json:"source"`
	Priority        *string  `json:"priority"`
	Tags            []string `json:"tags"`
	AnzscoID        *string  `json:"anzscoId"`
	Occupation      *string  `json:"occupation"`
	JobTitle        *string  `json:"jobTitle"`
	Employer        *string  `json:"employer"`
	ExperienceYears *float64 `json:"experienceYears"`
	Qualification   *string  `json:"qualification"`
	Skills          []string `json:"skills"`
	PipelineID      *string  `json:"pipelineId"`
	StageID         *string  `json:"stageId"`
	PotentialValue  *float64 `json:"potentialValue"`
	ExpectedOutcome *string  `json:"expectedOutcome"`
	Notes           *string  `json:"notes"`
	NextFollowUpAt  *string        `json:"nextFollowUpAt"`
	ForceUpdate     bool           `json:"forceUpdate"`
	CustomFields    map[string]any `json:"customFields"`
}

type ListFilter struct {
	Search        string
	OwnerUserID   string
	TeamID        string
	Source        string
	Country       string
	CreatedFrom   string
	CreatedTo     string
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

const customerSelect = `
	SELECT c.id::text, c.full_name, c.email, c.phone, c.country, c.nationality, c.location,
		c.owner_user_id::text, ou.full_name, c.team_id::text, t.name,
		c.source, c.priority, c.tags, c.anzsco_id::text, a.code, a.title,
		c.occupation, c.job_title, c.employer, c.experience_years, c.qualification, c.skills,
		c.pipeline_id::text, p.name, c.stage_id::text, ps.name,
		c.potential_value, c.expected_outcome, c.last_contacted_at, c.next_follow_up_at,
		c.notes, c.converted_from_lead_id::text, c.is_archived, c.created_at, c.updated_at
	FROM customers c
	LEFT JOIN users ou ON ou.id = c.owner_user_id
	LEFT JOIN teams t ON t.id = c.team_id
	LEFT JOIN anzsco_occupations a ON a.id = c.anzsco_id
	LEFT JOIN pipelines p ON p.id = c.pipeline_id
	LEFT JOIN pipeline_stages ps ON ps.id = c.stage_id
`

func scanCustomer(row pgx.Row) (*Customer, error) {
	var c Customer
	var tags, skills []string
	err := row.Scan(
		&c.ID, &c.FullName, &c.Email, &c.Phone, &c.Country, &c.Nationality, &c.Location,
		&c.OwnerUserID, &c.OwnerName, &c.TeamID, &c.TeamName,
		&c.Source, &c.Priority, &tags, &c.AnzscoID, &c.AnzscoCode, &c.AnzscoTitle,
		&c.Occupation, &c.JobTitle, &c.Employer, &c.ExperienceYears, &c.Qualification, &skills,
		&c.PipelineID, &c.PipelineName, &c.StageID, &c.StageName,
		&c.PotentialValue, &c.ExpectedOutcome, &c.LastContactedAt, &c.NextFollowUpAt,
		&c.Notes, &c.ConvertedFromLeadID, &c.IsArchived, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if tags == nil {
		tags = []string{}
	}
	if skills == nil {
		skills = []string{}
	}
	c.Tags = tags
	c.Skills = skills
	return &c, nil
}

func (r *Repository) Get(ctx context.Context, id string) (*Customer, error) {
	c, err := scanCustomer(r.pool.QueryRow(ctx, customerSelect+` WHERE c.id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

func (r *Repository) List(ctx context.Context, f ListFilter) ([]Customer, int, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 25
	}
	where := []string{"c.is_archived=FALSE", "($1='' OR c.full_name ILIKE '%'||$1||'%' OR COALESCE(c.email,'') ILIKE '%'||$1||'%')"}
	args := []any{f.Search}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.Replace(cond, "?", "$"+strconv.Itoa(len(args)), 1))
	}
	if f.OwnerUserID != "" {
		add("c.owner_user_id::text = ?", f.OwnerUserID)
	}
	if f.TeamID != "" {
		add("c.team_id::text = ?", f.TeamID)
	}
	if f.Source != "" {
		add("c.source = ?", f.Source)
	}
	if f.Country != "" {
		add("c.country = ?", f.Country)
	}
	if f.CreatedFrom != "" {
		add("c.created_at >= ?::timestamptz", f.CreatedFrom)
	}
	if f.CreatedTo != "" {
		add("c.created_at < (?::date + INTERVAL '1 day')", f.CreatedTo)
	}
	vis := datascope.Visibility{Unscoped: f.ScopeUnscoped, OwnerIDs: f.ScopeOwnerIDs, TeamIDs: f.ScopeTeamIDs}
	where, args = datascope.AppendWhere(where, args, vis, datascope.Columns{Owner: "c.owner_user_id", Team: "c.team_id"})
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM customers c WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, f.Limit, f.Offset)
	lim, off := len(args)-1, len(args)
	rows, err := r.pool.Query(ctx, customerSelect+`
		WHERE `+whereSQL+`
		ORDER BY c.created_at DESC LIMIT $`+strconv.Itoa(lim)+` OFFSET $`+strconv.Itoa(off), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var items []Customer
	for rows.Next() {
		c, err := scanCustomer(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *c)
	}
	if items == nil {
		items = []Customer{}
	}
	return items, total, rows.Err()
}

func (r *Repository) CreateFromLead(ctx context.Context, lead *leads.Lead) (*Customer, error) {
	id := uuid.NewString()
	tags := lead.Tags
	if tags == nil {
		tags = []string{}
	}
	skills := lead.Skills
	if skills == nil {
		skills = []string{}
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO customers (
			id, full_name, email, phone, country, nationality, location,
			owner_user_id, team_id, source, priority, tags, anzsco_id,
			occupation, job_title, employer, experience_years, qualification, skills,
			pipeline_id, stage_id, potential_value, expected_outcome, notes,
			converted_from_lead_id, last_contacted_at, next_follow_up_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27
		)
	`, id, lead.FullName, lead.Email, lead.Phone, lead.Country, lead.Nationality, lead.Location,
		lead.OwnerUserID, lead.TeamID, lead.Source, lead.Priority, tags, lead.AnzscoID,
		lead.Occupation, lead.JobTitle, lead.Employer, lead.ExperienceYears, lead.Qualification, skills,
		nil, nil, lead.PotentialValue, lead.ExpectedOutcome, lead.Notes,
		lead.ID, lead.LastActivityAt, lead.NextActivityAt)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *Repository) Create(ctx context.Context, in CreateInput) (*Customer, error) {
	id := uuid.NewString()
	priority := in.Priority
	if priority == "" {
		priority = "medium"
	}
	tags := in.Tags
	if tags == nil {
		tags = []string{}
	}
	skills := in.Skills
	if skills == nil {
		skills = []string{}
	}
	email := normalizeEmail(in.Email)
	phone := normalizePhone(in.Phone)
	_, err := r.pool.Exec(ctx, `
		INSERT INTO customers (
			id, full_name, email, phone, country, nationality, location,
			owner_user_id, team_id, source, priority, tags, anzsco_id,
			occupation, job_title, employer, experience_years, qualification, skills,
			pipeline_id, stage_id, potential_value, expected_outcome, notes
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24
		)
	`, id, strings.TrimSpace(in.FullName), email, phone, in.Country, in.Nationality, in.Location,
		emptyToNil(in.OwnerUserID), emptyToNil(in.TeamID), in.Source, priority, tags, emptyToNil(in.AnzscoID),
		in.Occupation, in.JobTitle, in.Employer, in.ExperienceYears, in.Qualification, skills,
		emptyToNil(in.PipelineID), emptyToNil(in.StageID), in.PotentialValue, in.ExpectedOutcome, in.Notes)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *Repository) Update(ctx context.Context, id string, current *Customer, in UpdateInput, nextFollow *time.Time, setNext bool) (*Customer, error) {
	fullName := current.FullName
	if in.FullName != nil {
		fullName = strings.TrimSpace(*in.FullName)
	}
	email := current.Email
	if in.Email != nil {
		email = normalizeEmail(in.Email)
	}
	phone := current.Phone
	if in.Phone != nil {
		phone = normalizePhone(in.Phone)
	}
	owner := current.OwnerUserID
	if in.OwnerUserID != nil {
		owner = emptyToNil(in.OwnerUserID)
	}
	team := current.TeamID
	if in.TeamID != nil {
		team = emptyToNil(in.TeamID)
	}
	anzsco := current.AnzscoID
	if in.AnzscoID != nil {
		anzsco = emptyToNil(in.AnzscoID)
	}
	pipeline := current.PipelineID
	if in.PipelineID != nil {
		pipeline = emptyToNil(in.PipelineID)
	}
	stage := current.StageID
	if in.StageID != nil {
		stage = emptyToNil(in.StageID)
	}
	tags := current.Tags
	if in.Tags != nil {
		tags = in.Tags
	}
	skills := current.Skills
	if in.Skills != nil {
		skills = in.Skills
	}
	next := current.NextFollowUpAt
	if setNext {
		next = nextFollow
	}

	_, err := r.pool.Exec(ctx, `
		UPDATE customers SET
			full_name=$2, email=$3, phone=$4, country=$5, nationality=$6, location=$7,
			owner_user_id=$8, team_id=$9, source=$10, priority=$11, tags=$12, anzsco_id=$13,
			occupation=$14, job_title=$15, employer=$16, experience_years=$17, qualification=$18, skills=$19,
			pipeline_id=$20, stage_id=$21, potential_value=$22, expected_outcome=$23, notes=$24,
			next_follow_up_at=$25
		WHERE id=$1
	`, id, fullName, email, phone,
		pick(in.Country, current.Country), pick(in.Nationality, current.Nationality), pick(in.Location, current.Location),
		owner, team, pick(in.Source, current.Source), pick(in.Priority, current.Priority), tags, anzsco,
		pick(in.Occupation, current.Occupation), pick(in.JobTitle, current.JobTitle), pick(in.Employer, current.Employer),
		coalesceFloat(in.ExperienceYears, current.ExperienceYears), pick(in.Qualification, current.Qualification), skills,
		pipeline, stage, coalesceFloat(in.PotentialValue, current.PotentialValue),
		pick(in.ExpectedOutcome, current.ExpectedOutcome), pick(in.Notes, current.Notes), next)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *Repository) ListDeals(ctx context.Context, customerID string) ([]DealSummary, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT d.id::text, d.title, d.value, d.currency, d.status, ps.name, p.name, ou.full_name, d.created_at
		FROM deals d
		LEFT JOIN pipeline_stages ps ON ps.id = d.stage_id
		LEFT JOIN pipelines p ON p.id = d.pipeline_id
		LEFT JOIN users ou ON ou.id = d.owner_user_id
		WHERE d.customer_id = $1
		ORDER BY d.created_at DESC
	`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []DealSummary
	for rows.Next() {
		var d DealSummary
		if err := rows.Scan(&d.ID, &d.Title, &d.Value, &d.Currency, &d.Status, &d.StageName, &d.PipelineName, &d.OwnerName, &d.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	if items == nil {
		items = []DealSummary{}
	}
	return items, rows.Err()
}

func (r *Repository) ListActivities(ctx context.Context, customerID string, limit int) ([]ActivityItem, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT a.id::text, a.kind, a.subject, a.body, a.status, a.due_at, u.full_name, a.metadata, a.created_at
		FROM activities a
		LEFT JOIN users u ON u.id = a.actor_user_id
		WHERE a.customer_id = $1
		ORDER BY a.created_at DESC
		LIMIT $2
	`, customerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ActivityItem
	for rows.Next() {
		var a ActivityItem
		var meta []byte
		if err := rows.Scan(&a.ID, &a.Kind, &a.Subject, &a.Body, &a.Status, &a.DueAt, &a.ActorName, &meta, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Metadata = map[string]any{}
		if len(meta) > 0 {
			_ = json.Unmarshal(meta, &a.Metadata)
		}
		items = append(items, a)
	}
	if items == nil {
		items = []ActivityItem{}
	}
	return items, rows.Err()
}

func (r *Repository) ListDocuments(ctx context.Context, customerID string) ([]DocumentItem, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT d.id::text, d.name, d.doc_type, d.category, d.status, d.mime_type, d.size_bytes,
			ub.full_name, vb.full_name, d.requested_at, d.uploaded_at, d.expires_at, d.created_at
		FROM documents d
		LEFT JOIN users ub ON ub.id = d.uploaded_by
		LEFT JOIN users vb ON vb.id = d.verified_by
		WHERE d.customer_id=$1
		ORDER BY COALESCE(d.uploaded_at, d.requested_at, d.created_at) DESC
	`, customerID)
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

func normalizeEmail(v *string) *string {
	if v == nil {
		return nil
	}
	s := strings.ToLower(strings.TrimSpace(*v))
	if s == "" {
		return nil
	}
	return &s
}

func normalizePhone(v *string) *string {
	if v == nil {
		return nil
	}
	s := strings.TrimSpace(*v)
	if s == "" {
		return nil
	}
	return &s
}

func emptyToNil(v *string) *string {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil
	}
	return v
}

func pick(ptr *string, fallback string) string {
	if ptr == nil {
		return fallback
	}
	return *ptr
}

func coalesceFloat(ptr, fallback *float64) *float64 {
	if ptr != nil {
		return ptr
	}
	return fallback
}
