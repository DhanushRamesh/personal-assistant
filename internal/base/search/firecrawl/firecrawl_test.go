package firecrawl

import "testing"

// TestDaysBecomeTheServicesOwnFilter : Rounded up to the next
// bracket it understands rather than down, because a filter tighter
// than asked for hides things that should have been found and one
// looser only lets a little extra through.
func TestDaysBecomeTheServicesOwnFilter(t *testing.T) {
	for _, c := range []struct {
		days int
		want string
	}{{0, ""}, {-3, ""}, {1, "qdr:d"}, {2, "qdr:w"}, {7, "qdr:w"},
		{8, "qdr:m"}, {31, "qdr:m"}, {90, "qdr:y"}} {
		if got := within(c.days); got != c.want {
			t.Errorf("%d days gave %q, want %q", c.days, got, c.want)
		}
	}
}
