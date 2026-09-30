package calls

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/crm/backend/internal/datascope"
	"github.com/crm/backend/internal/timeline"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

var nonDigits = regexp.MustCompile(`\D`)

func placeholder(args []any) string { return "$" + strconv.Itoa(len(args)) }

// contactSearch matches name, email, phone and employer; phone also matches on digits only.
func contactSearch(alias, q string, args []any) (string, []any) {
	q = strings.TrimSpace(q)
	if q == "" {
		return "", args
	}
	args = append(args, q)
	p := placeholder(args)
	parts := []string{
		alias + ".full_name ILIKE '%'||" + p + "||'%'",
		"COALESCE(" + alias + ".email,'') ILIKE '%'||" + p + "||'%'",
		"COALESCE(" + alias + ".phone,'') ILIKE '%'||" + p + "||'%'",
		alias + ".employer ILIKE '%'||" + p + "||'%'",
	}
	if digits := nonDigits.ReplaceAllString(q, ""); len(digits) >= 3 {
		args = append(args, digits)
		parts = append(parts, "regexp_replace(COALESCE("+alias+".phone,''),'\\D','','g') LIKE '%'||"+placeholder(args)+"||'%'")
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

// Contacts lists leads and customers the caller may see. A nil visibility skips
// that entity type (caller lacks the view permission). Converted leads are
// excluded so a person never appears twice.
func (r *Repository) Contacts(ctx context.Context, f ContactFilter, leadVis, customerVis *datascope.Visibility) ([]Contact, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 50
	}
	var args []any
	var parts []string

	if leadVis != nil && f.Type != EntityCustomer {
		where := []string{"l.is_archived = FALSE", "l.converted_customer_id IS NULL"}
		var cond string
		if cond, args = contactSearch("l", f.Search, args); cond != "" {
			where = append(where, cond)
		}
		where, args = datascope.AppendWhere(where, args, *leadVis, datascope.Columns{Owner: "l.owner_user_id", Team: "l.team_id"})
		parts = append(parts, `
			SELECT 'lead' AS type, l.id::text AS id, l.full_name, l.email, l.phone, l.employer,
				ps.name AS stage_name, l.priority, ou.full_name AS owner_name, l.updated_at
			FROM leads l
			LEFT JOIN pipeline_stages ps ON ps.id = l.stage_id
			LEFT JOIN users ou ON ou.id = l.owner_user_id
			WHERE `+strings.Join(where, " AND "))
	}
	if customerVis != nil && f.Type != EntityLead {
		where := []string{"c.is_archived = FALSE"}
		var cond string
		if cond, args = contactSearch("c", f.Search, args); cond != "" {
			where = append(where, cond)
		}
		where, args = datascope.AppendWhere(where, args, *customerVis, datascope.Columns{Owner: "c.owner_user_id", Team: "c.team_id"})
		parts = append(parts, `
			SELECT 'customer' AS type, c.id::text AS id, c.full_name, c.email, c.phone, c.employer,
				ps.name AS stage_name, c.priority, ou.full_name AS owner_name, c.updated_at
			FROM customers c
			LEFT JOIN pipeline_stages ps ON ps.id = c.stage_id
			LEFT JOIN users ou ON ou.id = c.owner_user_id
			WHERE `+strings.Join(where, " AND "))
	}
	if len(parts) == 0 {
		return []Contact{}, nil
	}
	args = append(args, f.Limit)
	rows, err := r.pool.Query(ctx, `
		SELECT type, id, full_name, email, phone, employer, stage_name, priority, owner_name, updated_at
		FROM (`+strings.Join(parts, " UNION ALL ")+`) x
		ORDER BY updated_at DESC, full_name ASC
		LIMIT `+placeholder(args), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Contact{}
	for rows.Next() {
		var c Contact
		if err := rows.Scan(&c.Type, &c.ID, &c.FullName, &c.Email, &c.Phone, &c.Employer,
			&c.StageName, &c.Priority, &c.OwnerName, &c.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

// InScope reports whether the record exists and falls inside the caller's visibility.
func (r *Repository) InScope(ctx context.Context, entityType, id string, vis datascope.Visibility) (bool, error) {
	table, alias := "leads l", "l"
	if entityType == EntityCustomer {
		table, alias = "customers c", "c"
	}
	where := []string{alias + ".id = $1"}
	args := []any{id}
	where, args = datascope.AppendWhere(where, args, vis, datascope.Columns{Owner: alias + ".owner_user_id", Team: alias + ".team_id"})
	var ok bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE `+strings.Join(where, " AND ")+`)`, args...).Scan(&ok)
	return ok, err
}

func entityColumn(entityType string) string {
	if entityType == EntityCustomer {
		return "customer_id"
	}
	return "lead_id"
}

func (r *Repository) Deals(ctx context.Context, customerID string) ([]DealSummary, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT d.id::text, d.title, d.status, ps.name, d.value::float8, d.currency,
			to_char(d.expected_close_at, 'YYYY-MM-DD'), d.lost_reason
		FROM deals d
		LEFT JOIN pipeline_stages ps ON ps.id = d.stage_id
		WHERE d.customer_id = $1 AND d.status <> 'archived'
		ORDER BY (d.status = 'open') DESC, d.updated_at DESC
		LIMIT 10
	`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []DealSummary{}
	for rows.Next() {
		var d DealSummary
		if err := rows.Scan(&d.ID, &d.Title, &d.Status, &d.StageName, &d.Value, &d.Currency, &d.ExpectedCloseAt, &d.LostReason); err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, rows.Err()
}

func (r *Repository) Upcoming(ctx context.Context, entityType, id string) ([]UpcomingActivity, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT a.id::text, COALESCE(NULLIF(a.title,''), a.subject), a.kind, t.name, a.status,
			COALESCE(a.due_at, a.start_at)
		FROM activities a
		LEFT JOIN activity_types t ON t.id = a.activity_type_id
		WHERE a.`+entityColumn(entityType)+` = $1 AND a.status IN ('upcoming','due','overdue')
		ORDER BY COALESCE(a.due_at, a.start_at) ASC NULLS LAST
		LIMIT 5
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []UpcomingActivity{}
	for rows.Next() {
		var a UpcomingActivity
		if err := rows.Scan(&a.ID, &a.Title, &a.Kind, &a.TypeName, &a.Status, &a.DueAt); err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return items, rows.Err()
}

// interactionEvents are timeline types that represent contact with the person.
var interactionEvents = []string{
	timeline.EventCall, timeline.EventWhatsApp, timeline.EventEmail, timeline.EventMeeting,
	timeline.EventNote, timeline.EventActivityCompleted,
}

func (r *Repository) LastInteraction(ctx context.Context, entityType, id string) (*Interaction, error) {
	var in Interaction
	err := r.pool.QueryRow(ctx, `
		SELECT e.event_type, e.title, e.body, e.occurred_at, u.full_name
		FROM timeline_events e
		LEFT JOIN users u ON u.id = e.actor_user_id
		WHERE e.`+entityColumn(entityType)+` = $1 AND e.event_type = ANY($2)
		ORDER BY e.occurred_at DESC, e.created_at DESC
		LIMIT 1
	`, id, interactionEvents).Scan(&in.EventType, &in.Title, &in.Body, &in.OccurredAt, &in.ActorName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &in, nil
}

func (r *Repository) Documents(ctx context.Context, entityType, id string) ([]DocumentSummary, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT d.id::text, d.name, d.doc_type, d.status
		FROM documents d
		WHERE d.`+entityColumn(entityType)+` = $1
		ORDER BY d.created_at DESC
		LIMIT 20
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []DocumentSummary{}
	for rows.Next() {
		var d DocumentSummary
		if err := rows.Scan(&d.ID, &d.Name, &d.DocType, &d.Status); err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, rows.Err()
}

/* —— Notes —— */

const noteSelect = `
	SELECT n.id::text, n.lead_id::text, n.customer_id::text, n.topic, n.content,
		n.author_user_id::text, u.full_name, n.created_at, n.updated_at
	FROM conversation_notes n
	LEFT JOIN users u ON u.id = n.author_user_id
`

func scanNote(row pgx.Row) (*Note, error) {
	var n Note
	if err := row.Scan(&n.ID, &n.LeadID, &n.CustomerID, &n.Topic, &n.Content,
		&n.AuthorUserID, &n.AuthorName, &n.CreatedAt, &n.UpdatedAt); err != nil {
		return nil, err
	}
	return &n, nil
}

func (r *Repository) ListNotes(ctx context.Context, entityType, id string) ([]Note, error) {
	rows, err := r.pool.Query(ctx, noteSelect+` WHERE n.`+entityColumn(entityType)+` = $1 ORDER BY n.created_at DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Note{}
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *n)
	}
	return items, rows.Err()
}

func (r *Repository) GetNote(ctx context.Context, id string) (*Note, error) {
	n, err := scanNote(r.pool.QueryRow(ctx, noteSelect+` WHERE n.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return n, err
}

// timelineTitle renders "Call note · Next steps" from a topic id.
func timelineTitle(topic string) string {
	label := strings.ReplaceAll(topic, "_", " ")
	if label != "" {
		label = strings.ToUpper(label[:1]) + label[1:]
	}
	return "Call note · " + label
}

func timelineMeta(noteID, topic string) string {
	b, _ := json.Marshal(map[string]any{"noteId": noteID, "topic": topic, "origin": "calls"})
	return string(b)
}

// CreateNote stores the note and its single timeline mirror in one transaction.
func (r *Repository) CreateNote(ctx context.Context, entityType, entityID, topic, content, authorID string) (*Note, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	col := entityColumn(entityType)
	var id string
	if err := tx.QueryRow(ctx, `
		INSERT INTO conversation_notes (`+col+`, topic, content, author_user_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id::text
	`, entityID, topic, content, authorID).Scan(&id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO timeline_events (id, event_type, title, body, occurred_at, actor_user_id, source,
			external_id, external_provider, `+col+`, metadata)
		VALUES (gen_random_uuid(), $1, $2, $3, NOW(), $4, 'crm', $5, $6, $7, $8::jsonb)
	`, timeline.EventNote, timelineTitle(topic), content, authorID, id, timelineProvider, entityID, timelineMeta(id, topic)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetNote(ctx, id)
}

func (r *Repository) UpdateNote(ctx context.Context, id, topic, content string) (*Note, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `UPDATE conversation_notes SET topic = $2, content = $3 WHERE id = $1`, id, topic, content); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE timeline_events SET title = $3, body = $4, metadata = $5::jsonb
		WHERE external_provider = $1 AND external_id = $2
	`, timelineProvider, id, timelineTitle(topic), content, timelineMeta(id, topic)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetNote(ctx, id)
}

func (r *Repository) DeleteNote(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM timeline_events WHERE external_provider = $1 AND external_id = $2`, timelineProvider, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM conversation_notes WHERE id = $1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
