package predictions

import (
	"net/http"
	"strconv"

	"github.com/crm/backend/internal/auth"
	"github.com/crm/backend/pkg/response"
)

type Handler struct {
	service *PredictionService
}

func NewHandler(service *PredictionService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Catalog(w http.ResponseWriter, r *http.Request) {
	response.OK(w, h.service.Catalog())
}

func (h *Handler) ScoreLead(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	res, err := h.service.ScoreLead(r.Context(), claims.UserID, r.PathValue("id"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, res)
}

func (h *Handler) LeadScoreHistory(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	res, err := h.service.LeadScoreHistory(r.Context(), r.PathValue("id"), limit)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, res)
}

func (h *Handler) LeadInsights(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	res, err := h.service.LeadInsights(r.Context(), claims.UserID, r.PathValue("id"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, res)
}

func (h *Handler) DealInsights(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	res, err := h.service.DealInsights(r.Context(), claims.UserID, r.PathValue("id"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, res)
}

func (h *Handler) Workload(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	owner := r.URL.Query().Get("ownerUserId")
	if owner == "" {
		owner = claims.UserID
	}
	res, err := h.service.Workload(r.Context(), claims.UserID, owner)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, res)
}

func (h *Handler) PipelineRisk(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	res, err := h.service.PipelineRisk(r.Context(), claims.UserID, r.URL.Query().Get("pipelineId"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, res)
}
