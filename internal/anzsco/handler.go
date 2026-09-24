package anzsco

import (
	"net/http"
	"strconv"

	"github.com/crm/backend/pkg/response"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.service.Search(r.Context(), r.URL.Query().Get("q"), limit)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, items)
}
