package activities

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/crm/backend/internal/audit"
	"github.com/crm/backend/internal/automation"
	"github.com/crm/backend/internal/datascope"
	"github.com/crm/backend/internal/systemactivity"
	"github.com/crm/backend/internal/timeline"
)

type ActivityType struct {
	ID                string `json:"id"`
	Code              string `json:"code"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	Color             string `json:"color"`
	Icon              string `json:"icon"`
	IsSystem          bool   `json:"isSystem"`
	IsActive          bool   `json:"isActive"`
	AllowsExternal    bool   `json:"allowsExternal"`
	Position          int    `json:"position"`
	RequiresDatetime  bool   `json:"requiresDatetime"`
	RequiresDuration  bool   `json:"requiresDuration"`
	RequiresOutcome   bool   `json:"requiresOutcome"`
	RequiresNotes     bool   `json:"requiresNotes"`
}

type Activity struct {
	ID               string         `json:"id"`
	Title            string         `json:"title"`
	Kind             string         `json:"kind"` // type code
	TypeID           *string        `json:"typeId"`
	TypeName         *string        `json:"typeName"`
	TypeColor        *string        `json:"typeColor"`
	Status           string         `json:"status"`
	DisplayStatus    string         `json:"displayStatus"`
	Priority         string         `json:"priority"`
	Notes            string         `json:"notes"`
	Outcome          string         `json:"outcome"`
	OwnerUserID      *string        `json:"ownerUserId"`
	OwnerName        *string        `json:"ownerName"`
	CreatedByUserID  *string        `json:"createdByUserId"`
	CreatedByName    *string        `json:"createdByName"`
	CompletedByUserID *string       `json:"completedByUserId"`
	CompletedByName  *string        `json:"completedByName"`
	LeadID           *string        `json:"leadId"`
	LeadName         *string        `json:"leadName"`
	CustomerID       *string        `json:"customerId"`
	CustomerName     *string        `json:"customerName"`
	DealID           *string        `json:"dealId"`
	DealTitle        *string        `json:"dealTitle"`
	PipelineID       *string        `json:"pipelineId"`
	PipelineName     *string        `json:"pipelineName"`
	StartAt          *time.Time     `json:"startAt"`
	EndAt            *time.Time     `json:"endAt"`
	DueAt            *time.Time     `json:"dueAt"`
	CompletedAt      *time.Time     `json:"completedAt"`
	ExternalProvider string         `json:"externalProvider"`
	ExternalID       string         `json:"externalId"`
	ExternalThreadID string         `json:"externalThreadId"`
	Metadata         map[string]any `json:"metadata"`
	CreatedAt        time.Time      `json:"createdAt"`
	UpdatedAt        time.Time      `json:"updatedAt"`
}

type ActivityDetail struct {
	Activity *Activity     `json:"activity"`
	Context  *CRMContext   `json:"context"`
}

type CRMContext struct {
	CustomerID   *string `json:"customerId"`
	CustomerName *string `json:"customerName"`
	DealID       *string `json:"dealId"`
	DealTitle    *string `json:"dealTitle"`
	LeadID       *string `json:"leadId"`
	LeadName     *string `json:"leadName"`
	TimelineHref string  `json:"timelineHref"`
}

type FollowUpIntel struct {
	LastActivityAt       *time.Time `json:"lastActivityAt"`
	NextActivityAt       *time.Time `json:"nextActivityAt"`
	DaysSinceContact     *int       `json:"daysSinceContact"`
	OverdueDurationHours *int       `json:"overdueDurationHours"`
	DaysInStage          *int       `json:"daysInStage"`
	StageEnteredAt       *time.Time `json:"stageEnteredAt"`
	OverdueCount         int        `json:"overdueCount"`
	UpcomingCount        int        `json:"upcomingCount"`
}

type CreateInput struct {
	Title            string         `json:"title"`
	Subject          string         `json:"subject"` // alias for title
	Kind             string         `json:"kind"`
	TypeCode         string         `json:"typeCode"`
	Body             string         `json:"body"`
	Notes            string         `json:"notes"`
	Outcome          string         `json:"outcome"`
	Status           string         `json:"status"`
	Priority         string         `json:"priority"`
	OwnerUserID      *string        `json:"ownerUserId"`
	LeadID           *string        `json:"leadId"`
	CustomerID       *string        `json:"customerId"`
	DealID           *string        `json:"dealId"`
	StartAt          *string        `json:"startAt"`
	EndAt            *string        `json:"endAt"`
	DueAt            *string        `json:"dueAt"`
	ExternalProvider string         `json:"externalProvider"`
	ExternalID       string         `json:"externalId"`
	ExternalThreadID string         `json:"externalThreadId"`
	Metadata         map[string]any `json:"metadata"`
}

type UpdateInput struct {
	Title            *string `json:"title"`
	Kind             *string `json:"kind"`
	TypeCode         *string `json:"typeCode"`
	Notes            *string `json:"notes"`
	Outcome          *string `json:"outcome"`
	Status           *string `json:"status"`
	Priority         *string `json:"priority"`
	OwnerUserID      *string `json:"ownerUserId"`
	LeadID           *string `json:"leadId"`
	CustomerID       *string `json:"customerId"`
	DealID           *string `json:"dealId"`
	StartAt          *string `json:"startAt"`
	EndAt            *string `json:"endAt"`
	DueAt            *string `json:"dueAt"`
	ClearDueAt       bool    `json:"clearDueAt"`
	ExternalProvider *string `json:"externalProvider"`
	ExternalID       *string `json:"externalId"`
	ExternalThreadID *string `json:"externalThreadId"`
}

type ListFilter struct {
	LeadID        string
	CustomerID    string
	DealID        string
	OwnerUserID   string
	TeamID        string
	TypeCode      string
	PipelineID    string
	Status        string
	From          *time.Time
	To            *time.Time
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

const activitySelect = `
	SELECT a.id::text, COALESCE(NULLIF(a.title,''), a.subject), a.kind,
		a.activity_type_id::text, t.name, t.color,
		a.status, a.priority, a.body, a.outcome,
		a.owner_user_id::text, ou.full_name,
		a.created_by_user_id::text, cb.full_name,
		a.completed_by_user_id::text, cp.full_name,
		a.lead_id::text, l.full_name,
		a.customer_id::text, c.full_name,
		a.deal_id::text, d.title,
		d.pipeline_id::text, p.name,
		a.start_at, a.end_at, a.due_at, a.completed_at,
		a.external_provider, a.external_id, a.external_thread_id,
		a.metadata, a.created_at, a.updated_at
	FROM activities a
	LEFT JOIN activity_types t ON t.id = a.activity_type_id
	LEFT JOIN users ou ON ou.id = a.owner_user_id
	LEFT JOIN users cb ON cb.id = a.created_by_user_id
	LEFT JOIN users cp ON cp.id = a.completed_by_user_id
	LEFT JOIN leads l ON l.id = a.lead_id
	LEFT JOIN customers c ON c.id = a.customer_id
	LEFT JOIN deals d ON d.id = a.deal_id
	LEFT JOIN pipelines p ON p.id = d.pipeline_id
`

func scanActivity(row pgx.Row) (*Activity, error) {
	var a Activity
	var meta []byte
	var notes string
	err := row.Scan(
		&a.ID, &a.Title, &a.Kind, &a.TypeID, &a.TypeName, &a.TypeColor,
		&a.Status, &a.Priority, &notes, &a.Outcome,
		&a.OwnerUserID, &a.OwnerName, &a.CreatedByUserID, &a.CreatedByName,
		&a.CompletedByUserID, &a.CompletedByName,
		&a.LeadID, &a.LeadName, &a.CustomerID, &a.CustomerName,
		&a.DealID, &a.DealTitle, &a.PipelineID, &a.PipelineName,
		&a.StartAt, &a.EndAt, &a.DueAt, &a.CompletedAt,
		&a.ExternalProvider, &a.ExternalID, &a.ExternalThreadID,
		&meta, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	a.Notes = notes
	a.Metadata = map[string]any{}
	_ = json.Unmarshal(meta, &a.Metadata)
	a.DisplayStatus = resolveDisplayStatus(a.Status, a.DueAt, time.Now().UTC())
	return &a, nil
}

func resolveDisplayStatus(stored string, due *time.Time, now time.Time) string {
	switch stored {
	case "completed", "cancelled":
		return stored
	}
	if due == nil {
		if stored == "due" {
			return "due"
		}
		return "upcoming"
	}
	if due.Before(now) {
		return "overdue"
	}
	// Due within the next 24h → due
	if due.Sub(now) <= 24*time.Hour {
		return "due"
	}
	return "upcoming"
}

func (r *Repository) ListTypes(ctx context.Context, includeInactive bool) ([]ActivityType, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, code, name, description, color, icon, is_system, is_active, allows_external, position,
			COALESCE(requires_datetime,false), COALESCE(requires_duration,false),
			COALESCE(requires_outcome,false), COALESCE(requires_notes,false)
		FROM activity_types
		WHERE ($1 OR is_active = TRUE)
		ORDER BY position, name
	`, includeInactive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ActivityType
	for rows.Next() {
		var t ActivityType
		if err := rows.Scan(&t.ID, &t.Code, &t.Name, &t.Description, &t.Color, &t.Icon,
			&t.IsSystem, &t.IsActive, &t.AllowsExternal, &t.Position,
			&t.RequiresDatetime, &t.RequiresDuration, &t.RequiresOutcome, &t.RequiresNotes); err != nil {
			return nil, err
		}
		items = append(items, t)
	}
	if items == nil {
		items = []ActivityType{}
	}
	return items, rows.Err()
}

func (r *Repository) TypeByCode(ctx context.Context, code string) (*ActivityType, error) {
	var t ActivityType
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, code, name, description, color, icon, is_system, is_active, allows_external, position,
			COALESCE(requires_datetime,false), COALESCE(requires_duration,false),
			COALESCE(requires_outcome,false), COALESCE(requires_notes,false)
		FROM activity_types WHERE code=$1
	`, code).Scan(&t.ID, &t.Code, &t.Name, &t.Description, &t.Color, &t.Icon,
		&t.IsSystem, &t.IsActive, &t.AllowsExternal, &t.Position,
		&t.RequiresDatetime, &t.RequiresDuration, &t.RequiresOutcome, &t.RequiresNotes)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *Repository) Get(ctx context.Context, id string) (*Activity, error) {
	a, err := scanActivity(r.pool.QueryRow(ctx, activitySelect+` WHERE a.id=$1`, id))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return a, err
}

func (r *Repository) List(ctx context.Context, f ListFilter) ([]Activity, int, error) {
	if f.Limit <= 0 {
		f.Limit = 100
	}
	args := []any{}
	where := []string{"1=1"}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.Replace(cond, "?", "$"+itoa(len(args)), 1))
	}
	if f.LeadID != "" {
		add("a.lead_id::text = ?", f.LeadID)
	}
	if f.CustomerID != "" {
		add("a.customer_id::text = ?", f.CustomerID)
	}
	if f.DealID != "" {
		add("a.deal_id::text = ?", f.DealID)
	}
	if f.OwnerUserID != "" {
		add("a.owner_user_id::text = ?", f.OwnerUserID)
	}
	if f.TeamID != "" {
		add(`EXISTS (
			SELECT 1 FROM team_members tm
			WHERE tm.user_id = a.owner_user_id AND tm.team_id::text = ?
		)`, f.TeamID)
	}
	vis := datascope.Visibility{Unscoped: f.ScopeUnscoped, OwnerIDs: f.ScopeOwnerIDs, TeamIDs: f.ScopeTeamIDs}
	where, args = datascope.AppendWhere(where, args, vis, datascope.Columns{Owner: "a.owner_user_id", Team: ""})
	if f.TypeCode != "" {
		add("a.kind = ?", f.TypeCode)
	}
	if f.PipelineID != "" {
		add("d.pipeline_id::text = ?", f.PipelineID)
	}
	if f.Status != "" && f.Status != "all" {
		// Filter by stored + derived overdue
		if f.Status == "overdue" {
			where = append(where, `a.status IN ('upcoming','due','overdue') AND a.due_at IS NOT NULL AND a.due_at < NOW()`)
		} else if f.Status == "upcoming" {
			where = append(where, `a.status IN ('upcoming','due') AND (a.due_at IS NULL OR a.due_at >= NOW())`)
		} else {
			add("a.status = ?", f.Status)
		}
	}
	if f.From != nil {
		add("(COALESCE(a.start_at, a.due_at, a.created_at) >= ?)", *f.From)
	}
	if f.To != nil {
		add("(COALESCE(a.start_at, a.due_at, a.created_at) < ?)", *f.To)
	}

	whereSQL := strings.Join(where, " AND ")
	var total int
	countSQL := `
		SELECT COUNT(*)
		FROM activities a
		LEFT JOIN deals d ON d.id = a.deal_id
		WHERE ` + whereSQL
	if err := r.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, f.Limit, f.Offset)
	limitPh := "$" + itoa(len(args)-1)
	offsetPh := "$" + itoa(len(args))
	rows, err := r.pool.Query(ctx, activitySelect+` WHERE `+whereSQL+`
		ORDER BY COALESCE(a.start_at, a.due_at, a.created_at) ASC
		LIMIT `+limitPh+` OFFSET `+offsetPh, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var items []Activity
	for rows.Next() {
		a, err := scanActivity(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *a)
	}
	if items == nil {
		items = []Activity{}
	}
	return items, total, rows.Err()
}

func (r *Repository) Create(ctx context.Context, actorID string, in CreateInput, typ *ActivityType, start, end, due *time.Time) (*Activity, error) {
	id := uuid.NewString()
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = strings.TrimSpace(in.Subject)
	}
	if title == "" {
		title = typ.Name
	}
	notes := in.Notes
	if notes == "" {
		notes = in.Body
	}
	status := in.Status
	if status == "" {
		if due != nil && due.Before(time.Now().UTC()) {
			status = "overdue"
		} else if due != nil && due.Sub(time.Now().UTC()) <= 24*time.Hour {
			status = "due"
		} else if due != nil || start != nil {
			status = "upcoming"
		} else {
			status = "completed"
		}
	}
	// normalize legacy
	if status == "planned" {
		status = "upcoming"
	}
	priority := in.Priority
	if priority == "" {
		priority = "medium"
	}
	owner := emptyToNil(in.OwnerUserID)
	if owner == nil {
		owner = &actorID
	}
	meta, _ := json.Marshal(in.Metadata)
	if in.Metadata == nil {
		meta = []byte("{}")
	}
	var completedAt any
	var completedBy any
	if status == "completed" {
		completedAt = time.Now().UTC()
		completedBy = actorID
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO activities (
			id, kind, activity_type_id, title, subject, body, status, priority, outcome,
			due_at, start_at, end_at, completed_at,
			actor_user_id, owner_user_id, created_by_user_id, completed_by_user_id,
			lead_id, customer_id, deal_id,
			external_provider, external_id, external_thread_id, metadata
		) VALUES (
			$1,$2,$3,$4,$4,$5,$6,$7,$8,
			$9,$10,$11,$12,
			$13,$14,$13,$15,
			$16,$17,$18,
			$19,$20,$21,$22::jsonb
		)
	`, id, typ.Code, typ.ID, title, notes, status, priority, in.Outcome,
		due, start, end, completedAt,
		actorID, owner, completedBy,
		emptyToNil(in.LeadID), emptyToNil(in.CustomerID), emptyToNil(in.DealID),
		in.ExternalProvider, in.ExternalID, in.ExternalThreadID, string(meta))
	if err != nil {
		return nil, err
	}
	r.touchRelated(ctx, emptyToNil(in.LeadID), emptyToNil(in.CustomerID), emptyToNil(in.DealID), due, status)
	return r.Get(ctx, id)
}

func (r *Repository) Update(ctx context.Context, actorID string, current *Activity, in UpdateInput, typ *ActivityType, start, end, due *time.Time, setDue, setStart, setEnd bool) (*Activity, error) {
	title := current.Title
	if in.Title != nil {
		title = strings.TrimSpace(*in.Title)
	}
	kind := current.Kind
	typeID := current.TypeID
	if typ != nil {
		kind = typ.Code
		typeID = &typ.ID
	}
	notes := current.Notes
	if in.Notes != nil {
		notes = *in.Notes
	}
	outcome := current.Outcome
	if in.Outcome != nil {
		outcome = *in.Outcome
	}
	status := current.Status
	if in.Status != nil && *in.Status != "" {
		status = *in.Status
		if status == "planned" {
			status = "upcoming"
		}
	}
	priority := current.Priority
	if in.Priority != nil && *in.Priority != "" {
		priority = *in.Priority
	}
	owner := current.OwnerUserID
	if in.OwnerUserID != nil {
		owner = emptyToNil(in.OwnerUserID)
	}
	lead := current.LeadID
	if in.LeadID != nil {
		lead = emptyToNil(in.LeadID)
	}
	customer := current.CustomerID
	if in.CustomerID != nil {
		customer = emptyToNil(in.CustomerID)
	}
	deal := current.DealID
	if in.DealID != nil {
		deal = emptyToNil(in.DealID)
	}
	startAt := current.StartAt
	if setStart {
		startAt = start
	}
	endAt := current.EndAt
	if setEnd {
		endAt = end
	}
	dueAt := current.DueAt
	if setDue {
		dueAt = due
	}
	extProv := current.ExternalProvider
	if in.ExternalProvider != nil {
		extProv = *in.ExternalProvider
	}
	extID := current.ExternalID
	if in.ExternalID != nil {
		extID = *in.ExternalID
	}
	extThread := current.ExternalThreadID
	if in.ExternalThreadID != nil {
		extThread = *in.ExternalThreadID
	}

	completedAt := current.CompletedAt
	completedBy := current.CompletedByUserID
	if status == "completed" && (current.Status != "completed") {
		now := time.Now().UTC()
		completedAt = &now
		completedBy = &actorID
	}

	_, err := r.pool.Exec(ctx, `
		UPDATE activities SET
			title=$2, subject=$2, kind=$3, activity_type_id=$4, body=$5, outcome=$6,
			status=$7, priority=$8, owner_user_id=$9,
			lead_id=$10, customer_id=$11, deal_id=$12,
			start_at=$13, end_at=$14, due_at=$15,
			completed_at=$16, completed_by_user_id=$17,
			external_provider=$18, external_id=$19, external_thread_id=$20
		WHERE id=$1
	`, current.ID, title, kind, typeID, notes, outcome, status, priority, owner,
		lead, customer, deal, startAt, endAt, dueAt, completedAt, completedBy,
		extProv, extID, extThread)
	if err != nil {
		return nil, err
	}
	r.touchRelated(ctx, lead, customer, deal, dueAt, status)
	return r.Get(ctx, current.ID)
}

func (r *Repository) touchRelated(ctx context.Context, lead, customer, deal *string, due *time.Time, status string) {
	now := time.Now().UTC()
	if lead != nil {
		_, _ = r.pool.Exec(ctx, `
			UPDATE leads SET
				last_activity_at = CASE WHEN $2 IN ('completed') THEN NOW() ELSE last_activity_at END,
				next_activity_at = CASE
					WHEN $3::timestamptz IS NOT NULL AND $2 IN ('upcoming','due','overdue') THEN $3
					ELSE next_activity_at
				END
			WHERE id=$1
		`, *lead, status, due)
	}
	if customer != nil {
		_, _ = r.pool.Exec(ctx, `
			UPDATE customers SET
				last_contacted_at = CASE WHEN $2 = 'completed' THEN NOW() ELSE last_contacted_at END,
				next_follow_up_at = CASE
					WHEN $3::timestamptz IS NOT NULL AND $2 IN ('upcoming','due','overdue') THEN $3
					ELSE next_follow_up_at
				END
			WHERE id=$1
		`, *customer, status, due)
	}
	if deal != nil {
		_, _ = r.pool.Exec(ctx, `
			UPDATE deals SET
				last_activity_at = CASE WHEN $2 = 'completed' THEN $4 ELSE last_activity_at END,
				next_activity_at = CASE
					WHEN $3::timestamptz IS NOT NULL AND $2 IN ('upcoming','due','overdue') THEN $3
					ELSE next_activity_at
				END
			WHERE id=$1
		`, *deal, status, due, now)
	}
}

func (r *Repository) FollowUpIntel(ctx context.Context, leadID, customerID, dealID string) (*FollowUpIntel, error) {
	intel := &FollowUpIntel{}
	var last, next *time.Time
	err := r.pool.QueryRow(ctx, `
		SELECT
			MAX(CASE WHEN status='completed' THEN COALESCE(completed_at, created_at) END),
			MIN(CASE WHEN status IN ('upcoming','due','overdue') THEN COALESCE(due_at, start_at) END)
		FROM activities
		WHERE ($1='' OR lead_id::text=$1)
		  AND ($2='' OR customer_id::text=$2)
		  AND ($3='' OR deal_id::text=$3)
	`, leadID, customerID, dealID).Scan(&last, &next)
	if err != nil {
		return nil, err
	}
	intel.LastActivityAt = last
	intel.NextActivityAt = next
	if last != nil {
		d := int(time.Since(*last).Hours() / 24)
		intel.DaysSinceContact = &d
	}
	_ = r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE status IN ('upcoming','due','overdue') AND due_at < NOW()),
			COUNT(*) FILTER (WHERE status IN ('upcoming','due') AND (due_at IS NULL OR due_at >= NOW()))
		FROM activities
		WHERE ($1='' OR lead_id::text=$1)
		  AND ($2='' OR customer_id::text=$2)
		  AND ($3='' OR deal_id::text=$3)
	`, leadID, customerID, dealID).Scan(&intel.OverdueCount, &intel.UpcomingCount)

	if dealID != "" {
		var entered *time.Time
		_ = r.pool.QueryRow(ctx, `SELECT stage_entered_at FROM deals WHERE id=$1`, dealID).Scan(&entered)
		if entered != nil {
			intel.StageEnteredAt = entered
			d := int(time.Since(*entered).Hours() / 24)
			intel.DaysInStage = &d
		}
	}
	if customerID != "" && intel.StageEnteredAt == nil {
		var entered *time.Time
		// approximate via latest deal stage
		_ = r.pool.QueryRow(ctx, `
			SELECT stage_entered_at FROM deals WHERE customer_id=$1 AND status='open'
			ORDER BY stage_entered_at DESC NULLS LAST LIMIT 1
		`, customerID).Scan(&entered)
		if entered != nil {
			intel.StageEnteredAt = entered
			d := int(time.Since(*entered).Hours() / 24)
			intel.DaysInStage = &d
		}
	}
	if next != nil && next.Before(time.Now().UTC()) {
		h := int(time.Since(*next).Hours())
		intel.OverdueDurationHours = &h
	} else if intel.OverdueCount > 0 {
		var oldest *time.Time
		_ = r.pool.QueryRow(ctx, `
			SELECT MIN(due_at) FROM activities
			WHERE status IN ('upcoming','due','overdue') AND due_at < NOW()
			  AND ($1='' OR lead_id::text=$1)
			  AND ($2='' OR customer_id::text=$2)
			  AND ($3='' OR deal_id::text=$3)
		`, leadID, customerID, dealID).Scan(&oldest)
		if oldest != nil {
			h := int(time.Since(*oldest).Hours())
			intel.OverdueDurationHours = &h
		}
	}
	return intel, nil
}

func (r *Repository) UserTimezone(ctx context.Context, userID string) (string, error) {
	var tz string
	err := r.pool.QueryRow(ctx, `SELECT timezone FROM users WHERE id=$1`, userID).Scan(&tz)
	if err != nil {
		return "UTC", err
	}
	if tz == "" {
		return "UTC", nil
	}
	return tz, nil
}

func (r *Repository) SetUserTimezone(ctx context.Context, userID, tz string) error {
	_, err := r.pool.Exec(ctx, `UPDATE users SET timezone=$2 WHERE id=$1`, userID, tz)
	return err
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

// Service / Handler below in same package files for clarity — keep service in service.go
type Service struct {
	repo        *Repository
	audit       *audit.Service
	timeline    *timeline.Service
	hooks       *automation.Emitter
	sysActivity *systemactivity.Service
}

func NewService(repo *Repository, auditSvc *audit.Service, timelineSvc *timeline.Service, hooks *automation.Emitter, sysActivity *systemactivity.Service) *Service {
	return &Service{repo: repo, audit: auditSvc, timeline: timelineSvc, hooks: hooks, sysActivity: sysActivity}
}
