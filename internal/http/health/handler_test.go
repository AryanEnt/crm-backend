package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/crm/backend/pkg/response"
)

type fakePinger struct {
	err error
}

func (f fakePinger) Ping(ctx context.Context) error {
	return f.err
}

func TestHandlerReturnsOK(t *testing.T) {
	t.Parallel()

	checker := &Checker{
		version: "test",
		started: time.Now().UTC(),
		ping:    fakePinger{},
	}
	h := NewHandler(checker)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rr := httptest.NewRecorder()
	h.Get(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var env response.Envelope
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !env.Success {
		t.Fatal("expected success=true")
	}
}

func TestHandlerDegradedWhenDBDown(t *testing.T) {
	t.Parallel()

	checker := &Checker{
		version: "test",
		started: time.Now().UTC(),
		ping:    fakePinger{err: context.DeadlineExceeded},
	}
	h := NewHandler(checker)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rr := httptest.NewRecorder()
	h.Get(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
}
