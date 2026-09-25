package leads

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
	inactive, _ := strconv.Atoi(q.Get("inactiveDays"))
	seID := firstNonEmpty(q.Get("salesExecutiveId"), q.Get("sales_executive_id"))
	ownerID := q.Get("ownerUserId")
	if seID != "" && ownerID == "" {
		ownerID = seID
	}
	f := ListFilter{
		Search: q.Get("q"), OwnerUserID: ownerID, TeamID: q.Get("teamId"),
		PipelineID: q.Get("pipelineId"), StageID: q.Get("stageId"), Source: q.Get("source"),
		Priority: q.Get("priority"), AnzscoID: q.Get("anzscoId"), Tag: q.Get("tag"),
		CreatedFrom: q.Get("createdFrom"), CreatedTo: q.Get("createdTo"),
		InactiveDays: inactive, IncludeArchived: q.Get("includeArchived") == "true",
		Status: q.Get("status"), Attention: q.Get("attention"),
		Sort: q.Get("sort"), Order: q.Get("order"), Limit: limit, Offset: offset,
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
		Success: true,
		Data:    items,
		Meta:    map[string]any{"total": total, "limit": f.Limit, "offset": f.Offset},
	})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	lead, err := h.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, lead)
}

func (h *Handler) Qualify(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	lead, err := h.service.Qualify(r.Context(), claims.UserID, r.PathValue("id"), clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, lead)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var in CreateInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	result, err := h.service.Create(r.Context(), claims.UserID, in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	if result.NeedsReview {
		response.JSON(w, http.StatusConflict, response.Envelope{
			Success: false,
			Data:    result,
			Error: &response.ErrorBody{
				Code:    string(apperrors.CodeConflict),
				Message: "Potential duplicates found. Review and confirm to continue.",
			},
		})
		return
	}
	response.Created(w, result.Lead)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var in UpdateInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	result, err := h.service.Update(r.Context(), claims.UserID, r.PathValue("id"), in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	if result.NeedsReview {
		response.JSON(w, http.StatusConflict, response.Envelope{
			Success: false,
			Data:    result,
			Error: &response.ErrorBody{
				Code:    string(apperrors.CodeConflict),
				Message: "Potential duplicates found. Review and confirm to continue.",
			},
		})
		return
	}
	response.OK(w, result.Lead)
}

func (h *Handler) BulkArchive(w http.ResponseWriter, r *http.Request) {
	var in BulkArchiveInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	n, err := h.service.Archive(r.Context(), claims.UserID, in.IDs, in.Archive, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, map[string]any{"updated": n})
}

func (h *Handler) BulkAssign(w http.ResponseWriter, r *http.Request) {
	var in BulkAssignInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	n, err := h.service.BulkAssign(r.Context(), claims.UserID, in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, map[string]any{"updated": n})
}

func (h *Handler) BulkStage(w http.ResponseWriter, r *http.Request) {
	var in BulkStageInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	n, err := h.service.BulkStage(r.Context(), claims.UserID, in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, map[string]any{"updated": n})
}

type duplicateQuery struct {
	Email *string `json:"email"`
	Phone *string `json:"phone"`
	Name  *string `json:"name"`
}

func (h *Handler) CheckDuplicates(w http.ResponseWriter, r *http.Request) {
	var in duplicateQuery
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	items, err := h.service.CheckDuplicates(r.Context(), r.URL.Query().Get("excludeId"), in.Email, in.Phone, in.Name)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, items)
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
	vis, err := datascope.Resolve(claims, permissions.LeadsView, seID)
	if err != nil {
		return err
	}
	if seID == "" && f.OwnerUserID != "" {
		vis, err = datascope.ApplyOwnerFilter(vis, f.OwnerUserID)
		if err != nil {
			return err
		}
	}
	// Client team_id is never an authorization grant — only an optional narrow within scope
	if f.TeamID != "" && !vis.Unscoped {
		if len(vis.TeamIDs) > 0 {
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
	}
	f.ScopeUnscoped = vis.Unscoped
	f.ScopeOwnerIDs = vis.OwnerIDs
	f.ScopeTeamIDs = vis.TeamIDs
	// Avoid double-filtering: ownership is enforced via Scope*
	f.OwnerUserID = ""
	if !vis.Unscoped {
		f.TeamID = ""
	}
	return nil
}
