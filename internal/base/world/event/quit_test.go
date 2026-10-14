package event

import (
	"testing"
	"time"
)

func when(s string) time.Time {
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		panic(err)
	}
	return t
}

// The whole table as it stood on 14 October 2026, the day seven dead
// kinds were found and removed: every kind, its count, and the first
// and last report of it, exactly as the database gave them.
//
// Real numbers because the question is a separation, and whether the
// line falls in a gap is only answerable from the real spread. The
// seven come from the dump taken before they were deleted.
var (
	theLiving = []Kind{
		{"battery.level", 2253, when("2026-10-04 05:41:29"), when("2026-10-14 10:50:12")},
		{"location.fix", 2010, when("2026-10-07 09:40:01"), when("2026-10-14 10:50:01")},
		{"screen.unlocked", 631, when("2026-10-04 09:18:54"), when("2026-10-14 10:48:44")},
		{"screen.locked", 595, when("2026-10-04 10:29:25"), when("2026-10-14 10:51:14")},
		{"music.state", 293, when("2026-10-03 10:33:33"), when("2026-10-14 10:52:55")},
		{"wifi.joined", 259, when("2026-10-01 10:27:11"), when("2026-10-14 08:19:57")},
		{"wifi.left", 225, when("2026-10-01 10:33:15"), when("2026-10-14 07:01:48")},
		{"bluetooth.connected", 65, when("2026-10-04 12:16:46"), when("2026-10-14 10:32:32")},
		{"place.stayed", 63, when("2026-10-02 06:28:39"), when("2026-10-14 08:20:06")},
		{"call.received", 45, when("2026-10-02 18:24:30"), when("2026-10-14 04:10:51")},
		{"bluetooth.disconnected", 42, when("2026-10-04 12:37:08"), when("2026-10-14 10:32:38")},
		{"power.plugged", 40, when("2026-10-04 09:27:58"), when("2026-10-14 06:01:33")},
		{"power.unplugged", 39, when("2026-10-04 09:40:25"), when("2026-10-14 06:47:50")},
		{"call.made", 36, when("2026-10-02 18:21:50"), when("2026-10-14 05:21:24")},
		{"place.exited", 26, when("2026-10-02 04:52:21"), when("2026-10-14 07:07:51")},
		{"power.charging", 24, when("2026-10-08 17:00:16"), when("2026-10-14 06:01:33")},
		{"power.discharging", 20, when("2026-10-08 17:40:55"), when("2026-10-14 06:47:49")},
		{"call.missed", 19, when("2026-10-02 17:38:40"), when("2026-10-13 11:12:25")},
		{"place.entered", 16, when("2026-10-01 13:44:40"), when("2026-10-14 08:15:01")},
		{"power.not_charging", 7, when("2026-10-08 17:40:49"), when("2026-10-13 18:31:17")},
		{"battery.full", 5, when("2026-10-04 06:58:23"), when("2026-10-13 20:47:01")},
		{"call.unanswered", 3, when("2026-10-04 11:09:38"), when("2026-10-13 05:49:09")},
		{"power.full", 3, when("2026-10-11 00:22:37"), when("2026-10-13 20:56:16")},
	}
	theDead = []Kind{
		{"screen.changed", 42, when("2026-10-03 21:11:05"), when("2026-10-04 08:30:52")},
		{"music.playing", 9, when("2026-10-03 09:52:55"), when("2026-10-03 10:25:08")},
		{"bluetooth.changed", 9, when("2026-10-03 11:42:41"), when("2026-10-03 16:07:33")},
		{"music.paused", 7, when("2026-10-03 10:07:57"), when("2026-10-03 10:25:36")},
		{"music.resumed", 5, when("2026-10-03 10:17:24"), when("2026-10-03 10:25:36")},
		{"music.started", 4, when("2026-10-03 08:40:26"), when("2026-10-03 10:07:35")},
	}
	// The moment the census was taken.
	census = when("2026-10-14 10:55:00")
)

// TestQuitSeparatesTheDeadFromTheMerelyQuiet : the line has to fall in
// the gap, and the gap is only visible in real data. Every kind in the
// table, judged at once.
func TestQuitSeparatesTheDeadFromTheMerelyQuiet(t *testing.T) {
	all := append(append([]Kind{}, theLiving...), theDead...)
	got := Quit(all, census)

	found := map[string]Stopped{}
	for _, s := range got {
		found[s.Kind] = s
	}
	for _, k := range theDead {
		s, ok := found[k.Kind]
		if !ok {
			t.Errorf("%s stopped eleven days ago and was not noticed", k.Kind)
			continue
		}
		t.Logf("dead   %-22s silent %6s, usual %7s, %8.0fx",
			s.Kind, Roughly(s.Silent), Roughly(s.Usual), s.Times())
	}
	for _, k := range theLiving {
		if s, ok := found[k.Kind]; ok {
			t.Errorf("%s is still reporting and was called dead (%.2fx its usual %s)",
				s.Kind, s.Times(), Roughly(s.Usual))
		}
	}
	if len(got) != len(theDead) {
		t.Errorf("found %d stopped kinds, want %d", len(got), len(theDead))
	}
}

// How wide the margin actually is, so that narrowing it is a deliberate
// act and not something a later change does by accident.
func TestTheMarginIsWide(t *testing.T) {
	worstLiving := 0.0
	for _, k := range theLiving {
		s := Stopped{Count: k.Count, Last: k.Last, Silent: census.Sub(k.Last),
			Usual: k.Last.Sub(k.First) / time.Duration(k.Count-1)}
		if x := s.Times(); x > worstLiving {
			worstLiving = x
		}
	}
	bestDead := 0.0
	for i, k := range theDead {
		s := Stopped{Count: k.Count, Last: k.Last, Silent: census.Sub(k.Last),
			Usual: k.Last.Sub(k.First) / time.Duration(k.Count-1)}
		if x := s.Times(); i == 0 || x < bestDead {
			bestDead = x
		}
	}
	t.Logf("quietest living %.2fx, liveliest dead %.0fx, line at %dx", worstLiving, bestDead, Retired)
	if worstLiving >= Retired {
		t.Errorf("a living kind reaches %.2fx, at or past the line of %d", worstLiving, Retired)
	}
	if bestDead <= Retired {
		t.Errorf("a dead kind only reaches %.0fx, at or below the line of %d", bestDead, Retired)
	}
}

// A kind with no habit yet is left alone rather than guessed at, and a
// kind quiet for an afternoon is not dead.
func TestTooLittleToJudge(t *testing.T) {
	for _, c := range []struct {
		what string
		k    Kind
	}{
		{"one report", Kind{"a.b", 1, when("2026-10-01 00:00:00"), when("2026-10-01 00:00:00")}},
		{"two reports, one interval", Kind{"a.b", 2, when("2026-10-01 00:00:00"), when("2026-10-01 00:01:00")}},
	} {
		if got := Quit([]Kind{c.k}, census); len(got) != 0 {
			t.Errorf("%s: called dead on no habit", c.what)
		}
	}
	// Chatty and quiet since lunch: thousands of times its usual gap,
	// and still not three days.
	busy := Kind{"a.b", 5000, when("2026-10-14 00:00:00"), when("2026-10-14 06:00:00")}
	if got := Quit([]Kind{busy}, census); len(got) != 0 {
		t.Errorf("a kind quiet for %s was called dead", Roughly(census.Sub(busy.Last)))
	}
}
