package auth

import (
	"net/http"
	"time"

	"github.com/crm/backend/internal/config"
)

func SetAuthCookies(w http.ResponseWriter, cfg *config.Config, pair *TokenPair) {
	secure := !cfg.IsDevelopment()
	sameSite := http.SameSiteLaxMode
	if !cfg.IsDevelopment() {
		sameSite = http.SameSiteNoneMode
	}

	http.SetCookie(w, &http.Cookie{
		Name:     AccessCookie,
		Value:    pair.AccessToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
		Expires:  pair.AccessExpiresAt,
		MaxAge:   int(time.Until(pair.AccessExpiresAt).Seconds()),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     RefreshCookie,
		Value:    pair.RefreshToken,
		Path:     "/api/v1/auth",
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
		Expires:  pair.RefreshExpiresAt,
		MaxAge:   int(time.Until(pair.RefreshExpiresAt).Seconds()),
	})
}

func ClearAuthCookies(w http.ResponseWriter, cfg *config.Config) {
	secure := !cfg.IsDevelopment()
	sameSite := http.SameSiteLaxMode
	if !cfg.IsDevelopment() {
		sameSite = http.SameSiteNoneMode
	}
	http.SetCookie(w, &http.Cookie{
		Name: AccessCookie, Value: "", Path: "/", HttpOnly: true, Secure: secure, SameSite: sameSite, MaxAge: -1, Expires: time.Unix(0, 0),
	})
	http.SetCookie(w, &http.Cookie{
		Name: RefreshCookie, Value: "", Path: "/api/v1/auth", HttpOnly: true, Secure: secure, SameSite: sameSite, MaxAge: -1, Expires: time.Unix(0, 0),
	})
}

func RefreshTokenFromRequest(r *http.Request) string {
	if c, err := r.Cookie(RefreshCookie); err == nil && c.Value != "" {
		return c.Value
	}
	return ""
}
