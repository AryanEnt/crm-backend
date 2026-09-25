package auth

import (
	"encoding/json"
	"net/http"

	"github.com/crm/backend/internal/config"
	"github.com/crm/backend/pkg/apperrors"
	"github.com/crm/backend/pkg/response"
	"github.com/crm/backend/pkg/validate"
)

type Handler struct {
	service *Service
	cfg     *config.Config
}

func NewHandler(service *Service, cfg *config.Config) *Handler {
	return &Handler{service: service, cfg: cfg}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authResponse struct {
	User             *SessionUser `json:"user"`
	AccessToken      string       `json:"accessToken"`
	RefreshToken     string       `json:"refreshToken"`
	AccessExpiresAt  string       `json:"accessExpiresAt"`
	RefreshExpiresAt string       `json:"refreshExpiresAt"`
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := validate.DecodeJSON(r, &req); err != nil {
		response.Fail(w, err)
		return
	}
	pair, user, err := h.service.Login(r.Context(), req.Email, req.Password, r.UserAgent(), clientIP(r))
	if err != nil {
		response.Fail(w, err)
		return
	}
	SetAuthCookies(w, h.cfg, pair)
	response.OK(w, authResponse{
		User:             user,
		AccessToken:      pair.AccessToken,
		RefreshToken:     pair.RefreshToken,
		AccessExpiresAt:  pair.AccessExpiresAt.Format(timeRFC3339),
		RefreshExpiresAt: pair.RefreshExpiresAt.Format(timeRFC3339),
	})
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	_ = h.service.Logout(r.Context(), RefreshTokenFromRequest(r))
	ClearAuthCookies(w, h.cfg)
	response.OK(w, map[string]bool{"loggedOut": true})
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	pair, user, err := h.service.Refresh(r.Context(), RefreshTokenFromRequest(r), r.UserAgent(), clientIP(r))
	if err != nil {
		ClearAuthCookies(w, h.cfg)
		response.Fail(w, err)
		return
	}
	SetAuthCookies(w, h.cfg, pair)
	response.OK(w, authResponse{
		User:             user,
		AccessToken:      pair.AccessToken,
		RefreshToken:     pair.RefreshToken,
		AccessExpiresAt:  pair.AccessExpiresAt.Format(timeRFC3339),
		RefreshExpiresAt: pair.RefreshExpiresAt.Format(timeRFC3339),
	})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	claims, ok := ClaimsFromContext(r.Context())
	if !ok {
		response.Fail(w, apperrors.Unauthorized("authentication required"))
		return
	}
	user, err := h.service.Me(r.Context(), mustAccess(r))
	if err != nil {
		response.Fail(w, err)
		return
	}
	_ = claims
	response.OK(w, user)
}

func (h *Handler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	claims, ok := ClaimsFromContext(r.Context())
	if !ok {
		response.Fail(w, apperrors.Unauthorized("authentication required"))
		return
	}
	var in UpdateProfileInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	user, err := h.service.UpdateProfile(r.Context(), claims.UserID, in)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, user)
}

func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	claims, ok := ClaimsFromContext(r.Context())
	if !ok {
		response.Fail(w, apperrors.Unauthorized("authentication required"))
		return
	}
	var in ChangePasswordInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	pair, user, err := h.service.ChangePassword(r.Context(), claims.UserID, in, r.UserAgent(), clientIP(r))
	if err != nil {
		response.Fail(w, err)
		return
	}
	SetAuthCookies(w, h.cfg, pair)
	response.OK(w, authResponse{
		User:             user,
		AccessToken:      pair.AccessToken,
		RefreshToken:     pair.RefreshToken,
		AccessExpiresAt:  pair.AccessExpiresAt.Format(timeRFC3339),
		RefreshExpiresAt: pair.RefreshExpiresAt.Format(timeRFC3339),
	})
}

func mustAccess(r *http.Request) string {
	return extractBearer(r)
}

const timeRFC3339 = "2006-01-02T15:04:05Z07:00"

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	return r.RemoteAddr
}

// Decode optional body helpers for empty logout bodies.
func decodeOptional(r *http.Request, dest any) error {
	if r.Body == nil {
		return nil
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return nil
	}
	return nil
}
