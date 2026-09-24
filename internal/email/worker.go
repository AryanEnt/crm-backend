package email

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

type Worker struct {
	service *Service
	stop    chan struct{}
	wg      sync.WaitGroup
}

func NewWorker(service *Service) *Worker {
	return &Worker{service: service, stop: make(chan struct{})}
}

func (w *Worker) Start(ctx context.Context) {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		watchTicker := time.NewTicker(6 * time.Hour)
		schedTicker := time.NewTicker(30 * time.Second)
		defer watchTicker.Stop()
		defer schedTicker.Stop()
		w.service.RenewWatches(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-w.stop:
				return
			case <-watchTicker.C:
				w.service.RenewWatches(ctx)
			case <-schedTicker.C:
				w.service.SendDueScheduled(ctx)
			}
		}
	}()
	slog.Info("gmail email worker started")
}

func (w *Worker) Stop() {
	select {
	case <-w.stop:
	default:
		close(w.stop)
	}
	w.wg.Wait()
}
