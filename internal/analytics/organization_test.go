package analytics

import (
	"strings"
	"testing"
	"time"
)

func TestOrgFiltersApplyEveryDimension(t *testing.T) {
	f := Filter{
		From:        time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC),
		To:          time.Date(2026, 9, 29, 23, 59, 59, 0, time.UTC),
		PipelineID:  "p1",
		TeamID:      "t1",
		OwnerUserID: "u1",
	}

	t.Run("records", func(t *testing.T) {
		b := &sqlBuilder{}
		b.inRange("l.created_at", f)
		b.recordDims("l", f)
		want := "l.created_at >= $1 AND l.created_at <= $2 AND l.pipeline_id::text = $3 AND l.team_id::text = $4 AND l.owner_user_id::text = $5"
		if got := b.sql(); got != want {
			t.Fatalf("sql = %q\nwant %q", got, want)
		}
		if len(b.args) != 5 || b.args[2] != "p1" || b.args[3] != "t1" || b.args[4] != "u1" {
			t.Fatalf("args = %v", b.args)
		}
	})

	t.Run("activities number every placeholder", func(t *testing.T) {
		b := &sqlBuilder{}
		b.inRange("a.created_at", f)
		b.activityDims(f)
		sql := b.sql()
		if strings.Contains(sql, "?") {
			t.Fatalf("unbound placeholder in %q", sql)
		}
		// 2 range + 1 owner + 3 team + 3 pipeline.
		if len(b.args) != 9 {
			t.Fatalf("args = %d, want 9", len(b.args))
		}
		for _, p := range []string{"$3", "$4", "$6", "$7", "$9"} {
			if !strings.Contains(sql, p) {
				t.Fatalf("missing %s in %q", p, sql)
			}
		}
	})

	t.Run("users", func(t *testing.T) {
		b := &sqlBuilder{}
		b.add("u.is_active = ?", true)
		b.userDims(f)
		sql := b.sql()
		if !strings.Contains(sql, "tm.team_id::text = $2") || !strings.Contains(sql, "u.id::text = $3") {
			t.Fatalf("sql = %q", sql)
		}
	})

	t.Run("referrals", func(t *testing.T) {
		b := &sqlBuilder{}
		b.referralDims(f)
		want := "COALESCE(c.pipeline_id, l.pipeline_id)::text = $1 AND COALESCE(c.team_id, l.team_id)::text = $2 AND COALESCE(c.owner_user_id, l.owner_user_id)::text = $3"
		if got := b.sql(); got != want {
			t.Fatalf("sql = %q\nwant %q", got, want)
		}
	})

	t.Run("no filters leaves records unrestricted", func(t *testing.T) {
		b := &sqlBuilder{}
		b.recordDims("d", Filter{})
		b.activityDims(Filter{})
		b.userDims(Filter{})
		if b.sql() != "TRUE" || len(b.args) != 0 {
			t.Fatalf("sql = %q args = %v", b.sql(), b.args)
		}
	})
}
