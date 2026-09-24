package automation

import (
	"net/http"
	"strconv"

	"github.com/crm/backend/internal/auth"
	"github.com/crm/backend/pkg/response"
	"github.com/crm/backend/pkg/validate"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Catalog(w http.ResponseWriter, r *http.Request) {
	response.OK(w, h.service.Catalog())
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	activeOnly := q.Get("activeOnly") == "true"
	items, total, err := h.service.List(r.Context(), q.Get("triggerType"), activeOnly, limit, offset)
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
		Meta:    map[string]any{"total": total, "limit": limit, "offset": offset},
	})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	a, err := h.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, a)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var in CreateInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	a, err := h.service.Create(r.Context(), claims.UserID, in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: a})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var in UpdateInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	a, err := h.service.Update(r.Context(), claims.UserID, r.PathValue("id"), in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, a)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	if err := h.service.Delete(r.Context(), claims.UserID, r.PathValue("id"), clientIP(r), r.UserAgent()); err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, map[string]any{"deleted": true})
}

func (h *Handler) ListRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	items, total, err := h.service.ListRuns(r.Context(), q.Get("automationId"), q.Get("status"), limit, offset)
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
		Meta:    map[string]any{"total": total, "limit": limit, "offset": offset},
	})
}

func (h *Handler) ListJobs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	items, total, err := h.service.ListJobs(r.Context(), q.Get("status"), limit, offset)
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
		Meta:    map[string]any{"total": total, "limit": limit, "offset": offset},
	})
}

func (h *Handler) RetryJob(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	job, err := h.service.RetryJob(r.Context(), claims.UserID, r.PathValue("id"), clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, job)
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	return r.RemoteAddr
}
