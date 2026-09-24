package customers

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
		Search: q.Get("q"), OwnerUserID: ownerID, TeamID: q.Get("teamId"),
		Source: q.Get("source"), Country: q.Get("country"),
		CreatedFrom: q.Get("createdFrom"), CreatedTo: q.Get("createdTo"),
		Limit: limit, Offset: offset,
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

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	c, err := h.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, c)
}

func (h *Handler) Get360(w http.ResponseWriter, r *http.Request) {
	profile, err := h.service.Get360(r.Context(), r.PathValue("id"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, profile)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var in CreateInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	applyCustomerOwnerCreatePolicy(claims, &in)
	c, dups, err := h.service.Create(r.Context(), claims.UserID, in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	if dups != nil {
		response.JSON(w, http.StatusConflict, response.Envelope{
			Success: false,
			Data:    map[string]any{"duplicates": dups, "needsReview": true},
			Error: &response.ErrorBody{
				Code: string(apperrors.CodeConflict), Message: "Potential duplicates found. Review and confirm to continue.",
			},
		})
		return
	}
	response.Created(w, c)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var in UpdateInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	applyCustomerOwnerUpdatePolicy(claims, &in)
	c, dups, err := h.service.Update(r.Context(), claims.UserID, r.PathValue("id"), in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	if dups != nil {
		response.JSON(w, http.StatusConflict, response.Envelope{
			Success: false,
			Data:    map[string]any{"duplicates": dups, "needsReview": true},
			Error: &response.ErrorBody{
				Code: string(apperrors.CodeConflict), Message: "Potential duplicates found. Review and confirm to continue.",
			},
		})
		return
	}
	response.OK(w, c)
}

// Team leads may assign owners. Sales executives / support always own their creates
// and cannot reassign on update.
func canAssignCustomerOwner(roleCode string) bool {
	return roleCode == auth.RoleSalesManager || roleCode == auth.RoleSuperAdmin
}

func applyCustomerOwnerCreatePolicy(claims auth.Claims, in *CreateInput) {
	if canAssignCustomerOwner(claims.RoleCode) {
		return
	}
	id := claims.UserID
	in.OwnerUserID = &id
}

func applyCustomerOwnerUpdatePolicy(claims auth.Claims, in *UpdateInput) {
	if canAssignCustomerOwner(claims.RoleCode) {
		return
	}
	in.OwnerUserID = nil
}

func (h *Handler) ConvertLead(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	c, err := h.service.ConvertFromLead(r.Context(), claims.UserID, r.PathValue("id"), clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.Created(w, c)
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

func (h *Handler) applyListScope(r *http.Request, f *ListFilter, seID string) error {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		return apperrors.Unauthorized("authentication required")
	}
	vis, err := datascope.Resolve(claims, permissions.CustomersView, seID)
	if err != nil {
		return err
	}
	if seID == "" && f.OwnerUserID != "" {
		vis, err = datascope.ApplyOwnerFilter(vis, f.OwnerUserID)
		if err != nil {
			return err
		}
	}
	if f.TeamID != "" && !vis.Unscoped && len(vis.TeamIDs) > 0 {
		allowed := false
		for _, tid := range vis.TeamIDs {
			if tid == f.TeamID {
				allowed = true
				break
			}
		}
		if !allowed {
			return apperrors.Forbidden("team is outside your scope")
		}
		vis.TeamIDs = []string{f.TeamID}
	}
	f.ScopeUnscoped = vis.Unscoped
	f.ScopeOwnerIDs = vis.OwnerIDs
	f.ScopeTeamIDs = vis.TeamIDs
	f.OwnerUserID = ""
	if !vis.Unscoped {
		f.TeamID = ""
	}
	return nil
}
