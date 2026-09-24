package health

import (
	"context"
	"net/http"
	"time"

	"github.com/crm/backend/pkg/response"
)

// Pinger is the database health dependency.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Checker reports dependency health.
type Checker struct {
	ping    Pinger
	version string
	started time.Time
}

// NewChecker constructs a health checker.
func NewChecker(ping Pinger, version string) *Checker {
	return &Checker{
		ping:    ping,
		version: version,
		started: time.Now().UTC(),
	}
}

// Status is the health payload.
type Status struct {
	Status    string            `json:"status"`
	Version   string            `json:"version"`
	Uptime    string            `json:"uptime"`
	Checks    map[string]string `json:"checks"`
	Timestamp time.Time         `json:"timestamp"`
}

// Check probes dependencies and returns overall status.
func (c *Checker) Check(ctx context.Context) Status {
	checks := map[string]string{
		"database": "ok",
	}
	overall := "ok"

	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if err := c.ping.Ping(pingCtx); err != nil {
		checks["database"] = "unavailable"
		overall = "degraded"
	}

	return Status{
		Status:    overall,
		Version:   c.version,
		Uptime:    time.Since(c.started).Round(time.Second).String(),
		Checks:    checks,
		Timestamp: time.Now().UTC(),
	}
}

// Handler serves GET /health.
type Handler struct {
	checker *Checker
}

// NewHandler creates a health HTTP handler.
func NewHandler(checker *Checker) *Handler {
	return &Handler{checker: checker}
}

// Get handles health check requests.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	status := h.checker.Check(r.Context())
	code := http.StatusOK
	if status.Status != "ok" {
		code = http.StatusServiceUnavailable
	}
	response.JSON(w, code, response.Envelope{
		Success: status.Status == "ok",
		Data:    status,
	})
}
