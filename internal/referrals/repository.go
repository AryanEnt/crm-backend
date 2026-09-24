package referrals

import (
	"context"
	"fmt"
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

func (r *Repository) ListCatalog(ctx context.Context) (*Meta, error) {
	types, err := r.listTypes(ctx)
	if err != nil {
		return nil, err
	}
	rels, err := r.listRelationships(ctx)
	if err != nil {
		return nil, err
	}
	statuses, err := r.listStatuses(ctx)
	if err != nil {
		return nil, err
	}
	return &Meta{ReferrerTypes: types, Relationships: rels, Statuses: statuses}, nil
}

func (r *Repository) listTypes(ctx context.Context) ([]CatalogItem, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, code, name, description, is_active, sort_order, FALSE
		FROM referral_referrer_types
		WHERE is_active = TRUE
		ORDER BY sort_order, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCatalog(rows)
}

func (r *Repository) listRelationships(ctx context.Context) ([]CatalogItem, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, code, name, description, is_active, sort_order, FALSE
		FROM referral_relationships
		WHERE is_active = TRUE
		ORDER BY sort_order, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCatalog(rows)
}

func (r *Repository) listStatuses(ctx context.Context) ([]CatalogItem, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, code, name, description, is_active, sort_order, is_converted
		FROM referral_statuses
		WHERE is_active = TRUE
		ORDER BY sort_order, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCatalog(rows)
}

func scanCatalog(rows pgx.Rows) ([]CatalogItem, error) {
	var out []CatalogItem
	for rows.Next() {
		var c CatalogItem
		if err := rows.Scan(&c.ID, &c.Code, &c.Name, &c.Description, &c.IsActive, &c.SortOrder, &c.IsConverted); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repository) TypeByCode(ctx context.Context, code string) (*CatalogItem, error) {
	var c CatalogItem
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, code, name, description, is_active, sort_order, FALSE
		FROM referral_referrer_types WHERE lower(code) = lower($1)`, code).
		Scan(&c.ID, &c.Code, &c.Name, &c.Description, &c.IsActive, &c.SortOrder, &c.IsConverted)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repository) RelationshipByCode(ctx context.Context, code string) (*CatalogItem, error) {
	var c CatalogItem
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, code, name, description, is_active, sort_order, FALSE
		FROM referral_relationships WHERE lower(code) = lower($1)`, code).
		Scan(&c.ID, &c.Code, &c.Name, &c.Description, &c.IsActive, &c.SortOrder, &c.IsConverted)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repository) StatusByCode(ctx context.Context, code string) (*CatalogItem, error) {
	var c CatalogItem
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, code, name, description, is_active, sort_order, is_converted
		FROM referral_statuses WHERE lower(code) = lower($1)`, code).
		Scan(&c.ID, &c.Code, &c.Name, &c.Description, &c.IsActive, &c.SortOrder, &c.IsConverted)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

const referralSelect = `
	SELECT
		r.id::text,
		r.lead_id::text,
		r.customer_id::text,
		r.referrer_type_id::text,
		rt.code, rt.name,
		r.referrer_user_id::text, ru.full_name,
		r.referrer_customer_id::text, rc.full_name,
		r.referrer_partner_id::text, rp.name,
		r.referrer_name,
		COALESCE(
			NULLIF(trim(r.referrer_name), ''),
			ru.full_name,
			rc.full_name,
			rp.name,
			'Unknown'
		) AS referrer_display,
		r.relationship_id::text, rr.code, rr.name,
		to_char(r.referral_date, 'YYYY-MM-DD'),
		r.referral_source, r.notes,
		r.status_id::text, rs.code, rs.name, rs.is_converted,
		r.referral_code,
		r.created_by_user_id::text,
		COALESCE(c.full_name, l.full_name) AS referred_name,
		COALESCE(c.owner_user_id, l.owner_user_id)::text,
		COALESCE(ou.full_name, ol.full_name),
		COALESCE(c.team_id, l.team_id)::text,
		COALESCE(c.pipeline_id, l.pipeline_id)::text,
		COALESCE(cp.name, lp.name),
		COALESCE(c.stage_id, l.stage_id)::text,
		COALESCE(cs.name, ls.name),
		COALESCE(c.potential_value, l.potential_value),
		CASE WHEN r.customer_id IS NOT NULL THEN 'customer' WHEN r.lead_id IS NOT NULL THEN 'lead' ELSE '' END,
		r.created_at, r.updated_at
	FROM referrals r
	JOIN referral_referrer_types rt ON rt.id = r.referrer_type_id
	JOIN referral_statuses rs ON rs.id = r.status_id
	LEFT JOIN referral_relationships rr ON rr.id = r.relationship_id
	LEFT JOIN users ru ON ru.id = r.referrer_user_id
	LEFT JOIN customers rc ON rc.id = r.referrer_customer_id
	LEFT JOIN referral_partners rp ON rp.id = r.referrer_partner_id
	LEFT JOIN leads l ON l.id = r.lead_id
	LEFT JOIN customers c ON c.id = r.customer_id
	LEFT JOIN users ol ON ol.id = l.owner_user_id
	LEFT JOIN users ou ON ou.id = c.owner_user_id
	LEFT JOIN pipelines lp ON lp.id = l.pipeline_id
	LEFT JOIN pipelines cp ON cp.id = c.pipeline_id
	LEFT JOIN pipeline_stages ls ON ls.id = l.stage_id
	LEFT JOIN pipeline_stages cs ON cs.id = c.stage_id
`

func scanReferral(row pgx.Row) (*Referral, error) {
	var ref Referral
	err := row.Scan(
		&ref.ID, &ref.LeadID, &ref.CustomerID,
		&ref.ReferrerTypeID, &ref.ReferrerTypeCode, &ref.ReferrerTypeName,
		&ref.ReferrerUserID, &ref.ReferrerUserName,
		&ref.ReferrerCustomerID, &ref.ReferrerCustomerName,
		&ref.ReferrerPartnerID, &ref.ReferrerPartnerName,
		&ref.ReferrerName, &ref.ReferrerDisplayName,
		&ref.RelationshipID, &ref.RelationshipCode, &ref.RelationshipName,
		&ref.ReferralDate, &ref.ReferralSource, &ref.Notes,
		&ref.StatusID, &ref.StatusCode, &ref.StatusName, &ref.IsConverted,
		&ref.ReferralCode, &ref.CreatedByUserID,
		&ref.ReferredName, &ref.OwnerUserID, &ref.OwnerName, &ref.TeamID,
		&ref.PipelineID, &ref.PipelineName, &ref.StageID, &ref.StageName,
		&ref.PotentialValue, &ref.SubjectKind,
		&ref.CreatedAt, &ref.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ref, nil
}

func (r *Repository) Get(ctx context.Context, id string) (*Referral, error) {
	return scanReferral(r.pool.QueryRow(ctx, referralSelect+` WHERE r.id = $1`, id))
}

func (r *Repository) GetByLeadID(ctx context.Context, leadID string) (*Referral, error) {
	return scanReferral(r.pool.QueryRow(ctx, referralSelect+` WHERE r.lead_id = $1`, leadID))
}

func (r *Repository) GetByCustomerID(ctx context.Context, customerID string) (*Referral, error) {
	return scanReferral(r.pool.QueryRow(ctx, referralSelect+` WHERE r.customer_id = $1`, customerID))
}

func (r *Repository) Create(ctx context.Context, in createRow) (*Referral, error) {
	id := uuid.NewString()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO referrals (
			id, lead_id, customer_id, referrer_type_id,
			referrer_user_id, referrer_customer_id, referrer_partner_id, referrer_name,
			relationship_id, referral_date, referral_source, notes, status_id, referral_code, created_by_user_id
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15
		)`,
		id, nilUUID(in.LeadID), nilUUID(in.CustomerID), in.ReferrerTypeID,
		nilUUID(in.ReferrerUserID), nilUUID(in.ReferrerCustomerID), nilUUID(in.ReferrerPartnerID), in.ReferrerName,
		nilUUID(in.RelationshipID), in.ReferralDate, in.ReferralSource, in.Notes, in.StatusID,
		nilStr(in.ReferralCode), nilUUID(in.CreatedByUserID),
	)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

type createRow struct {
	LeadID             *string
	CustomerID         *string
	ReferrerTypeID     string
	ReferrerUserID     *string
	ReferrerCustomerID *string
	ReferrerPartnerID  *string
	ReferrerName       string
	RelationshipID     *string
	ReferralDate       time.Time
	ReferralSource     string
	Notes              string
	StatusID           string
	ReferralCode       *string
	CreatedByUserID    *string
}

func (r *Repository) Update(ctx context.Context, id string, in createRow) (*Referral, error) {
	_, err := r.pool.Exec(ctx, `
		UPDATE referrals SET
			lead_id = $2,
			customer_id = $3,
			referrer_type_id = $4,
			referrer_user_id = $5,
			referrer_customer_id = $6,
			referrer_partner_id = $7,
			referrer_name = $8,
			relationship_id = $9,
			referral_date = $10,
			referral_source = $11,
			notes = $12,
			status_id = $13,
			referral_code = $14
		WHERE id = $1`,
		id, nilUUID(in.LeadID), nilUUID(in.CustomerID), in.ReferrerTypeID,
		nilUUID(in.ReferrerUserID), nilUUID(in.ReferrerCustomerID), nilUUID(in.ReferrerPartnerID),
		in.ReferrerName, nilUUID(in.RelationshipID), in.ReferralDate, in.ReferralSource, in.Notes,
		in.StatusID, nilStr(in.ReferralCode),
	)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *Repository) AttachCustomer(ctx context.Context, leadID, customerID, convertedStatusID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE referrals
		SET customer_id = $2,
		    status_id = $3
		WHERE lead_id = $1`, leadID, customerID, convertedStatusID)
	return err
}

func (r *Repository) List(ctx context.Context, f ListFilter) ([]Referral, int, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	where, args := buildListWhere(f)
	var total int
	countSQL := `SELECT COUNT(*) FROM referrals r
		JOIN referral_referrer_types rt ON rt.id = r.referrer_type_id
		JOIN referral_statuses rs ON rs.id = r.status_id
		LEFT JOIN leads l ON l.id = r.lead_id
		LEFT JOIN customers c ON c.id = r.customer_id
		LEFT JOIN users ru ON ru.id = r.referrer_user_id
		LEFT JOIN customers rc ON rc.id = r.referrer_customer_id
		LEFT JOIN referral_partners rp ON rp.id = r.referrer_partner_id
		WHERE ` + where
	if err := r.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	sortCol := "r.referral_date"
	switch f.Sort {
	case "referrer":
		sortCol = "referrer_display"
	case "referred":
		sortCol = "referred_name"
	case "status":
		sortCol = "rs.name"
	case "value":
		sortCol = "potential_value"
	case "createdAt":
		sortCol = "r.created_at"
	}
	order := "DESC"
	if strings.EqualFold(f.Order, "asc") {
		order = "ASC"
	}

	args = append(args, f.Limit, f.Offset)
	listSQL := referralSelect + ` WHERE ` + where +
		fmt.Sprintf(` ORDER BY %s %s NULLS LAST LIMIT $%d OFFSET $%d`, sortCol, order, len(args)-1, len(args))

	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Referral
	for rows.Next() {
		ref, err := scanReferral(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *ref)
	}
	return out, total, rows.Err()
}

func buildListWhere(f ListFilter) (string, []any) {
	parts := []string{"TRUE"}
	args := []any{}
	add := func(clause string, v any) {
		args = append(args, v)
		parts = append(parts, fmt.Sprintf(clause, len(args)))
	}
	if f.Search != "" {
		add(`(
			COALESCE(c.full_name, l.full_name, '') ILIKE '%%' || $%d || '%%'
			OR COALESCE(ru.full_name, rc.full_name, rp.name, r.referrer_name, '') ILIKE '%%' || $%d || '%%'
			OR COALESCE(r.referral_code, '') ILIKE '%%' || $%d || '%%'
		)`, f.Search)
		// reuse same arg three times — fix by using one placeholder
		args = args[:len(args)-1]
		args = append(args, f.Search)
		parts[len(parts)-1] = fmt.Sprintf(`(
			COALESCE(c.full_name, l.full_name, '') ILIKE '%%' || $%d || '%%'
			OR COALESCE(ru.full_name, rc.full_name, rp.name, r.referrer_name, '') ILIKE '%%' || $%d || '%%'
			OR COALESCE(r.referral_code, '') ILIKE '%%' || $%d || '%%'
		)`, len(args), len(args), len(args))
	}
	if f.OwnerUserID != "" {
		add(`COALESCE(c.owner_user_id, l.owner_user_id)::text = $%d`, f.OwnerUserID)
	}
	if f.TeamID != "" {
		add(`COALESCE(c.team_id, l.team_id)::text = $%d`, f.TeamID)
	}
	if f.PipelineID != "" {
		add(`COALESCE(c.pipeline_id, l.pipeline_id)::text = $%d`, f.PipelineID)
	}
	if f.ReferrerTypeCode != "" {
		add(`rt.code = $%d`, f.ReferrerTypeCode)
	}
	if f.ReferrerUserID != "" {
		add(`r.referrer_user_id::text = $%d`, f.ReferrerUserID)
	}
	if f.ReferrerCustomerID != "" {
		add(`r.referrer_customer_id::text = $%d`, f.ReferrerCustomerID)
	}
	if f.ReferrerPartnerID != "" {
		add(`r.referrer_partner_id::text = $%d`, f.ReferrerPartnerID)
	}
	if f.ReferrerName != "" {
		add(`COALESCE(ru.full_name, rc.full_name, rp.name, r.referrer_name, '') ILIKE '%%' || $%d || '%%'`, f.ReferrerName)
	}
	if f.StatusCode != "" {
		add(`rs.code = $%d`, f.StatusCode)
	}
	if f.DateFrom != "" {
		add(`r.referral_date >= $%d::date`, f.DateFrom)
	}
	if f.DateTo != "" {
		add(`r.referral_date <= $%d::date`, f.DateTo)
	}
	if !f.Unscoped {
		if len(f.ScopeOwnerIDs) > 0 || len(f.ScopeTeamIDs) > 0 {
			scopeParts := []string{}
			if len(f.ScopeOwnerIDs) > 0 {
				args = append(args, f.ScopeOwnerIDs)
				scopeParts = append(scopeParts, fmt.Sprintf(`COALESCE(c.owner_user_id, l.owner_user_id) = ANY($%d::uuid[])`, len(args)))
			}
			if len(f.ScopeTeamIDs) > 0 {
				args = append(args, f.ScopeTeamIDs)
				scopeParts = append(scopeParts, fmt.Sprintf(`COALESCE(c.team_id, l.team_id) = ANY($%d::uuid[])`, len(args)))
			}
			parts = append(parts, "("+strings.Join(scopeParts, " OR ")+")")
		}
	}
	return strings.Join(parts, " AND "), args
}

func (r *Repository) Summary(ctx context.Context, f ListFilter) (*SummaryMetrics, error) {
	where, args := buildListWhere(f)
	sql := `
		SELECT
			COUNT(*)::int,
			COUNT(*) FILTER (WHERE rs.code = 'active')::int,
			COUNT(*) FILTER (WHERE rs.is_converted = TRUE)::int,
			COUNT(*) FILTER (WHERE rs.code = 'unconverted' OR (rs.is_converted = FALSE AND rs.code <> 'cancelled' AND rs.code <> 'active'))::int,
			COALESCE(SUM(COALESCE(c.potential_value, l.potential_value)) FILTER (
				WHERE rs.is_converted = FALSE AND rs.code <> 'cancelled'
			), 0)::float8
		FROM referrals r
		JOIN referral_referrer_types rt ON rt.id = r.referrer_type_id
		JOIN referral_statuses rs ON rs.id = r.status_id
		LEFT JOIN leads l ON l.id = r.lead_id
		LEFT JOIN customers c ON c.id = r.customer_id
		LEFT JOIN users ru ON ru.id = r.referrer_user_id
		LEFT JOIN customers rc ON rc.id = r.referrer_customer_id
		LEFT JOIN referral_partners rp ON rp.id = r.referrer_partner_id
		WHERE ` + where
	var m SummaryMetrics
	err := r.pool.QueryRow(ctx, sql, args...).Scan(
		&m.TotalReferrals, &m.ActiveReferrals, &m.ConvertedReferrals, &m.UnconvertedReferrals, &m.ReferralPipelineValue,
	)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *Repository) ReferrerProfile(ctx context.Context, typ, id string) (*ReferrerProfile, error) {
	f := ListFilter{Limit: 100, Offset: 0, Unscoped: true}
	switch typ {
	case "user":
		f.ReferrerUserID = id
	case "customer":
		f.ReferrerCustomerID = id
	case "partner":
		f.ReferrerPartnerID = id
	default:
		return nil, fmt.Errorf("invalid referrer type")
	}
	items, _, err := r.List(ctx, f)
	if err != nil {
		return nil, err
	}
	profile := &ReferrerProfile{
		ReferrerKey: typ + ":" + id,
		History:     items,
	}
	for _, it := range items {
		profile.TotalReferrals++
		if it.StatusCode == "active" {
			profile.ActiveReferrals++
		}
		if it.IsConverted {
			profile.ConvertedReferrals++
		}
		if profile.ReferrerDisplayName == "" {
			profile.ReferrerDisplayName = it.ReferrerDisplayName
			profile.ReferrerTypeCode = it.ReferrerTypeCode
			profile.ReferrerTypeName = it.ReferrerTypeName
			profile.ReferrerUserID = it.ReferrerUserID
			profile.ReferrerCustomerID = it.ReferrerCustomerID
			profile.ReferrerPartnerID = it.ReferrerPartnerID
		}
	}
	return profile, nil
}

func (r *Repository) ListPartners(ctx context.Context, q string, limit int) ([]Partner, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, name, email, phone, company, notes, is_active, created_at, updated_at
		FROM referral_partners
		WHERE is_active = TRUE
		  AND ($1 = '' OR name ILIKE '%' || $1 || '%' OR COALESCE(company,'') ILIKE '%' || $1 || '%')
		ORDER BY name
		LIMIT $2`, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Partner
	for rows.Next() {
		var p Partner
		if err := rows.Scan(&p.ID, &p.Name, &p.Email, &p.Phone, &p.Company, &p.Notes, &p.IsActive, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repository) CreatePartner(ctx context.Context, in CreatePartnerInput) (*Partner, error) {
	id := uuid.NewString()
	var email any
	if in.Email != nil && strings.TrimSpace(*in.Email) != "" {
		e := strings.ToLower(strings.TrimSpace(*in.Email))
		email = e
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO referral_partners (id, name, email, phone, company, notes)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		id, strings.TrimSpace(in.Name), email, nilStr(in.Phone), in.Company, in.Notes)
	if err != nil {
		return nil, err
	}
	var p Partner
	err = r.pool.QueryRow(ctx, `
		SELECT id::text, name, email, phone, company, notes, is_active, created_at, updated_at
		FROM referral_partners WHERE id = $1`, id).
		Scan(&p.ID, &p.Name, &p.Email, &p.Phone, &p.Company, &p.Notes, &p.IsActive, &p.CreatedAt, &p.UpdatedAt)
	return &p, err
}

func nilUUID(s *string) any {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	return *s
}

func nilStr(s *string) any {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	return strings.TrimSpace(*s)
}
