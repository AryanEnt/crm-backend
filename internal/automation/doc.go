// Package automation owns the CRM WHEN / IF / THEN automation engine.
//
// Flow:
//
//	Domain services → Emitter → automation_events
//	Worker → claim events → enqueue automation_jobs → evaluate IF → execute THEN
//	→ automation_runs (audit) → retry failed jobs
//
// Execution never runs inside UI components. The in-process Worker can later be
// replaced by dedicated workers for schedules, notifications, WhatsApp/Twilio,
// external APIs, and large workloads.
package automation
