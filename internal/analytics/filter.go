package analytics

import (
	"strconv"
	"strings"
	"time"
)

// Filter is the shared analytics query filter.
type Filter struct {
	From         time.Time
	To           time.Time
	PipelineID   string
	TeamID       string
	OwnerUserID  string
	Source       string
	AnzscoID     string
	GroupBy      string // team | user | period | pipeline
	Period       string // day | week | month
	SortBy       string
	SortDir      string
}

func ParseFilter(q map[string]string) (Filter, error) {
	f := Filter{
		PipelineID:  strings.TrimSpace(q["pipelineId"]),
		TeamID:      strings.TrimSpace(q["teamId"]),
		OwnerUserID: strings.TrimSpace(q["ownerUserId"]),
		Source:      strings.TrimSpace(q["source"]),
		AnzscoID:    strings.TrimSpace(q["anzscoId"]),
		GroupBy:     strings.TrimSpace(q["groupBy"]),
		Period:      strings.TrimSpace(q["period"]),
		SortBy:      strings.TrimSpace(q["sortBy"]),
		SortDir:     strings.ToLower(strings.TrimSpace(q["sortDir"])),
	}
	if f.Period == "" {
		f.Period = "month"
	}
	if f.GroupBy == "" {
		f.GroupBy = "user"
	}
	if f.SortDir != "asc" {
		f.SortDir = "desc"
	}

	now := time.Now().UTC()
	fromRaw := strings.TrimSpace(q["from"])
	toRaw := strings.TrimSpace(q["to"])
	if fromRaw == "" && toRaw == "" {
		// Default: last 90 days
		f.To = now
		f.From = now.AddDate(0, 0, -90)
	} else {
		var err error
		if fromRaw != "" {
			f.From, err = parseTime(fromRaw)
			if err != nil {
				return f, err
			}
		} else {
			f.From = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
		}
		if toRaw != "" {
			f.To, err = parseTime(toRaw)
			if err != nil {
				return f, err
			}
			// Inclusive end-of-day if date-only
			if len(toRaw) == 10 {
				f.To = f.To.Add(24*time.Hour - time.Nanosecond)
			}
		} else {
			f.To = now
		}
	}
	return f, nil
}

func parseTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, errInvalidDate
}

var errInvalidDate = errString("invalid date; use YYYY-MM-DD or RFC3339")

type errString string

func (e errString) Error() string { return string(e) }

// SQL helper for dynamic WHERE building.
type sqlBuilder struct {
	args  []any
	where []string
}

func (b *sqlBuilder) add(cond string, v any) {
	b.args = append(b.args, v)
	b.where = append(b.where, strings.Replace(cond, "?", "$"+strconv.Itoa(len(b.args)), 1))
}

func (b *sqlBuilder) sql() string {
	if len(b.where) == 0 {
		return "TRUE"
	}
	return strings.Join(b.where, " AND ")
}

func (b *sqlBuilder) applyCommonDeal(alias string, f Filter, joinCustomer bool) {
	b.add(alias+".created_at >= ?", f.From)
	b.add(alias+".created_at <= ?", f.To)
	if f.PipelineID != "" {
		b.add(alias+".pipeline_id::text = ?", f.PipelineID)
	}
	if f.TeamID != "" {
		b.add(alias+".team_id::text = ?", f.TeamID)
	}
	if f.OwnerUserID != "" {
		b.add(alias+".owner_user_id::text = ?", f.OwnerUserID)
	}
	if f.Source != "" {
		b.add(alias+".source = ?", f.Source)
	}
	if f.AnzscoID != "" && joinCustomer {
		b.add("c.anzsco_id::text = ?", f.AnzscoID)
	}
}

func (b *sqlBuilder) applyCommonLead(alias string, f Filter) {
	b.add(alias+".created_at >= ?", f.From)
	b.add(alias+".created_at <= ?", f.To)
	b.add(alias+".is_archived = ?", false)
	if f.PipelineID != "" {
		b.add(alias+".pipeline_id::text = ?", f.PipelineID)
	}
	if f.TeamID != "" {
		b.add(alias+".team_id::text = ?", f.TeamID)
	}
	if f.OwnerUserID != "" {
		b.add(alias+".owner_user_id::text = ?", f.OwnerUserID)
	}
	if f.Source != "" {
		b.add(alias+".source = ?", f.Source)
	}
	if f.AnzscoID != "" {
		b.add(alias+".anzsco_id::text = ?", f.AnzscoID)
	}
}

func pct(num, den int) float64 {
	if den <= 0 {
		return 0
	}
	return float64(num) / float64(den) * 100
}

func div(num, den float64) float64 {
	if den == 0 {
		return 0
	}
	return num / den
}
