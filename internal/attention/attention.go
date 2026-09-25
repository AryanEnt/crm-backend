package attention

import "time"

const (
	NoNext           = "no_next_activity"
	OverdueNext      = "overdue_next"
	OverSLA          = "over_sla"
	AttentionNeeded  = "attention_needed"
	NoRecentActivity = "no_recent_activity"
)

const inactiveWindow = 7 * 24 * time.Hour

// Input is the read-time snapshot used to classify a record.
// Closed records (won, lost, archived, converted, unqualified) return no code.
// Anchor is when the current stage started; CreatedAt is the fallback for "no recent activity".
type Input struct {
	Closed    bool
	Next      *time.Time
	Last      *time.Time
	Anchor    time.Time
	CreatedAt time.Time
	SLAHours  *int
	Now       time.Time
}

// Compute classifies attention. It is not stored. Precedence is closed, missing next,
// overdue next, stage SLA, then seven days without activity.
func Compute(in Input) string {
	if in.Closed {
		return ""
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if in.Next == nil {
		return NoNext
	}
	if !in.Next.After(now) {
		return OverdueNext
	}
	if in.SLAHours != nil && *in.SLAHours > 0 && !in.Anchor.IsZero() {
		hours := now.Sub(in.Anchor).Hours()
		limit := float64(*in.SLAHours)
		if hours > limit {
			return OverSLA
		}
		if hours > limit*0.8 {
			return AttentionNeeded
		}
	}
	ref := in.Last
	if ref == nil {
		t := in.CreatedAt
		ref = &t
	}
	if !ref.IsZero() && now.Sub(*ref) > inactiveWindow {
		return NoRecentActivity
	}
	return ""
}

func ValidCode(code string) bool {
	switch code {
	case "", NoNext, OverdueNext, OverSLA, AttentionNeeded, NoRecentActivity:
		return true
	default:
		return false
	}
}

// SQL expressions. Callers must use the aliases documented on each constant
// and join pipeline_stages as ps when the expression mentions it.
const (
	LeadSQL = `CASE
		WHEN l.status IN ('converted','archived','unqualified') OR l.is_archived THEN ''
		WHEN l.next_activity_at IS NULL THEN 'no_next_activity'
		WHEN l.next_activity_at <= NOW() THEN 'overdue_next'
		WHEN ps.sla_hours IS NOT NULL AND ps.sla_hours > 0 AND EXTRACT(EPOCH FROM (NOW() - l.updated_at)) / 3600 > ps.sla_hours THEN 'over_sla'
		WHEN ps.sla_hours IS NOT NULL AND ps.sla_hours > 0 AND EXTRACT(EPOCH FROM (NOW() - l.updated_at)) / 3600 > ps.sla_hours * 0.8 THEN 'attention_needed'
		WHEN COALESCE(l.last_activity_at, l.created_at) < NOW() - INTERVAL '7 days' THEN 'no_recent_activity'
		ELSE ''
	END`

	CustomerSQL = `CASE
		WHEN c.is_archived THEN ''
		WHEN c.next_follow_up_at IS NULL THEN 'no_next_activity'
		WHEN c.next_follow_up_at <= NOW() THEN 'overdue_next'
		WHEN COALESCE(c.last_contacted_at, c.created_at) < NOW() - INTERVAL '7 days' THEN 'no_recent_activity'
		ELSE ''
	END`

	DealSQL = `CASE
		WHEN d.status <> 'open' THEN ''
		WHEN d.next_activity_at IS NULL THEN 'no_next_activity'
		WHEN d.next_activity_at <= NOW() THEN 'overdue_next'
		WHEN ps.sla_hours IS NOT NULL AND ps.sla_hours > 0 AND EXTRACT(EPOCH FROM (NOW() - d.stage_entered_at)) / 3600 > ps.sla_hours THEN 'over_sla'
		WHEN ps.sla_hours IS NOT NULL AND ps.sla_hours > 0 AND EXTRACT(EPOCH FROM (NOW() - d.stage_entered_at)) / 3600 > ps.sla_hours * 0.8 THEN 'attention_needed'
		WHEN COALESCE(d.last_activity_at, d.created_at) < NOW() - INTERVAL '7 days' THEN 'no_recent_activity'
		ELSE ''
	END`
)
