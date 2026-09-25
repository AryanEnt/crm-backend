package attention

import "testing"
import "time"

func TestComputePrecedence(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	old := now.Add(-8 * 24 * time.Hour)
	sla := 10

	cases := []struct {
		name string
		in   Input
		want string
	}{
		{name: "closed", in: Input{Closed: true, Now: now}, want: ""},
		{name: "no next", in: Input{CreatedAt: now, Now: now}, want: NoNext},
		{name: "overdue next beats sla", in: Input{Next: &past, Anchor: old, SLAHours: &sla, CreatedAt: now, Now: now}, want: OverdueNext},
		{name: "over sla", in: Input{Next: &future, Anchor: old, SLAHours: &sla, CreatedAt: now, Now: now}, want: OverSLA},
		{name: "quiet week", in: Input{Next: &future, Last: &old, CreatedAt: old, Anchor: now, Now: now}, want: NoRecentActivity},
		{name: "healthy", in: Input{Next: &future, Last: &now, CreatedAt: now, Anchor: now, Now: now}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Compute(tc.in); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
