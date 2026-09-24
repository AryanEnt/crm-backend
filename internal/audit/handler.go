package audit

import (
	"net/http"
	"strconv"

	"github.com/crm/backend/pkg/apperrors"
	"github.com/crm/backend/pkg/response"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	items, total, err := h.service.List(r.Context(), ListFilter{
		Search: r.URL.Query().Get("q"),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		response.Fail(w, apperrors.Internal("failed to list audit logs", err))
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{
		Success: true,
		Data:    items,
		Meta: map[string]any{
			"total":  total,
			"limit":  limit,
			"offset": offset,
		},
	})
}
