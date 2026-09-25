package deals

import (
	"net/http"
	"strconv"

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

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	return r.RemoteAddr
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// Team leads may assign owners. Sales executives / support always own their creates
// and cannot reassign on update.
func canAssignDealOwner(roleCode string) bool {
	return roleCode == auth.RoleSalesManager || roleCode == auth.RoleSuperAdmin
}

func applyDealOwnerCreatePolicy(claims auth.Claims, in *CreateInput) {
	if canAssignDealOwner(claims.RoleCode) {
		return
	}
	id := claims.UserID
	in.OwnerUserID = &id
	if len(claims.TeamIDs) > 0 {
		tid := claims.TeamIDs[0]
		in.TeamID = &tid
	}
}

func applyDealOwnerUpdatePolicy(claims auth.Claims, in *UpdateInput) {
	if canAssignDealOwner(claims.RoleCode) {
		return
	}
	in.OwnerUserID = nil
}

func (h *Handler) applyListScope(r *http.Request, f *ListFilter, seID string) error {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		return apperrors.Unauthorized("authentication required")
	}
	vis, err := datascope.Resolve(claims, permissions.DealsView, seID)
	if err != nil {
		return err
	}
	if seID == "" && f.OwnerID != "" {
		vis, err = datascope.ApplyOwnerFilter(vis, f.OwnerID)
		if err != nil {
			return err
		}
	}
	f.ScopeUnscoped = vis.Unscoped
	f.ScopeOwnerIDs = vis.OwnerIDs
	f.ScopeTeamIDs = vis.TeamIDs
	f.OwnerID = ""
	return nil
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	seID := firstNonEmpty(q.Get("salesExecutiveId"), q.Get("sales_executive_id"))
	ownerID := q.Get("ownerUserId")
	if seID != "" && ownerID == "" {
		ownerID = seID
	}
	f := ListFilter{
		PipelineID: q.Get("pipelineId"),
		Search:     q.Get("q"),
		OwnerID:    ownerID,
		TeamID:     q.Get("teamId"),
		Status:     q.Get("status"),
		Attention:  q.Get("attention"),
		Limit:      limit,
		Offset:     offset,
	}
	if err := h.applyListScope(r, &f, seID); err != nil {
		response.Fail(w, err)
		return
	}
	items, total, err := h.service.List(r.Context(), f)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{
		Success: true, Data: items, Meta: map[string]any{"total": total, "limit": f.Limit, "offset": f.Offset},
	})
}

func (h *Handler) Board(w http.ResponseWriter, r *http.Request) {
	pipelineID := r.URL.Query().Get("pipelineId")
	if pipelineID == "" {
		response.Fail(w, apperrors.Validation("pipelineId is required"))
		return
	}
	seID := firstNonEmpty(r.URL.Query().Get("salesExecutiveId"), r.URL.Query().Get("sales_executive_id"))
	f := ListFilter{PipelineID: pipelineID, Status: "open"}
	if err := h.applyListScope(r, &f, seID); err != nil {
		response.Fail(w, err)
		return
	}
	board, err := h.service.Board(r.Context(), f)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, board)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	detail, err := h.service.GetDetail(r.Context(), r.PathValue("id"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, detail)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var in CreateInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	applyDealOwnerCreatePolicy(claims, &in)
	d, err := h.service.Create(r.Context(), claims.UserID, in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.Created(w, d)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var in UpdateInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	applyDealOwnerUpdatePolicy(claims, &in)
	d, err := h.service.Update(r.Context(), claims.UserID, r.PathValue("id"), in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, d)
}

func (h *Handler) Move(w http.ResponseWriter, r *http.Request) {
	var in MoveInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		response.Fail(w, apperrors.Unauthorized("authentication required"))
		return
	}
	d, err := h.service.Move(r.Context(), claims, r.PathValue("id"), in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, d)
}

func (h *Handler) AddDocument(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		Category string `json:"category"`
	}
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	if err := h.service.AddDocument(r.Context(), claims.UserID, r.PathValue("id"), in.Name, in.Category); err != nil {
		response.Fail(w, err)
		return
	}
	detail, err := h.service.GetDetail(r.Context(), r.PathValue("id"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, detail)
}
