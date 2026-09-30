package documents

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/crm/backend/internal/auth"
	"github.com/crm/backend/internal/datascope"
)

const (
	StatusRequested = "requested"
	StatusUploaded  = "uploaded"
	StatusVerified  = "verified"
	StatusRejected  = "rejected"
	StatusMissing   = "missing"
	StatusExpired   = "expired"
)

type Document struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	DocType         string     `json:"type"`
	Category        string     `json:"category"`
	Status          string     `json:"status"`
	MimeType        string     `json:"mimeType"`
	SizeBytes       int64      `json:"sizeBytes"`
	FileKey         string     `json:"fileKey"`
	StorageProvider string     `json:"storageProvider"`
	Notes           string     `json:"notes"`
	RejectedReason  string     `json:"rejectedReason"`
	LeadID          *string    `json:"leadId"`
	CustomerID      *string    `json:"customerId"`
	CustomerName    *string    `json:"customerName"`
	DealID          *string    `json:"dealId"`
	DealTitle       *string    `json:"dealTitle"`
	UploadedByID    *string    `json:"uploadedBy"`
	UploadedByName  *string    `json:"uploadedByName"`
	VerifiedByID    *string    `json:"verifiedBy"`
	VerifiedByName  *string    `json:"verifiedByName"`
	RequestedAt     *time.Time `json:"requestedDate"`
	UploadedAt      *time.Time `json:"uploadedDate"`
	ExpiresAt       *time.Time `json:"expiryDate"`
	VerifiedAt      *time.Time `json:"verifiedAt"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

type ListFilter struct {
	CustomerID string
	DealID     string
	LeadID     string
	Status     string
	DocType    string
	Q          string
	Limit      int
	Offset     int
	// Scope and Viewer restrict rows to documents the caller can see.
	Scope  datascope.Visibility
	Viewer auth.Claims
}

type RequestInput struct {
	Name       string  `json:"name"`
	DocType    string  `json:"type"`
	Category   string  `json:"category"`
	LeadID     *string `json:"leadId"`
	CustomerID *string `json:"customerId"`
	DealID     *string `json:"dealId"`
	ExpiresAt  *string `json:"expiryDate"`
	Notes      string  `json:"notes"`
	Status     string  `json:"status"` // requested | missing
}

type UpdateMetaInput struct {
	Name      *string `json:"name"`
	DocType   *string `json:"type"`
	Category  *string `json:"category"`
	ExpiresAt *string `json:"expiryDate"`
	Notes     *string `json:"notes"`
	Status    *string `json:"status"`
}

type RejectInput struct {
	Reason string `json:"reason"`
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const docSelect = `
	SELECT d.id::text, d.name, d.doc_type, d.category, d.status, d.mime_type, d.size_bytes,
		d.file_key, d.storage_provider, d.notes, d.rejected_reason,
		d.lead_id::text, d.customer_id::text, c.full_name, d.deal_id::text, deal.title,
		d.uploaded_by::text, ub.full_name, d.verified_by::text, vb.full_name,
		d.requested_at, d.uploaded_at, d.expires_at, d.verified_at,
		d.created_at, d.updated_at
	FROM documents d
	LEFT JOIN customers c ON c.id = d.customer_id
	LEFT JOIN deals deal ON deal.id = d.deal_id
	LEFT JOIN users ub ON ub.id = d.uploaded_by
	LEFT JOIN users vb ON vb.id = d.verified_by
`

func (r *Repository) Get(ctx context.Context, id string) (*Document, error) {
	row := r.pool.QueryRow(ctx, docSelect+` WHERE d.id=$1`, id)
	return scanDoc(row)
}

func (r *Repository) List(ctx context.Context, f ListFilter) ([]Document, int, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 50
	}
	args := []any{}
	where := []string{"1=1"}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.Replace(cond, "?", "$"+itoa(len(args)), 1))
	}
	if f.CustomerID != "" {
		add("d.customer_id::text = ?", f.CustomerID)
	}
	if f.DealID != "" {
		add("d.deal_id::text = ?", f.DealID)
	}
	if f.LeadID != "" {
		add("d.lead_id::text = ?", f.LeadID)
	}
	if f.Status != "" {
		add("d.status = ?", f.Status)
	}
	if f.DocType != "" {
		add("d.doc_type = ?", f.DocType)
	}
	if f.Q != "" {
		args = append(args, "%"+strings.ToLower(f.Q)+"%")
		where = append(where, "(lower(d.name) LIKE $"+itoa(len(args))+" OR lower(d.doc_type) LIKE $"+itoa(len(args))+")")
	}
	where, args = datascope.Documents.AppendScope(where, args, f.Scope, f.Viewer)
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM documents d WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, f.Limit, f.Offset)
	rows, err := r.pool.Query(ctx, docSelect+`
		WHERE `+whereSQL+`
		ORDER BY COALESCE(d.uploaded_at, d.requested_at, d.created_at) DESC
		LIMIT $`+itoa(len(args)-1)+` OFFSET $`+itoa(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var items []Document
	for rows.Next() {
		d, err := scanDoc(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *d)
	}
	if items == nil {
		items = []Document{}
	}
	return items, total, rows.Err()
}

func (r *Repository) InsertRequested(ctx context.Context, in RequestInput, expires *time.Time, status string) (*Document, error) {
	id := uuid.NewString()
	docType := strings.TrimSpace(in.DocType)
	if docType == "" {
		docType = strings.TrimSpace(in.Category)
	}
	category := strings.TrimSpace(in.Category)
	if category == "" {
		category = docType
	}
	now := time.Now().UTC()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO documents (
			id, name, category, doc_type, status, requested_at, expires_at, notes,
			lead_id, customer_id, deal_id
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
	`, id, strings.TrimSpace(in.Name), category, docType, status, now, expires, in.Notes,
		emptyToNil(in.LeadID), emptyToNil(in.CustomerID), emptyToNil(in.DealID))
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *Repository) MarkUploaded(ctx context.Context, id, actorID, fileKey, provider, mime string, size int64) (*Document, error) {
	now := time.Now().UTC()
	_, err := r.pool.Exec(ctx, `
		UPDATE documents SET
			file_key=$2, storage_provider=$3, mime_type=$4, size_bytes=$5,
			uploaded_by=$6, uploaded_at=$7, status='uploaded',
			rejected_reason='', verified_by=NULL, verified_at=NULL
		WHERE id=$1
	`, id, fileKey, provider, mime, size, emptyToNil(&actorID), now)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *Repository) CreateUploaded(ctx context.Context, name, docType, category, actorID, fileKey, provider, mime string, size int64, leadID, customerID, dealID *string, expires *time.Time, notes string) (*Document, error) {
	id := uuid.NewString()
	now := time.Now().UTC()
	if docType == "" {
		docType = category
	}
	if category == "" {
		category = docType
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO documents (
			id, name, category, doc_type, status, file_key, storage_provider, mime_type, size_bytes,
			uploaded_by, uploaded_at, expires_at, notes, lead_id, customer_id, deal_id
		) VALUES ($1,$2,$3,$4,'uploaded',$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
	`, id, name, category, docType, fileKey, provider, mime, size,
		emptyToNil(&actorID), now, expires, notes,
		emptyToNil(leadID), emptyToNil(customerID), emptyToNil(dealID))
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *Repository) Verify(ctx context.Context, id, actorID string) (*Document, error) {
	now := time.Now().UTC()
	_, err := r.pool.Exec(ctx, `
		UPDATE documents SET status='verified', verified_by=$2, verified_at=$3, rejected_reason=''
		WHERE id=$1
	`, id, actorID, now)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *Repository) Reject(ctx context.Context, id, actorID, reason string) (*Document, error) {
	now := time.Now().UTC()
	_, err := r.pool.Exec(ctx, `
		UPDATE documents SET status='rejected', verified_by=$2, verified_at=$3, rejected_reason=$4
		WHERE id=$1
	`, id, actorID, now, reason)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *Repository) UpdateMeta(ctx context.Context, id string, current *Document, in UpdateMetaInput, expires *time.Time, setExpires bool) (*Document, error) {
	name := current.Name
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	docType := current.DocType
	if in.DocType != nil {
		docType = strings.TrimSpace(*in.DocType)
	}
	category := current.Category
	if in.Category != nil {
		category = strings.TrimSpace(*in.Category)
	}
	notes := current.Notes
	if in.Notes != nil {
		notes = *in.Notes
	}
	status := current.Status
	if in.Status != nil {
		status = strings.TrimSpace(*in.Status)
	}
	exp := current.ExpiresAt
	if setExpires {
		exp = expires
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE documents SET name=$2, doc_type=$3, category=$4, notes=$5, status=$6, expires_at=$7
		WHERE id=$1
	`, id, name, docType, category, notes, status, exp)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM documents WHERE id=$1`, id)
	return err
}

func (r *Repository) RefreshExpired(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE documents SET status='expired'
		WHERE expires_at IS NOT NULL AND expires_at < NOW()
		  AND status IN ('uploaded','verified','requested')
	`)
	return err
}

type scannable interface {
	Scan(dest ...any) error
}

func scanDoc(row scannable) (*Document, error) {
	var d Document
	err := row.Scan(
		&d.ID, &d.Name, &d.DocType, &d.Category, &d.Status, &d.MimeType, &d.SizeBytes,
		&d.FileKey, &d.StorageProvider, &d.Notes, &d.RejectedReason,
		&d.LeadID, &d.CustomerID, &d.CustomerName, &d.DealID, &d.DealTitle,
		&d.UploadedByID, &d.UploadedByName, &d.VerifiedByID, &d.VerifiedByName,
		&d.RequestedAt, &d.UploadedAt, &d.ExpiresAt, &d.VerifiedAt,
		&d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
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
