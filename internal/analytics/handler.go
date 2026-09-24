package analytics

import (
	"net/http"

	"github.com/crm/backend/pkg/apperrors"
	"github.com/crm/backend/pkg/response"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) filterFromRequest(r *http.Request) (Filter, error) {
	q := map[string]string{}
	for _, k := range []string{
		"from", "to", "pipelineId", "teamId", "ownerUserId", "source", "anzscoId",
		"groupBy", "period", "sortBy", "sortDir",
	} {
		q[k] = r.URL.Query().Get(k)
	}
	f, err := ParseFilter(q)
	if err != nil {
		return f, apperrors.Validation(err.Error())
	}
	return f, nil
}

func (h *Handler) Funnel(w http.ResponseWriter, r *http.Request) {
	f, err := h.filterFromRequest(r)
	if err != nil {
		response.Fail(w, err)
		return
	}
	res, err := h.service.Funnel(r.Context(), f)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, res)
}

func (h *Handler) Summary(w http.ResponseWriter, r *http.Request) {
	f, err := h.filterFromRequest(r)
	if err != nil {
		response.Fail(w, err)
		return
	}
	res, err := h.service.Summary(r.Context(), f)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, res)
}

func (h *Handler) Leads(w http.ResponseWriter, r *http.Request) {
	f, err := h.filterFromRequest(r)
	if err != nil {
		response.Fail(w, err)
		return
	}
	res, err := h.service.Leads(r.Context(), f)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, res)
}

func (h *Handler) Pipeline(w http.ResponseWriter, r *http.Request) {
	f, err := h.filterFromRequest(r)
	if err != nil {
		response.Fail(w, err)
		return
	}
	res, err := h.service.Pipeline(r.Context(), f)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, res)
}

func (h *Handler) Activities(w http.ResponseWriter, r *http.Request) {
	f, err := h.filterFromRequest(r)
	if err != nil {
		response.Fail(w, err)
		return
	}
	res, err := h.service.Activities(r.Context(), f)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, res)
}

func (h *Handler) Conversions(w http.ResponseWriter, r *http.Request) {
	f, err := h.filterFromRequest(r)
	if err != nil {
		response.Fail(w, err)
		return
	}
	res, err := h.service.Conversions(r.Context(), f)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, res)
}

func (h *Handler) Teams(w http.ResponseWriter, r *http.Request) {
	f, err := h.filterFromRequest(r)
	if err != nil {
		response.Fail(w, err)
		return
	}
	res, err := h.service.Teams(r.Context(), f)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, res)
}

func (h *Handler) Sources(w http.ResponseWriter, r *http.Request) {
	f, err := h.filterFromRequest(r)
	if err != nil {
		response.Fail(w, err)
		return
	}
	res, err := h.service.Sources(r.Context(), f)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, res)
}

func (h *Handler) Organization(w http.ResponseWriter, r *http.Request) {
	f, err := h.filterFromRequest(r)
	if err != nil {
		response.Fail(w, err)
		return
	}
	res, err := h.service.Organization(r.Context(), f)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, res)
}
