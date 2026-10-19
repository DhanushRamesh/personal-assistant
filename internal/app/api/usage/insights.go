package usage

import (
	"sort"
	"time"

	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/domain/usage"
)

// Insights : What the usage page draws, worked out from every call in its
// window (owner, 18 October 2026: *"in usage I need graphs .. I should be
// seeing all insights like a dashboard"*).
//
// Worked out here, from the calls, rather than in SQL: a month is a few
// thousand rows, and the arithmetic -- buckets in their zone, the spread
// of a time -- reads plainly in Go and is tested without a database.
type Insights struct {
	// Bucket : "hour" for today, "day" for 7 and 30 days.
	Bucket string `json:"bucket"`
	// Buckets : The window, oldest first, every hour or day of it, empty
	// ones too, so the chart has its gaps.
	Buckets []Bucket `json:"buckets"`
	// Ends : How the window's calls ended, by ending.
	Ends map[string]int `json:"ends"`
	// ChatTime : How long chat answers took.
	ChatTime Spread `json:"chat_time"`
	// Took : Each feature's mean time a call, in milliseconds.
	Took map[string]int64 `json:"took"`
	// Before : The window as long, just before this one -- yesterday for
	// today -- for comparing.
	Before Totals `json:"before"`
}

// Bucket : One hour or day: the tokens in, chat and in the background, and
// how many calls.
type Bucket struct {
	At              time.Time `json:"at"`
	ChatInput       int       `json:"chat_input"`
	BackgroundInput int       `json:"background_input"`
	Calls           int       `json:"calls"`
}

// Spread : How long something took: the middle, nine in ten, the longest,
// and of how many.
type Spread struct {
	Calls int   `json:"calls"`
	P50MS int64 `json:"p50_ms"`
	P90MS int64 `json:"p90_ms"`
	MaxMS int64 `json:"max_ms"`
}

// insightsOf : The insights of calls in the window from `from`, `days` long,
// bucketed in loc -- by hour for one day, by day for more.
func insightsOf(calls []usage.Call, from time.Time, days int, loc *time.Location) Insights {
	out := Insights{Bucket: "day", Ends: map[string]int{}, Took: map[string]int64{}}
	from = from.In(loc)
	if days == 1 {
		out.Bucket = "hour"
		for h := 0; h < 24; h++ {
			out.Buckets = append(out.Buckets, Bucket{At: from.Add(time.Duration(h) * time.Hour)})
		}
	} else {
		start := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc)
		for d := 0; d <= days; d++ {
			out.Buckets = append(out.Buckets, Bucket{At: start.AddDate(0, 0, d)})
		}
	}

	var chatTimes []int64
	took := map[string][2]int64{}
	for _, c := range calls {
		at := c.At.In(loc)
		var i int
		if out.Bucket == "hour" {
			i = int(at.Sub(from) / time.Hour)
		} else {
			day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, loc)
			i = int(day.Sub(out.Buckets[0].At).Hours()/24 + 0.5)
		}
		if i >= 0 && i < len(out.Buckets) {
			b := &out.Buckets[i]
			b.Calls++
			if usage.IsChat(c.Feature) {
				b.ChatInput += c.Input
			} else {
				b.BackgroundInput += c.Input
			}
		}
		out.Ends[string(c.Ended)]++
		ms := c.Took.Milliseconds()
		t := took[string(c.Feature)]
		took[string(c.Feature)] = [2]int64{t[0] + ms, t[1] + 1}
		if c.Feature == environment.FeatureChat && c.Ended != environment.EndedUnused {
			chatTimes = append(chatTimes, ms)
		}
	}
	for f, t := range took {
		out.Took[f] = t[0] / t[1]
	}
	out.ChatTime = spread(chatTimes)
	return out
}

// spread : The middle, the ninetieth percentile and the longest of times.
func spread(ms []int64) Spread {
	if len(ms) == 0 {
		return Spread{}
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i] < ms[j] })
	at := func(p float64) int64 { return ms[int(p*float64(len(ms)-1)+0.5)] }
	return Spread{Calls: len(ms), P50MS: at(0.5), P90MS: at(0.9), MaxMS: ms[len(ms)-1]}
}
