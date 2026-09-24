package leads

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/crm/backend/internal/datascope"
)

const leadSelect = `
	SELECT l.id::text, l.full_name, l.email, l.phone, l.country, l.nationality, l.location,
		l.owner_user_id::text, ou.full_name, l.team_id::text, t.name,
		l.source, l.priority, l.tags, l.anzsco_id::text, a.code, a.title,
		l.pipeline_id::text, p.name, l.stage_id::text, ps.name,
		l.notes, l.occupation, l.job_title, l.employer, l.experience_years, l.qualification, l.skills,
		l.potential_value, l.expected_outcome, l.last_activity_at, l.next_activity_at,
		l.status, l.converted_customer_id::text, l.converted_at, l.is_archived, l.archived_at,
		GREATEST(0, EXTRACT(DAY FROM NOW() - l.created_at)::int),
		l.created_at, l.updated_at
	FROM leads l
	LEFT JOIN users ou ON ou.id = l.owner_user_id
	LEFT JOIN teams t ON t.id = l.team_id
	LEFT JOIN anzsco_occupations a ON a.id = l.anzsco_id
	LEFT JOIN pipelines p ON p.id = l.pipeline_id
	LEFT JOIN pipeline_stages ps ON ps.id = l.stage_id
`

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func scanLead(row pgx.Row) (*Lead, error) {
	var l Lead
	var tags, skills []string
	err := row.Scan(
		&l.ID, &l.FullName, &l.Email, &l.Phone, &l.Country, &l.Nationality, &l.Location,
		&l.OwnerUserID, &l.OwnerName, &l.TeamID, &l.TeamName,
		&l.Source, &l.Priority, &tags, &l.AnzscoID, &l.AnzscoCode, &l.AnzscoTitle,
		&l.PipelineID, &l.PipelineName, &l.StageID, &l.StageName,
		&l.Notes, &l.Occupation, &l.JobTitle, &l.Employer, &l.ExperienceYears, &l.Qualification, &skills,
		&l.PotentialValue, &l.ExpectedOutcome, &l.LastActivityAt, &l.NextActivityAt,
		&l.Status, &l.ConvertedCustomerID, &l.ConvertedAt, &l.IsArchived, &l.ArchivedAt,
		&l.AgeDays, &l.CreatedAt, &l.UpdatedAt,
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
	l.Tags = tags
	l.Skills = skills
	return &l, nil
}

func (r *Repository) Get(ctx context.Context, id string) (*Lead, error) {
	l, err := scanLead(r.pool.QueryRow(ctx, leadSelect+` WHERE l.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return l, err
}

func (r *Repository) List(ctx context.Context, f ListFilter) ([]Lead, int, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 25
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	where := []string{"($1 = '' OR l.full_name ILIKE '%' || $1 || '%' OR COALESCE(l.email,'') ILIKE '%' || $1 || '%' OR COALESCE(l.phone,'') ILIKE '%' || $1 || '%')"}
	args := []any{f.Search}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}

	if !f.IncludeArchived {
		where = append(where, "l.is_archived = FALSE")
	}
	if f.OwnerUserID != "" {
		add("l.owner_user_id::text = $%d", f.OwnerUserID)
	}
	if f.TeamID != "" {
		add("l.team_id::text = $%d", f.TeamID)
	}
	if f.PipelineID != "" {
		add("l.pipeline_id::text = $%d", f.PipelineID)
	}
	if f.StageID != "" {
		add("l.stage_id::text = $%d", f.StageID)
	}
	if f.Source != "" {
		add("l.source = $%d", f.Source)
	}
	if f.Priority != "" {
		add("l.priority = $%d", f.Priority)
	}
	if f.AnzscoID != "" {
		add("l.anzsco_id::text = $%d", f.AnzscoID)
	}
	if f.Tag != "" {
		add("$%d = ANY(l.tags)", f.Tag)
	}
	if f.CreatedFrom != "" {
		add("l.created_at >= $%d::timestamptz", f.CreatedFrom)
	}
	if f.CreatedTo != "" {
		add("l.created_at < ($%d::date + INTERVAL '1 day')", f.CreatedTo)
	}
	if f.InactiveDays > 0 {
		add("(l.last_activity_at IS NULL OR l.last_activity_at < NOW() - ($%d || ' days')::interval)", f.InactiveDays)
	}

	vis := datascope.Visibility{
		Unscoped: f.ScopeUnscoped,
		OwnerIDs: f.ScopeOwnerIDs,
		TeamIDs:  f.ScopeTeamIDs,
	}
	where, args = datascope.AppendWhere(where, args, vis, datascope.Columns{
		Owner: "l.owner_user_id",
		Team:  "l.team_id",
	})

	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM leads l WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	sortCol := map[string]string{
		"name": "l.full_name", "created": "l.created_at", "priority": "l.priority",
		"lastActivity": "l.last_activity_at", "nextActivity": "l.next_activity_at", "age": "l.created_at",
	}[f.Sort]
	if sortCol == "" {
		sortCol = "l.created_at"
	}
	order := "DESC"
	if strings.EqualFold(f.Order, "asc") {
		order = "ASC"
	}
	if f.Sort == "age" {
		if order == "DESC" {
			order = "ASC"
		} else {
			order = "DESC"
		}
	}

	limitIdx := len(args) + 1
	offsetIdx := len(args) + 2
	args = append(args, f.Limit, f.Offset)

	q := leadSelect + ` WHERE ` + whereSQL +
		fmt.Sprintf(` ORDER BY %s %s NULLS LAST LIMIT $%d OFFSET $%d`, sortCol, order, limitIdx, offsetIdx)

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var items []Lead
	for rows.Next() {
		l, err := scanLead(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *l)
	}
	if items == nil {
		items = []Lead{}
	}
	return items, total, rows.Err()
}

func (r *Repository) Create(ctx context.Context, in CreateInput, nextAt *time.Time) (*Lead, error) {
	id := uuid.NewString()
	email := normalizeEmailPtr(in.Email)
	phone := normalizePhonePtr(in.Phone)
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

	_, err := r.pool.Exec(ctx, `
		INSERT INTO leads (
			id, full_name, email, phone, country, nationality, location,
			owner_user_id, team_id, source, priority, tags, anzsco_id,
			pipeline_id, stage_id, notes, occupation, job_title, employer,
			experience_years, qualification, skills, potential_value, expected_outcome,
			next_activity_at, last_activity_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25, NOW()
		)
	`, id, strings.TrimSpace(in.FullName), email, phone, in.Country, in.Nationality, in.Location,
		nullIfEmpty(in.OwnerUserID), nullIfEmpty(in.TeamID), in.Source, priority, tags, nullIfEmpty(in.AnzscoID),
		nullIfEmpty(in.PipelineID), nullIfEmpty(in.StageID), in.Notes, in.Occupation, in.JobTitle, in.Employer,
		in.ExperienceYears, in.Qualification, skills, in.PotentialValue, in.ExpectedOutcome, nextAt)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *Repository) Update(ctx context.Context, id string, current *Lead, in UpdateInput, nextAt **time.Time) (*Lead, error) {
	fullName := current.FullName
	if in.FullName != nil {
		fullName = strings.TrimSpace(*in.FullName)
	}
	email := current.Email
	if in.Email != nil {
		email = normalizeEmailPtr(in.Email)
	}
	phone := current.Phone
	if in.Phone != nil {
		phone = normalizePhonePtr(in.Phone)
	}
	country := pickStr(in.Country, current.Country)
	nationality := pickStr(in.Nationality, current.Nationality)
	location := pickStr(in.Location, current.Location)
	source := pickStr(in.Source, current.Source)
	priority := pickStr(in.Priority, current.Priority)
	notes := pickStr(in.Notes, current.Notes)
	occupation := pickStr(in.Occupation, current.Occupation)
	jobTitle := pickStr(in.JobTitle, current.JobTitle)
	employer := pickStr(in.Employer, current.Employer)
	qualification := pickStr(in.Qualification, current.Qualification)
	expected := pickStr(in.ExpectedOutcome, current.ExpectedOutcome)
	status := pickStr(in.Status, current.Status)

	owner := current.OwnerUserID
	if in.OwnerUserID != nil {
		owner = nullIfEmpty(in.OwnerUserID)
	}
	team := current.TeamID
	if in.TeamID != nil {
		team = nullIfEmpty(in.TeamID)
	}
	anzsco := current.AnzscoID
	if in.AnzscoID != nil {
		anzsco = nullIfEmpty(in.AnzscoID)
	}
	pipeline := current.PipelineID
	if in.PipelineID != nil {
		pipeline = nullIfEmpty(in.PipelineID)
	}
	stage := current.StageID
	if in.StageID != nil {
		stage = nullIfEmpty(in.StageID)
	}
	tags := current.Tags
	if in.Tags != nil {
		tags = in.Tags
	}
	skills := current.Skills
	if in.Skills != nil {
		skills = in.Skills
	}
	exp := current.ExperienceYears
	if in.ExperienceYears != nil {
		exp = in.ExperienceYears
	}
	val := current.PotentialValue
	if in.PotentialValue != nil {
		val = in.PotentialValue
	}
	next := current.NextActivityAt
	if nextAt != nil {
		next = *nextAt
	}

	_, err := r.pool.Exec(ctx, `
		UPDATE leads SET
			full_name=$2, email=$3, phone=$4, country=$5, nationality=$6, location=$7,
			owner_user_id=$8, team_id=$9, source=$10, priority=$11, tags=$12, anzsco_id=$13,
			pipeline_id=$14, stage_id=$15, notes=$16, occupation=$17, job_title=$18, employer=$19,
			experience_years=$20, qualification=$21, skills=$22, potential_value=$23, expected_outcome=$24,
			next_activity_at=$25, status=$26
		WHERE id=$1
	`, id, fullName, email, phone, country, nationality, location,
		owner, team, source, priority, tags, anzsco,
		pipeline, stage, notes, occupation, jobTitle, employer,
		exp, qualification, skills, val, expected, next, status)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *Repository) SetArchived(ctx context.Context, ids []string, archive bool) (int, error) {
	var archivedAt any
	status := "open"
	if archive {
		archivedAt = time.Now().UTC()
		status = "archived"
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE leads SET is_archived=$2, archived_at=$3,
			status = CASE WHEN $2 THEN 'archived' ELSE CASE WHEN status='archived' THEN $4 ELSE status END END
		WHERE id = ANY($1::uuid[])
	`, ids, archive, archivedAt, status)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (r *Repository) BulkAssign(ctx context.Context, ids []string, ownerID, teamID *string) (int, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE leads SET
			owner_user_id = COALESCE($2, owner_user_id),
			team_id = COALESCE($3, team_id)
		WHERE id = ANY($1::uuid[])
	`, ids, nullIfEmpty(ownerID), nullIfEmpty(teamID))
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (r *Repository) BulkStage(ctx context.Context, ids []string, pipelineID, stageID string) (int, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE leads SET pipeline_id=$2, stage_id=$3 WHERE id = ANY($1::uuid[])
	`, ids, pipelineID, stageID)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (r *Repository) FindDuplicates(ctx context.Context, excludeID string, email, phone, name *string) ([]DuplicateMatch, error) {
	var matches []DuplicateMatch

	collect := func(q string, args ...any) error {
		rows, err := r.pool.Query(ctx, q, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m DuplicateMatch
			if err := rows.Scan(&m.ID, &m.FullName, &m.Email, &m.Phone, &m.Entity, &m.Reason); err != nil {
				return err
			}
			matches = append(matches, m)
		}
		return rows.Err()
	}

	if email != nil && *email != "" {
		if err := collect(`
			SELECT id::text, full_name, email, phone, 'lead', 'email'
			FROM leads WHERE is_archived=FALSE AND email=$1 AND ($2='' OR id::text <> $2)
			UNION ALL
			SELECT id::text, full_name, email, phone, 'customer', 'email'
			FROM customers WHERE is_archived=FALSE AND email=$1
		`, *email, excludeID); err != nil {
			return nil, err
		}
	}
	if phone != nil && *phone != "" {
		if err := collect(`
			SELECT id::text, full_name, email, phone, 'lead', 'phone'
			FROM leads WHERE is_archived=FALSE AND phone=$1 AND ($2='' OR id::text <> $2)
			UNION ALL
			SELECT id::text, full_name, email, phone, 'customer', 'phone'
			FROM customers WHERE is_archived=FALSE AND phone=$1
		`, *phone, excludeID); err != nil {
			return nil, err
		}
	}
	if name != nil && strings.TrimSpace(*name) != "" {
		n := strings.TrimSpace(*name)
		if err := collect(`
			SELECT id::text, full_name, email, phone, 'lead', 'name'
			FROM leads WHERE is_archived=FALSE AND lower(full_name)=lower($1) AND ($2='' OR id::text <> $2)
			UNION ALL
			SELECT id::text, full_name, email, phone, 'customer', 'name'
			FROM customers WHERE is_archived=FALSE AND lower(full_name)=lower($1)
		`, n, excludeID); err != nil {
			return nil, err
		}
	}

	// de-dupe by entity+id keeping first reason
	seen := map[string]struct{}{}
	var unique []DuplicateMatch
	for _, m := range matches {
		key := m.Entity + ":" + m.ID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, m)
	}
	if unique == nil {
		unique = []DuplicateMatch{}
	}
	return unique, nil
}

func (r *Repository) MarkConverted(ctx context.Context, leadID, customerID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE leads SET status='converted', converted_customer_id=$2, converted_at=NOW()
		WHERE id=$1
	`, leadID, customerID)
	return err
}

func (r *Repository) TouchLastActivity(ctx context.Context, leadID string) error {
	_, err := r.pool.Exec(ctx, `UPDATE leads SET last_activity_at=NOW() WHERE id=$1`, leadID)
	return err
}

func normalizeEmailPtr(v *string) *string {
	if v == nil {
		return nil
	}
	s := strings.ToLower(strings.TrimSpace(*v))
	if s == "" {
		return nil
	}
	return &s
}

func normalizePhonePtr(v *string) *string {
	if v == nil {
		return nil
	}
	s := strings.TrimSpace(*v)
	if s == "" {
		return nil
	}
	return &s
}

func nullIfEmpty(v *string) *string {
	if v == nil {
		return nil
	}
	if strings.TrimSpace(*v) == "" {
		return nil
	}
	return v
}

func pickStr(ptr *string, fallback string) string {
	if ptr == nil {
		return fallback
	}
	return *ptr
}
