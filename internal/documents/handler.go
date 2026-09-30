package documents

import (
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/crm/backend/internal/auth"
	"github.com/crm/backend/internal/datascope"
	"github.com/crm/backend/internal/permissions"
	"github.com/crm/backend/pkg/apperrors"
	"github.com/crm/backend/pkg/response"
	"github.com/crm/backend/pkg/validate"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	claims, _ := auth.ClaimsFromContext(r.Context())
	vis, err := datascope.Resolve(claims, permissions.DocumentsView, "")
	if err != nil {
		response.Fail(w, err)
		return
	}
	items, total, err := h.service.List(r.Context(), ListFilter{
		Scope:      vis,
		Viewer:     claims,
		CustomerID: q.Get("customerId"),
		DealID:     q.Get("dealId"),
		LeadID:     q.Get("leadId"),
		Status:     q.Get("status"),
		DocType:    q.Get("type"),
		Q:          q.Get("q"),
		Limit:      limit,
		Offset:     offset,
	})
	if err != nil {
		response.Fail(w, err)
		return
	}
	if limit <= 0 {
		limit = 50
	}
	response.JSON(w, http.StatusOK, response.Envelope{
		Success: true,
		Data:    items,
		Meta: map[string]any{
			"total": total, "limit": limit, "offset": offset,
			"hasMore": offset+len(items) < total,
		},
	})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	d, err := h.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, d)
}

func (h *Handler) Request(w http.ResponseWriter, r *http.Request) {
	var in RequestInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	if err := h.requireParents(r, claims, in.LeadID, in.CustomerID, in.DealID); err != nil {
		response.Fail(w, err)
		return
	}
	d, err := h.service.Request(r.Context(), claims.UserID, in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.Created(w, d)
}

func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		response.Fail(w, apperrors.Validation("invalid multipart form"))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		response.Fail(w, apperrors.Validation("file is required"))
		return
	}
	defer file.Close()

	mime := header.Header.Get("Content-Type")
	if mime == "" {
		mime = "application/octet-stream"
	}
	filename := header.Filename
	docID := strings.TrimSpace(r.FormValue("documentId"))
	if docID == "" {
		docID = r.PathValue("id")
	}

	if docID != "" {
		if err := datascope.RequireRecord(r.Context(), h.service.repo.pool, claims, datascope.Documents, docID, permissions.DocumentsCreate); err != nil {
			response.Fail(w, err)
			return
		}
		d, err := h.service.UploadToExisting(r.Context(), claims.UserID, docID, filename, mime, header.Size, file, clientIP(r), r.UserAgent())
		if err != nil {
			response.Fail(w, err)
			return
		}
		response.OK(w, d)
		return
	}

	name := r.FormValue("name")
	if name == "" {
		name = filename
	}
	docType := r.FormValue("type")
	if docType == "" {
		docType = r.FormValue("category")
	}
	category := r.FormValue("category")
	var leadID, customerID, dealID, expires *string
	if v := r.FormValue("leadId"); v != "" {
		leadID = &v
	}
	if v := r.FormValue("customerId"); v != "" {
		customerID = &v
	}
	if v := r.FormValue("dealId"); v != "" {
		dealID = &v
	}
	if v := r.FormValue("expiryDate"); v != "" {
		expires = &v
	}
	notes := r.FormValue("notes")
	if err := h.requireParents(r, claims, leadID, customerID, dealID); err != nil {
		response.Fail(w, err)
		return
	}
	d, err := h.service.UploadNew(r.Context(), claims.UserID, name, docType, category, leadID, customerID, dealID, expires, notes, filename, mime, header.Size, file, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.Created(w, d)
}

// requireParents 404s when a document would be attached to a lead, customer or deal
// the caller can't see.
func (h *Handler) requireParents(r *http.Request, claims auth.Claims, leadID, customerID, dealID *string) error {
	parents := []struct {
		id  *string
		rec datascope.Record
	}{{leadID, datascope.Leads}, {customerID, datascope.Customers}, {dealID, datascope.Deals}}
	for _, p := range parents {
		if p.id == nil || strings.TrimSpace(*p.id) == "" {
			continue
		}
		if err := datascope.RequireRecord(r.Context(), h.service.repo.pool, claims, p.rec, strings.TrimSpace(*p.id), permissions.DocumentsCreate); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
	rc, d, err := h.service.Open(r.Context(), r.PathValue("id"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	defer rc.Close()
	mime := d.MimeType
	if mime == "" {
		mime = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", `attachment; filename="`+path.Base(d.Name)+`"`)
	if d.SizeBytes > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(d.SizeBytes, 10))
	}
	_, _ = io.Copy(w, rc)
}

func (h *Handler) Verify(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	d, err := h.service.Verify(r.Context(), claims.UserID, r.PathValue("id"), clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, d)
}

func (h *Handler) Reject(w http.ResponseWriter, r *http.Request) {
	var in RejectInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	d, err := h.service.Reject(r.Context(), claims.UserID, r.PathValue("id"), in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, d)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var in UpdateMetaInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	d, err := h.service.Update(r.Context(), claims.UserID, r.PathValue("id"), in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, d)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	if err := h.service.Delete(r.Context(), claims.UserID, r.PathValue("id"), clientIP(r), r.UserAgent()); err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, map[string]any{"deleted": true})
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		return host[:i]
	}
	return host
}
