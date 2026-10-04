// Package search : Reaches the web -- looking things up, and
// reading what was found.
//
// Two services rather than one, because the two things this is for
// want opposite trades. Owner: *"Firecrawl if I ask a question in
// conversation. TinyFish for announcements and research where speed
// is not important. In conversation, if Firecrawl doesn't work or
// credit is over, use TinyFish as fallback."*
//
// Measured, that is 510ms at 95.3% against 2.62s at 92.0%. In a turn
// the caller cuts at thirty seconds, two seconds is a quarter of
// what is left after speech and the model; in a background loop
// nobody is waiting and the only thing that matters is that it can
// be done at all. Firecrawl allows five hundred searches a month and
// TinyFish does not count them.
package search

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Result : One thing found.
type Result struct {
	// Title, URL : What it is and where.
	Title string
	URL   string
	// Snippet : The service's own summary, which is usually enough.
	// Reading the page itself is a separate and dearer act.
	Snippet string
	// Site : Who published it, where the service says.
	Site string
	// When : When it was published, as the service wrote it. Often
	// empty, and kept as text because a service that cannot parse
	// its own dates should not have this one guessing.
	When string
}

// Ask : One search, and how to narrow it.
type Ask struct {
	// Query : What to search for.
	Query string
	// Limit : How many results.
	Limit int
	// Days : Only things published in the last this many days. Zero
	// is any time.
	//
	// Not a nicety. Asked what was trending today, the search
	// returned a Pinterest board called "510 Tamil memes ideas in
	// 2026" and a Play Store listing, because an index ranks an
	// evergreen page above a fresh one and nothing had told it the
	// question was about now. Both services offer this and neither
	// was being given it.
	Days int
	// News : Whether to search news rather than the web at large.
	// For something that happened rather than something that is.
	News bool
}

// Page : One page, read.
type Page struct {
	// URL : Where it was asked for, and where it ended up after any
	// redirect. Both, because a search result that redirects to a
	// paywall or a login is worth being able to see.
	URL   string
	Final string
	// Title : What the page calls itself.
	Title string
	// Text : The readable part, with the navigation and the adverts
	// taken out by whichever service read it.
	Text string
	// Trouble : Why this one could not be read, when it could not.
	// One page failing must not lose the others.
	Trouble string
}

// Searcher : Something that can look the web up.
type Searcher interface {
	// Search : What the web says about this, best first.
	Search(ctx context.Context, ask Ask) ([]Result, error)
	// Where : Which service this is, for the log and for a refusal
	// that has to name what failed.
	Where() string
}

// Reader : Something that can read a page.
type Reader interface {
	// Read : What these pages say.
	Read(ctx context.Context, urls []string) ([]Page, error)
	// Where : Which service this is.
	Where() string
}

// Service : Something that can do both, which both of ours can.
type Service interface {
	Searcher
	Reader
}

// ErrSpent : The service will not answer again until its allowance
// renews. Distinct from an ordinary failure because there is no
// point asking it a second time.
var ErrSpent = errors.New("search: the allowance is spent")

// DefaultRested : How long a spent service is left alone.
//
// Its allowance is monthly, so the honest wait is weeks; an hour is
// chosen instead because the alternative to guessing wrong is a
// wasted round trip on every single search until somebody notices,
// and an hour costs one wasted trip an hour at worst.
const DefaultRested = time.Hour

// Pair : The quick one, falling back to the one that never runs out.
//
// Only for the conversation. A background loop asks the patient one
// directly: it has all the time in the world and no reason to spend
// an allowance that something waiting on an answer may need.
type Pair struct {
	// Quick : Asked first. Nil leaves only Spare.
	Quick Service
	// Spare : Asked when the quick one cannot answer. Required.
	Spare Service
	// Within : How long the quick one gets before the spare is asked
	// instead. Zero gives it everything, which is wrong in a turn:
	// a quick service that hangs would spend the whole budget and
	// leave none for the one that would have answered.
	Within time.Duration
	// Rested : How long a spent service is left alone. Zero selects
	// DefaultRested.
	Rested time.Duration
	// Now : The clock, replaceable in tests.
	Now func() time.Time
	// Logger : Where a fallback is recorded. Nil is silent, and a
	// silent fallback is how a dead key goes unnoticed for a month.
	Logger *slog.Logger

	mu    sync.Mutex
	spent time.Time
}

// Where : Which service this is.
func (p *Pair) Where() string {
	if p == nil {
		return "nothing"
	}
	if p.Quick == nil {
		return p.Spare.Where()
	}
	return p.Quick.Where() + " then " + p.Spare.Where()
}

// Search : The quick one if it can, the spare if it cannot.
func (p *Pair) Search(ctx context.Context, ask Ask) ([]Result, error) {
	return fall(p, ctx, func(ctx context.Context, s Service) ([]Result, error) {
		return s.Search(ctx, ask)
	})
}

// Read : The same, for pages.
func (p *Pair) Read(ctx context.Context, urls []string) ([]Page, error) {
	return fall(p, ctx, func(ctx context.Context, s Service) ([]Page, error) {
		return s.Read(ctx, urls)
	})
}

// fall : One thing asked of the quick service, and of the spare
// when the quick one cannot.
//
// Generic so that searching and reading share it. The four things
// it does are each the result of something that went wrong without
// them, and none of them is worth writing twice.
func fall[T any](p *Pair, ctx context.Context, do func(context.Context, Service) (T, error)) (T, error) {
	var none T
	if p == nil || p.Spare == nil {
		return none, errors.New("search: nothing is configured to reach the web")
	}
	if p.Quick == nil || p.resting() {
		return do(ctx, p.Spare)
	}

	quick := ctx
	if p.Within > 0 {
		var stop context.CancelFunc
		quick, stop = context.WithTimeout(ctx, p.Within)
		defer stop()
	}

	got, err := do(quick, p.Quick)
	if err == nil {
		return got, nil
	}
	// The turn itself ending is not the quick one failing, and
	// asking the spare would only fail again more slowly.
	if ctx.Err() != nil {
		return none, err
	}

	if errors.Is(err, ErrSpent) {
		p.rest()
	}
	p.note(ctx, "falling back to the other web service",
		slog.String("quick", p.Quick.Where()),
		slog.String("spare", p.Spare.Where()),
		slog.Bool("spent", errors.Is(err, ErrSpent)),
		slog.Any("error", err))

	return do(ctx, p.Spare)
}

// resting : Whether the quick one is being left alone.
func (p *Pair) resting() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.spent.IsZero() {
		return false
	}
	rested := p.Rested
	if rested <= 0 {
		rested = DefaultRested
	}
	return p.now().Sub(p.spent) < rested
}

// rest : Stops asking the quick one for a while.
func (p *Pair) rest() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.spent = p.now()
}

func (p *Pair) now() time.Time {
	if p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}

func (p *Pair) note(ctx context.Context, msg string, with ...any) {
	if p.Logger == nil {
		return
	}
	p.Logger.WarnContext(ctx, msg, with...)
}

// Describe : What was found, as the model reads it.
func Describe(query string, found []Result) string {
	if len(found) == 0 {
		return fmt.Sprintf("The web was searched for %q and nothing came back. "+
			"That is what the web says, not what is true: say you found nothing "+
			"rather than that the thing does not exist.", query)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d results for %q, best first. These are summaries written by "+
		"whoever published them, not facts you have checked. Say where something "+
		"came from when it matters, and do not read a URL aloud.\n", len(found), query)
	for i, r := range found {
		fmt.Fprintf(&b, "\n%d. %s\n", i+1, strings.TrimSpace(r.Title))
		if s := strings.TrimSpace(r.Snippet); s != "" {
			b.WriteString("   " + s + "\n")
		}
		where := strings.TrimSpace(r.Site)
		if where == "" {
			where = strings.TrimSpace(r.URL)
		}
		if when := strings.TrimSpace(r.When); when != "" {
			where += ", " + when
		}
		b.WriteString("   -- " + where + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// Said : What a page says, as the model reads it.
//
// Truncated, and loudly. A page can be a hundred thousand words and
// a turn has thirty seconds; handing the whole of one over spends
// the budget on text nobody asked for and buries the part that
// answered the question. Where it was cut is said, so the model
// knows it is reading part of something rather than all of it.
func Said(pages []Page, most int) string {
	if len(pages) == 0 {
		return "Nothing could be read."
	}
	if most <= 0 {
		most = 6000
	}

	var b strings.Builder
	for i, p := range pages {
		if i > 0 {
			b.WriteString("\n")
		}
		where := strings.TrimSpace(p.Final)
		if where == "" {
			where = strings.TrimSpace(p.URL)
		}
		if trouble := strings.TrimSpace(p.Trouble); trouble != "" {
			fmt.Fprintf(&b, "%s could not be read: %s\n", where, trouble)
			continue
		}

		fmt.Fprintf(&b, "%s -- %s\n", strings.TrimSpace(p.Title), where)
		text := strings.TrimSpace(p.Text)
		if text == "" {
			b.WriteString("(the page had no readable text)\n")
			continue
		}
		if len([]rune(text)) > most {
			text = string([]rune(text)[:most]) +
				"\n[cut here: the page goes on, and this is the first part of it]"
		}
		b.WriteString(text + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
