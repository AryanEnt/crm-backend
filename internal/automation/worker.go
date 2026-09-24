package automation

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Worker claims domain events and jobs, evaluates WHEN/IF, executes THEN, logs runs.
// Designed so the same loop can later move to a dedicated process handling
// schedules, notifications, WhatsApp/Twilio, and large workloads.
type Worker struct {
	repo     *Repository
	executor *Executor
	emitter  *Emitter
	id       string
	wake     chan struct{}
	stop     chan struct{}
	wg       sync.WaitGroup
}

func NewWorker(repo *Repository, executor *Executor, emitter *Emitter) *Worker {
	w := &Worker{
		repo: repo, executor: executor, emitter: emitter,
		id: "api-worker-" + uuid.NewString()[:8],
		wake: make(chan struct{}, 1),
		stop: make(chan struct{}),
	}
	if emitter != nil {
		emitter.SetNotify(w.Wake)
	}
	return w
}

func (w *Worker) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *Worker) Start(ctx context.Context) {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		ticker := time.NewTicker(5 * time.Second)
		scanTicker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		defer scanTicker.Stop()
		w.tick(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-w.stop:
				return
			case <-w.wake:
				w.tick(ctx)
			case <-ticker.C:
				w.tick(ctx)
			case <-scanTicker.C:
				w.scanSynthetic(ctx)
				w.tick(ctx)
			}
		}
	}()
}

func (w *Worker) Stop() {
	close(w.stop)
	w.wg.Wait()
}

func (w *Worker) tick(ctx context.Context) {
	w.processEvents(ctx)
	w.processJobs(ctx)
}

func (w *Worker) processEvents(ctx context.Context) {
	events, err := w.repo.ClaimPendingEvents(ctx, w.id, 25)
	if err != nil {
		slog.Warn("automation claim events failed", "error", err)
		return
	}
	for _, ev := range events {
		if suppress, _ := ev.Payload[PayloadSuppressAutomation].(bool); suppress {
			_ = w.repo.MarkEventProcessed(ctx, ev.ID)
			continue
		}
		if depth, ok := toInt(ev.Payload[PayloadAutomationDepth]); ok && depth > 0 {
			_ = w.repo.MarkEventProcessed(ctx, ev.ID)
			continue
		}
		rules, err := w.repo.ListActiveByTrigger(ctx, ev.EventType)
		if err != nil {
			_ = w.repo.MarkEventFailed(ctx, ev.ID, err.Error())
			continue
		}
		eventID := ev.ID
		for i := range rules {
			rule := rules[i]
			if _, err := w.repo.EnqueueJob(ctx, &eventID, &rule, ev.ResourceType, ev.ResourceID, ev.Payload); err != nil {
				slog.Warn("automation enqueue failed", "automation", rule.ID, "error", err)
			}
		}
		_ = w.repo.MarkEventProcessed(ctx, ev.ID)
	}
}

func (w *Worker) processJobs(ctx context.Context) {
	jobs, err := w.repo.ClaimJobs(ctx, w.id, 25)
	if err != nil {
		slog.Warn("automation claim jobs failed", "error", err)
		return
	}
	for _, job := range jobs {
		if err := w.runJob(ctx, job); err != nil {
			_ = w.repo.FailJob(ctx, job.ID, err.Error(), true, job.MaxAttempts, job.Attempts)
			continue
		}
		_ = w.repo.CompleteJob(ctx, job.ID)
	}
}

func (w *Worker) runJob(ctx context.Context, job Job) error {
	if job.AutomationID == nil {
		return fmt.Errorf("job missing automation")
	}
	auto, err := w.repo.GetAutomation(ctx, *job.AutomationID)
	if err != nil || auto == nil {
		return fmt.Errorf("automation not found")
	}
	if !auto.IsActive {
		run := skippedRun(auto, &job, "automation inactive")
		_, _ = w.repo.InsertRun(ctx, run)
		return nil
	}
	resourceID := ""
	if job.ResourceID != nil {
		resourceID = *job.ResourceID
	}
	entityCtx := map[string]any{}
	if resourceID != "" {
		entityCtx, err = w.repo.LoadEntityContext(ctx, job.ResourceType, resourceID)
		if err != nil {
			run := failedRun(auto, &job, nil, err.Error())
			_, _ = w.repo.InsertRun(ctx, run)
			return err
		}
	}
	// Merge event payload facts (without overwriting entity facts).
	for k, v := range job.Payload {
		if _, exists := entityCtx[k]; !exists {
			entityCtx[k] = v
		}
	}

	matched, detail := MatchConditions(auto.Conditions, entityCtx)
	if !matched {
		run := &Run{
			AutomationID: &auto.ID, AutomationName: auto.Name, JobID: &job.ID, EventID: job.EventID,
			TriggerType: auto.TriggerType, ResourceType: job.ResourceType, ResourceID: job.ResourceID,
			Conditions: auto.Conditions, ConditionsMatched: false, Actions: auto.Actions,
			ActionResults: []ActionResult{}, Status: "skipped", ErrorMessage: detail,
		}
		_, _ = w.repo.InsertRun(ctx, run)
		return nil
	}

	results, execErr := w.executor.Execute(ctx, auto, entityCtx, job.ResourceType, resourceID)
	status := "succeeded"
	errMsg := ""
	if execErr != nil {
		status = "failed"
		errMsg = execErr.Error()
	}
	for _, r := range results {
		if r.Status == "failed" {
			status = "failed"
			if errMsg == "" {
				errMsg = r.Error
			}
		}
	}
	run := &Run{
		AutomationID: &auto.ID, AutomationName: auto.Name, JobID: &job.ID, EventID: job.EventID,
		TriggerType: auto.TriggerType, ResourceType: job.ResourceType, ResourceID: job.ResourceID,
		Conditions: auto.Conditions, ConditionsMatched: true, Actions: auto.Actions,
		ActionResults: results, Status: status, ErrorMessage: errMsg,
	}
	if _, err := w.repo.InsertRun(ctx, run); err != nil {
		slog.Warn("automation run log failed", "error", err)
	}
	if status == "failed" {
		return fmt.Errorf("%s", errMsg)
	}
	return nil
}

func (w *Worker) scanSynthetic(ctx context.Context) {
	if w.emitter == nil {
		return
	}
	overdue, err := w.repo.FindOverdueActivities(ctx, 50)
	if err != nil {
		slog.Warn("overdue scan failed", "error", err)
	} else {
		for _, id := range overdue {
			_ = w.emitter.Emit(ctx, TriggerActivityOverdue, "activity", id, map[string]any{
				"scannedAt": time.Now().UTC().Format(time.RFC3339),
			})
		}
	}
	inactive, err := w.repo.FindInactiveDeals(ctx, 14, 50)
	if err != nil {
		slog.Warn("inactive deal scan failed", "error", err)
	} else {
		for _, id := range inactive {
			_ = w.emitter.Emit(ctx, TriggerDealInactive, "deal", id, map[string]any{
				"inactivityDays": 14,
				"scannedAt":      time.Now().UTC().Format(time.RFC3339),
			})
		}
	}
}

func skippedRun(auto *Automation, job *Job, reason string) *Run {
	return &Run{
		AutomationID: &auto.ID, AutomationName: auto.Name, JobID: &job.ID, EventID: job.EventID,
		TriggerType: auto.TriggerType, ResourceType: job.ResourceType, ResourceID: job.ResourceID,
		Conditions: auto.Conditions, Actions: auto.Actions, ActionResults: []ActionResult{},
		Status: "skipped", ErrorMessage: reason,
	}
}

func failedRun(auto *Automation, job *Job, results []ActionResult, errMsg string) *Run {
	if results == nil {
		results = []ActionResult{}
	}
	return &Run{
		AutomationID: &auto.ID, AutomationName: auto.Name, JobID: &job.ID, EventID: job.EventID,
		TriggerType: auto.TriggerType, ResourceType: job.ResourceType, ResourceID: job.ResourceID,
		Conditions: auto.Conditions, ConditionsMatched: true, Actions: auto.Actions,
		ActionResults: results, Status: "failed", ErrorMessage: errMsg,
	}
}

func toInt(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case int:
		return t, true
	case int64:
		return int(t), true
	default:
		return 0, false
	}
}
