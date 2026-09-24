package forecasting

import (
	"net/http"

	"github.com/crm/backend/pkg/response"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) PipelineForecast(w http.ResponseWriter, r *http.Request) {
	q := map[string]string{}
	for _, k := range []string{"pipelineId", "teamId", "ownerUserId", "from", "to", "historyFrom", "historyTo"} {
		q[k] = r.URL.Query().Get(k)
	}
	req, err := ParseRequest(q)
	if err != nil {
		response.Fail(w, err)
		return
	}
	res, err := h.service.Forecast(r.Context(), req)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, res)
}

func (h *Handler) Methodology(w http.ResponseWriter, r *http.Request) {
	response.OK(w, map[string]any{
		"disclaimer": Disclaimer,
		"minClosedSample": MinClosedSample,
		"insufficientMessage": InsufficientMessage,
		"methods": []Methodology{{
			Code:    MethodologyWeightedPipelineV1,
			Name:    "Weighted pipeline (v1)",
			Version: "1",
			Description: "Open deal values weighted by stage probability blended with historical win rate. " +
				"Conservative 0.7×, higher-case 1.3× (capped). Future statistical/ML models can be added under new methodology codes without changing this response shape.",
		}},
	})
}
