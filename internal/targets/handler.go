package targets

import (
	"net/http"
	"strconv"
	"strings"

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

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	items, total, err := h.service.List(r.Context(), ListFilter{
		PeriodType: q.Get("periodType"),
		Metric:     q.Get("metric"),
		ScopeType:  q.Get("scopeType"),
		TeamID:     q.Get("teamId"),
		UserID:     q.Get("userId"),
		PipelineID: q.Get("pipelineId"),
		From:       q.Get("from"),
		To:         q.Get("to"),
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
		Meta:    map[string]any{"total": total, "limit": limit, "offset": offset},
	})
}

func (h *Handler) ListProgress(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	items, total, err := h.service.ListProgress(r.Context(), ListFilter{
		PeriodType: q.Get("periodType"),
		Metric:     q.Get("metric"),
		ScopeType:  q.Get("scopeType"),
		TeamID:     q.Get("teamId"),
		UserID:     q.Get("userId"),
		PipelineID: q.Get("pipelineId"),
		From:       q.Get("from"),
		To:         q.Get("to"),
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
		Meta:    map[string]any{"total": total, "limit": limit, "offset": offset},
	})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	t, err := h.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, t)
}

func (h *Handler) Progress(w http.ResponseWriter, r *http.Request) {
	p, err := h.service.Progress(r.Context(), r.PathValue("id"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, p)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var in CreateInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	t, err := h.service.Create(r.Context(), claims.UserID, in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.Created(w, t)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var in UpdateInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	t, err := h.service.Update(r.Context(), claims.UserID, r.PathValue("id"), in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, t)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	if err := h.service.Delete(r.Context(), claims.UserID, r.PathValue("id"), clientIP(r), r.UserAgent()); err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, map[string]any{"deleted": true})
}

func (h *Handler) Metrics(w http.ResponseWriter, r *http.Request) {
	response.OK(w, h.service.MetricsCatalog(r.Context()))
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		return host[:i]
	}
	return host
}
