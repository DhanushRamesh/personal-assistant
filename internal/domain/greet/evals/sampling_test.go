//go:build evals

// Package evals asks a real model to greet real arrivals.
//
// Not a fixed set of situations somebody invented. It samples moments
// out of what the phone actually reported over the last four weeks,
// builds exactly what the server would have built at that moment, and
// asks the real model what it would have said.
//
// The point is accuracy rather than coverage. A greeting has no single
// right answer, so nothing here asserts the words; it asserts the
// handful of things that are wrong whatever the words are -- a name
// nobody reported, an inference stated as a fact, an accusation, a
// count read out loud -- and prints the rest for a person to read.
//
// Behind a build tag because it costs real calls to a real service:
//
//	make evals-greeting
package evals

import (
	"cmp"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/base/config"
	"github.com/DhanushRamesh/personal-assistant/internal/base/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/base/environment/platformai"
	"github.com/DhanushRamesh/personal-assistant/internal/base/event"
	eventmysql "github.com/DhanushRamesh/personal-assistant/internal/base/event/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/base/storage"
	"github.com/DhanushRamesh/personal-assistant/internal/domain/greet"
	"github.com/DhanushRamesh/personal-assistant/internal/domain/persona"
)

var (
	samples = flag.Int("samples", 8, "how many arrivals to sample")
	seed    = flag.Uint64("seed", 0, "0 picks one and prints it, so a run can be repeated")
	arrived = flag.String("at", "", "one arrival instead of a sample, as 2006-01-02 15:04 in their own zone")
	spokeAt = flag.String("since", "", "when the assistant last spoke before it, same format")
	rivalAt = flag.String("prompt", "", "judge this instruction file instead of the server's own prompt")
	which   = flag.String("model", "", "ask this model instead of the configured one")
	cases   = flag.Bool("cases", false, "run the judged cases in testdata/cases.json")
	runs    = flag.Int("runs", 2, "how many times to ask about each case")
	replay  = flag.Int("real", 0, "replay the last N greetings that were actually spoken, against the prompt as it stands")
	dry     = flag.String("dry", "", "\"facts\" prints this arrival's half of the prompt, \"all\" the whole of it; neither asks the model")
)

// Ordinary, OrdinaryMost, Most : The same bounds the server uses, so
// what the model sees here is what it would have seen at the door.
const (
	Ordinary     = 28 * 24 * time.Hour
	OrdinaryMost = 3000
	Pace         = 1500 * time.Millisecond
)

// asserted : Turning an inference into a fact. The whole of the rule
// about distances is that a thing which several observations merely
// point at is asked about, never stated.
var asserted = []string{
	"you went", "you were at", "you watched", "you attended",
	"you visited", "you spent", "you had dinner", "you made it to",
}

// accused : Putting an absence to somebody as though they owed an
// account of it. The failure an earlier attempt at the absence rule
// produced, and the reason it was reverted.
var accused = []string{
	"you didn't", "you did not", "you never",
	"you skipped", "you failed", "instead of",
}

// Not "you missed". It reads as an accusation in a list and is the
// plain word for a call that was not answered, which is a fact the
// phone reported rather than a charge laid against anybody.

// contact : Saying somebody tried to reach them. Checked against
// whether anything in the window was a call at all.
//
// The worst thing this can do, in the prompt's own words, because
// they will ring back somebody who never called -- and the check
// that was here only looked for invented names, so it waved through
// "someone tried to reach you while you were out, sir" over a
// window holding four wifi events and nothing else.
var contact = []string{
	"tried to reach", "tried to call", "tried to get", "get through",
	"rang", "called you", "a call", "missed call", "didn't connect",
	"did not connect", "trying to reach",
}

// greeted : A salutation. The server says the opening itself; one
// here is the model greeting them twice.
var greeted = []string{
	"good morning", "good afternoon", "good evening",
	"welcome back", "hello,", "hi,",
}

// counted : A number read out loud. "Never read out a count" -- it is
// speech, not a report.
//
// Narrow on purpose, twice over. A number is not a count when it is
// part of a name -- the first run flagged "How was Meesaya Murukku
// 2, sir?", which is the title of the film and the best greeting in
// the sample -- and a number word is not a count when it is the
// hour, which flagged "just before six this morning".
//
// What is actually forbidden is a tally: four times, twice, two
// calls. Those are what turn a greeting into a report.
//
// Narrow again, twice. A branch for number words followed by a
// plural read "five this morning" as a count, because "this" ends
// in an s. Then the digit branch read "Meesaya Murukku 2 is at ten
// past six" the same way, for the same reason, and scored the
// answer the owner had asked for as wrong four times over. The
// following word has to be four letters or more, and the guard
// below is handed the number alone rather than the whole match.
var counted = regexp.MustCompile(
	`\b(\d+)\s+(?:[a-z]{4,}s|percent)\b|\b(times|twice|thrice)\b`)

func TestGreetingRealArrivals(t *testing.T) {
	cfg, err := config.Load("../../../../config.ini", os.LookupEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	quiet := slog.New(slog.NewJSONHandler(io.Discard, nil))

	db, err := storage.Open(t.Context(), cfg.Database, quiet, storage.Options{})
	if err != nil {
		t.Fatalf("database: %v", err)
	}
	events := eventmysql.New(db)

	var user string
	if err := db.DB.Raw("SELECT id FROM users ORDER BY created_at LIMIT 1").Scan(&user).Error; err != nil || user == "" {
		t.Fatalf("no user to greet: %v", err)
	}
	var profile string
	db.DB.Raw("SELECT body FROM memories WHERE user_id = ? AND subject = ? LIMIT 1",
		user, "What I have noticed about them").Scan(&profile)

	where := cfg.Assistant.Location
	if where == nil {
		where = time.UTC
	}
	diary := loadDiary(t, where)

	if *seed == 0 {
		*seed = rand.Uint64()
	}
	t.Logf("seed %d -- rerun this exact sample with -seed %d", *seed, *seed)
	dice := rand.New(rand.NewPCG(*seed, 0x9E3779B97F4A7C15))

	var env *platformai.Environment
	if *dry == "" {
		env = provider(t, cfg, quiet)
	}
	now := time.Now().In(where)

	if *cases {
		judged(t, env, cfg, events, user, profile, diary, where)
		return
	}

	// Every arrival that really happened, against the prompt as it
	// stands now. What it said at the time is printed beside what it
	// would say today, because the only verdict worth having is one
	// made on a real moment somebody remembers.
	if *replay > 0 {
		type spoken struct {
			At   time.Time
			Said string
		}
		var rows []spoken
		if err := db.DB.Raw(`
			SELECT created_at AS at, content AS said
			FROM messages WHERE role = 'assistant' AND kind = 'presence'
			ORDER BY created_at DESC LIMIT ?`, *replay).Scan(&rows).Error; err != nil {
			t.Fatalf("reading what was said: %v", err)
		}
		for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
			rows[i], rows[j] = rows[j], rows[i]
		}
		for n, r := range rows {
			at := r.At.In(where)
			var prev time.Time
			db.DB.Raw(`SELECT created_at FROM messages
				WHERE role = 'assistant' AND created_at < ?
				ORDER BY created_at DESC LIMIT 1`, r.At).Scan(&prev)
			since := prev.In(where)
			if prev.IsZero() {
				since = at.Add(-greet.Recent)
			}
			told, events := compose(t, events, user, profile, diary, at, since, where)

			fmt.Printf("\n── %d · %s · %d events since %s\n",
				n+1, at.Format("Mon 2 Jan 3:04 pm"), events, since.Format("Mon 3:04 pm"))
			for _, a := range told.Were {
				fmt.Printf("   diary while out: %s at %s\n", a.What, a.When.In(where).Format("3:04 pm"))
			}
			fmt.Printf("   then: %q\n", strings.TrimSpace(r.Said))
			if *dry != "" {
				fmt.Printf("%s\n", lines(told))
				continue
			}
			if n > 0 {
				time.Sleep(3 * time.Second)
			}
			said, took, _ := ask(t, env, cfg, told)
			fmt.Printf("   now:  %q   %v\n", said, took)
			for _, w := range wrong(said, told) {
				fmt.Printf("   ✗ %s\n", w)
			}
		}
		return
	}

	// One named arrival instead of a sample, so a window somebody
	// wants to look at twice can be looked at twice.
	if *arrived != "" {
		at, err := time.ParseInLocation("2006-01-02 15:04", *arrived, where)
		if err != nil {
			t.Fatalf("-at: %v", err)
		}
		since := at.Add(-5 * time.Hour)
		if *spokeAt != "" {
			if since, err = time.ParseInLocation("2006-01-02 15:04", *spokeAt, where); err != nil {
				t.Fatalf("-since: %v", err)
			}
		}
		told, n := compose(t, events, user, profile, diary, at, since, where)
		fmt.Printf("\n══ arrived %s · %d events since %s\n",
			at.Format("Mon 2 Jan 3:04 pm"), n, since.Format("3:04 pm"))
		for _, a := range told.Were {
			fmt.Printf("   diary while out: %s at %s\n", a.What, a.When.In(where).Format("3:04 pm"))
		}
		for _, a := range told.Diary {
			fmt.Printf("   still to come:   %s at %s\n", a.What, a.When.In(where).Format("Mon 3:04 pm"))
		}
		switch *dry {
		case "facts":
			fmt.Printf("\n%s\n", facts(told))
			return
		case "all":
			fmt.Printf("\n%s\n", greet.Prompt(told))
			return
		}
		said, took, _ := ask(t, env, cfg, told)
		fmt.Printf("\n   → %q   %v\n", said, took)
		for _, w := range wrong(said, told) {
			fmt.Printf("   ✗ %s\n", w)
			t.Errorf("%s -- %q", w, said)
		}
		return
	}

	// Only where there is anything to sample. Uniform over four weeks
	// put the first eight arrivals in September, where this phone had
	// never reported anything, and eight empty windows measure
	// nothing -- the first run of this made exactly that mistake.
	var first time.Time
	if err := db.DB.Raw("SELECT MIN(occurred_at) FROM events WHERE user_id = ?", user).
		Scan(&first).Error; err != nil || first.IsZero() {
		t.Fatalf("nothing has ever been reported: %v", err)
	}
	first = first.In(where)
	span := now.Sub(first)
	if span < 12*time.Hour {
		t.Skipf("only %v of history; not enough to sample", span.Round(time.Hour))
	}
	t.Logf("sampling arrivals between %s and now (%v of history)",
		first.Format("Mon 2 Jan 3:04 pm"), span.Round(time.Hour))

	var spoke, silent, flagged int
	for i := 1; i <= *samples; i++ {
		// An arrival somewhere in the history, with between two and
		// ten hours of not having spoken behind it.
		at := first.Add(time.Duration(dice.Int64N(int64(span))))
		since := at.Add(-time.Duration(2+dice.Int64N(9)) * time.Hour)

		told, n := compose(t, events, user, profile, diary, at, since, where)
		if i > 1 {
			time.Sleep(Pace)
		}
		said, took, _ := ask(t, env, cfg, told)

		fmt.Printf("\n── %d · arrived %s · %d events since %s\n",
			i, at.Format("Mon 2 Jan 3:04 pm"), n, since.Format("3:04 pm"))
		for _, a := range told.Were {
			fmt.Printf("   diary while out: %s at %s\n", a.What, a.When.In(where).Format("3:04 pm"))
		}
		if said == "" {
			silent++
			fmt.Printf("   → (nothing)   %v\n", took)
			continue
		}
		spoke++
		fmt.Printf("   → %q   %v\n", said, took)
		if why := wrong(said, told); len(why) > 0 {
			flagged++
			for _, w := range why {
				fmt.Printf("   ✗ %s\n", w)
				t.Errorf("sample %d: %s -- %q", i, w, said)
			}
		}
	}
	t.Logf("%d sampled: %d spoke, %d stayed quiet, %d had something wrong with them",
		*samples, spoke, silent, flagged)
}

// facts : The part of the prompt that is this arrival rather than
// the standing instructions.
//
// Everything from where the instructions stop, which is where the
// first of the fact blocks begins. Printed so that a window can be
// read by a person before anybody asks a model about it.
func facts(told greet.Told) string {
	whole := greet.Prompt(told)
	for _, mark := range []string{
		"This was in their diary for the time they were away",
		"Their devices reported nothing at all for these stretches",
		"This is what you have come to know about them",
		"What their devices reported in between",
	} {
		if i := strings.Index(whole, mark); i >= 0 {
			return whole[i:]
		}
	}
	return "(nothing)"
}

// judged : The cases the owner has ruled on, each asked about more
// than once.
//
// A greeting has no right words, so a case says whether it should
// speak at all and what about. Speaking when it should be quiet is
// the commoner failure by a long way: of the first ten he ruled on,
// six are silences, and nothing measured before this had ever
// produced one.
func judged(t *testing.T, env *platformai.Environment, cfg config.Config,
	events *eventmysql.Store, user, profile string,
	diary []greet.Appointment, where *time.Location) {

	raw, err := os.ReadFile("testdata/cases.json")
	if err != nil {
		t.Fatalf("cases: %v", err)
	}
	var set []struct {
		At, Since, Want, About string
		Must, Mustnt           []string
	}
	if err := json.Unmarshal(raw, &set); err != nil {
		t.Fatalf("cases: %v", err)
	}

	asked, right := 0, 0
	for i, c := range set {
		at, err := time.ParseInLocation("2006-01-02 15:04", c.At, where)
		if err != nil {
			t.Fatalf("case %d: at: %v", i+1, err)
		}
		since, err := time.ParseInLocation("2006-01-02 15:04", c.Since, where)
		if err != nil {
			t.Fatalf("case %d: since: %v", i+1, err)
		}
		told, n := compose(t, events, user, profile, diary, at, since, where)

		fmt.Printf("\n── %d · %s · %d events · wants %s: %s\n",
			i+1, at.Format("Mon 2 Jan 3:04 pm"), n, c.Want, c.About)

		for r := 0; r < *runs; r++ {
			if asked > 0 {
				time.Sleep(4 * time.Second)
			}
			asked++
			said, _, ok := ask(t, env, cfg, told)
			if !ok {
				continue
			}
			var why []string
			switch {
			case c.Want == "silent" && said != "":
				why = append(why, "should have said nothing")
			case c.Want == "speak" && said == "":
				why = append(why, "said nothing, and should have spoken")
			}
			low := strings.ToLower(said)
			for _, w := range c.Must {
				if said != "" && !strings.Contains(low, strings.ToLower(w)) {
					why = append(why, "never mentioned "+w)
				}
			}
			for _, w := range c.Mustnt {
				if strings.Contains(low, strings.ToLower(w)) {
					why = append(why, "raised "+w)
				}
			}
			why = append(why, wrong(said, told)...)
			if len(why) == 0 {
				right++
				fmt.Printf("   ✓ %q\n", said)
				continue
			}
			fmt.Printf("   ✗ %q\n", said)
			for _, w := range why {
				fmt.Printf("     %s\n", w)
			}
		}
	}
	fmt.Printf("\n═══ %d of %d right across %d cases\n", right, asked, len(set))
	if right < asked {
		t.Errorf("%d of %d answers were not what was wanted", asked-right, asked)
	}
}

// rival : A whole prompt built from somebody else's instructions and
// this server's own data.
//
// So that a rewrite can be scored on the judged cases before it is
// anywhere near the server. The instructions come from the file; the
// facts are assembled here in the plainest form, because the point
// is to compare instructions and not two different renderings of a
// window.
func rival(text string, told greet.Told) string {
	var b strings.Builder
	b.WriteString(strings.ReplaceAll(text, "%OPENING%", told.Opening))
	b.WriteString("\n\n## NOW\n\nIt is " + told.Now.Format("Monday 2 January, 3:04 pm") + ".")
	if !told.Since.IsZero() {
		b.WriteString(" You last said something to them at " +
			told.Since.In(told.Now.Location()).Format("Monday 3:04 pm") +
			". Anything before that you have already talked about.")
	}
	if len(told.Were) > 0 {
		b.WriteString("\n\n## IN THEIR DIARY FOR THE HOURS THEY WERE AWAY\n")
		for _, a := range told.Were {
			b.WriteString("\n- " + a.What + ", " + a.When.In(told.Now.Location()).Format("3:04 pm"))
			if a.Where != "" {
				b.WriteString(", at " + a.Where)
			}
		}
	}
	if len(told.Diary) > 0 {
		b.WriteString("\n\n## STILL TO COME\n")
		for _, a := range told.Diary {
			when := a.When.In(told.Now.Location())
			day := "today"
			if when.YearDay() != told.Now.YearDay() {
				day = "tomorrow"
			}
			b.WriteString("\n- " + day + " " + when.Format("3:04 pm") + ": " + a.What)
		}
	}
	if len(told.Reminders) > 0 {
		b.WriteString("\n\n## DUE WHILE THEY WERE OUT, AND NOT YET SAID\n")
		for _, r := range told.Reminders {
			b.WriteString("\n- " + r)
		}
	}
	if strings.TrimSpace(told.Profile) != "" {
		b.WriteString("\n\n## WHAT YOU KNOW ABOUT THEM\n\n" + told.Profile)
	}
	b.WriteString("\n\n## WHAT THEIR DEVICES REPORTED IN BETWEEN\n\n" + lines(told))
	return b.String()
}

// lines : The event lines alone, with the instructions around them
// taken off. What somebody reads to judge a window by.
func lines(told greet.Told) string {
	whole := greet.Prompt(told)
	mark := "being told they were at a restaurant they have never been to is worse than being told nothing."
	if i := strings.LastIndex(whole, mark); i >= 0 {
		return strings.TrimSpace(whole[i+len(mark):])
	}
	if i := strings.LastIndex(whole, "Their devices reported nothing in between."); i >= 0 {
		return "   (nothing reported)"
	}
	return "   (nothing)"
}

// wrong : The things that are wrong whatever the words are.
func wrong(said string, told greet.Told) []string {
	low := strings.ToLower(said)
	var why []string
	// Only where there was something to infer. "You went back in" is
	// a fact when a geofence recorded it and an inference only when
	// it was reached by putting a diary beside a stay, and a word
	// list cannot tell those apart on its own.
	if len(told.Were) > 0 {
		if w, ok := has(low, asserted); ok {
			why = append(why, "stated an inference as a fact ("+w+")")
		}
	}
	if w, ok := has(low, accused); ok {
		why = append(why, "put an absence to them as an accusation ("+w+")")
	}
	if w, ok := has(low, greeted); ok {
		why = append(why, "greeted them a second time ("+w+")")
	}
	if m := counted.FindStringSubmatch(low); m != nil {
		n := m[1]
		if n == "" {
			n = m[2]
		}
		if !named(n, told) {
			why = append(why, "read a count out loud ("+m[0]+")")
		}
	}
	if n := strings.Count(said, "?"); n > 1 {
		why = append(why, "asked more than one question")
	}
	if w, ok := has(low, contact); ok && !anyCall(told) {
		why = append(why, "said somebody tried to reach them ("+w+") with no call in the window")
	}
	for _, name := range invented(said, told) {
		why = append(why, "named somebody nothing reported ("+name+")")
	}
	return why
}

// invented : A capitalised name in the answer that appears nowhere in
// what the model was given about this window.
//
// The standing description is full of names -- a girlfriend, a
// mother, a best friend -- and reaching into it for somebody who was
// not there is the worst thing this can do. Checked against the
// window rather than against a list, so a name that really was
// reported passes.
func invented(said string, told greet.Told) []string {
	var seen strings.Builder
	for _, e := range told.Events {
		seen.WriteString(strings.ToLower(string(e.Payload)))
	}
	for _, a := range append(append([]greet.Appointment{}, told.Were...), told.Diary...) {
		seen.WriteString(strings.ToLower(a.What + " " + a.Where))
	}
	known := seen.String()

	// Mid-sentence only. A capital at the start of a sentence is
	// grammar, not a name, and treating it as one reported "Someone",
	// "Today" and "Looks" as invented people on the first run.
	var out []string
	for _, at := range regexp.MustCompile(`\b[A-Z][a-z]{2,}\b`).FindAllStringIndex(said, -1) {
		before := strings.TrimRight(said[:at[0]], " \"'")
		if before == "" || strings.HasSuffix(before, ".") || strings.HasSuffix(before, "?") ||
			strings.HasSuffix(before, "!") || strings.HasSuffix(before, "—") {
			continue
		}
		w := said[at[0]:at[1]]
		if !strings.Contains(known, strings.ToLower(w)) {
			out = append(out, w)
		}
	}
	return out
}

// anyCall : Whether anything in the window was a call of any kind.
//
// By prefix, not by naming the kinds. A phone that learns to report
// a new sort of call should not need a line here before the check
// understands it.
func anyCall(told greet.Told) bool {
	for _, e := range told.Events {
		if strings.HasPrefix(e.Kind, "call.") {
			return true
		}
	}
	return false
}

// named : Whether a number is part of something they were told
// about, rather than a count the model worked out.
func named(n string, told greet.Told) bool {
	for _, a := range append(append([]greet.Appointment{}, told.Were...), told.Diary...) {
		if strings.Contains(strings.ToLower(a.What), n) {
			return true
		}
	}
	for _, e := range told.Events {
		if strings.Contains(strings.ToLower(string(e.Payload)), n) {
			return true
		}
	}
	return false
}

func has(said string, words []string) (string, bool) {
	for _, w := range words {
		if strings.Contains(said, w) {
			return w, true
		}
	}
	return "", false
}

// compose : What the server would have built at that moment.
func compose(t *testing.T, events *eventmysql.Store, user, profile string,
	diary []greet.Appointment, at, since time.Time, where *time.Location) (greet.Told, int) {
	t.Helper()

	from := at.Add(-Ordinary)
	if since.Before(from) {
		from = since
	}
	all, err := events.Recent(t.Context(), user, event.Query{
		Since: from, Until: at, Limit: OrdinaryMost,
		Omit: []string{event.Fixed},
	})
	if err != nil {
		t.Fatalf("events: %v", err)
	}

	usual := map[string]int{}
	evs := make([]event.Event, 0, 32)
	for _, e := range all {
		usual[greet.Label(e)]++
		if !e.OccurredAt.Before(since) {
			evs = append(evs, e)
		}
	}
	for i, j := 0, len(evs)-1; i < j; i, j = i+1, j-1 {
		evs[i], evs[j] = evs[j], evs[i]
	}
	if len(evs) > greet.Most {
		evs = evs[len(evs)-greet.Most:]
	}

	day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, where)
	told := greet.Told{
		Now: at, Since: since, Events: evs, Usual: usual, Profile: profile,
		Opening: greet.Opening(greet.Salutations[greet.Band(at)][0]),
	}
	for _, a := range diary {
		if a.When.Before(day) || !a.When.Before(day.AddDate(0, 0, 2)) {
			continue
		}
		if !a.AllDay && a.When.Before(at) {
			if !a.When.Before(since) {
				told.Were = append(told.Were, a)
			}
			continue
		}
		told.Diary = append(told.Diary, a)
	}
	return told, len(evs)
}

func loadDiary(t *testing.T, where *time.Location) []greet.Appointment {
	t.Helper()
	raw, err := os.ReadFile("testdata/diary.json")
	if err != nil {
		t.Fatalf("diary: %v", err)
	}
	var rows []struct {
		What   string `json:"what"`
		When   string `json:"when"`
		AllDay bool   `json:"allday"`
		Where  string `json:"where"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("diary: %v", err)
	}
	out := make([]greet.Appointment, 0, len(rows))
	for _, r := range rows {
		layout := time.RFC3339
		if r.AllDay {
			layout = "2006-01-02"
		}
		when, err := time.ParseInLocation(layout, r.When, where)
		if err != nil {
			continue
		}
		out = append(out, greet.Appointment{
			What: r.What, When: when.In(where), AllDay: r.AllDay, Where: r.Where,
		})
	}
	return out
}

func provider(t *testing.T, cfg config.Config, quiet *slog.Logger) *platformai.Environment {
	t.Helper()
	env, err := platformai.New(platformai.Config{
		ClientID: cfg.PlatformAI.ClientID, ClientSecret: cfg.PlatformAI.ClientSecret,
		RefreshToken: cfg.PlatformAI.RefreshToken, PortalID: cfg.PlatformAI.PortalID,
		TokenURL: cfg.PlatformAI.TokenURL, ChatURL: cfg.PlatformAI.ChatURL,
		Scope: cfg.PlatformAI.Scope, RedirectURI: cfg.PlatformAI.RedirectURI,
		Vendor: cfg.PlatformAI.Vendor, Model: cfg.PlatformAI.Model,
		SystemPrompt: persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name),
		Thinking:     cfg.PlatformAI.Thinking, Timeout: cfg.PlatformAI.Timeout,
	}, quiet)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	return env
}

// ask : The model's own words, before the opening is put in front of
// them. Checked raw on purpose: the server strips a salutation the
// model wrote, and a test that looked after the stripping would never
// see it happening.
func ask(t *testing.T, env *platformai.Environment, cfg config.Config, told greet.Told) (string, time.Duration, bool) {
	t.Helper()
	model := cmp.Or(*which, cfg.PlatformAI.FastModel, cfg.PlatformAI.Model)
	ctx, done := context.WithTimeout(t.Context(), greet.Within)
	defer done()

	ask := greet.Prompt(told)
	if *rivalAt != "" {
		text, err := os.ReadFile(*rivalAt)
		if err != nil {
			t.Fatalf("prompt file: %v", err)
		}
		ask = rival(string(text), told)
	}

	start := time.Now()
	stream, err := env.Run(ctx, environment.Request{
		Prompt: ask, Purpose: environment.PurposeGreeting,
		Vendor: cfg.PlatformAI.Vendor, Model: model,
		SystemPrompt: persona.Voice(cfg.Assistant.Persona, cfg.Assistant.Name),
	})
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	var last *environment.Message
	for msg := range stream {
		m := msg
		last = &m
	}
	took := time.Since(start).Round(time.Millisecond)

	// Printed where the answer would be, and loudly. A refusal and a
	// silence are both an empty string to everything downstream, and
	// eleven rate-limited calls in a row were read as the model
	// choosing to say nothing -- a conclusion drawn three times, each
	// time wrong, because the failures went to the test log and the
	// answers went to stdout.
	if last == nil {
		fmt.Printf("   ✗✗ THE PROVIDER SENT NOTHING after %v -- not a silence\n", took)
		t.Errorf("the provider said nothing after %v", took)
		return "", took, false
	}
	if last.Kind == environment.KindError {
		fmt.Printf("   ✗✗ THE PROVIDER REFUSED after %v -- not a silence: %s\n      %s\n",
			took, last.Text, last.Detail)
		t.Errorf("the provider failed: %s (%s)", last.Text, last.Detail)
		return "", took, false
	}
	said := strings.TrimSpace(last.Text)
	if greet.Quiet(said) {
		return "", took, true
	}
	// A stream the deadline cut leaves a fragment, and a fragment
	// with no fault in it scores as a good answer. One case passed
	// on "'t know".
	if said != "" && len(strings.Fields(said)) < 3 {
		fmt.Printf("   ✗✗ CUT OFF after %v -- not an answer: %q\n", took, said)
		return "", took, false
	}
	return said, took, true
}
