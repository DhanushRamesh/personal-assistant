// Package nearest chooses which tools to describe for one question.
//
// A request carries every tool it offers in full, and that block was
// 117,923 bytes of a 196,382-byte request. Describing all of them is
// what deferral was built to stop: twenty-five are named in a
// hand-written list and described always, the rest are names the model
// can ask about.
//
// Measured on 18 October 2026, the hand-written list is the weakest
// part of it. Over the 106 questions in the tool suite, the right tool
// was reached first try 85 times with those twenty-five and 99 times
// with the twenty nearest the question by meaning -- and describing all
// eighty-eight reached 102, so choosing by meaning recovers most of the
// way on forty per cent of the bytes. Neither run ever asked what a
// tool was: when the right one arrives as a name, the model does not
// call tool_describe, it reaches for something else and is wrong.
//
// The block also costs time in itself. The same question took 2.36s to
// its first word with no tools, 2.94s with twenty and 3.66s with fifty,
// so twenty rather than fifty is about 0.71s off every call in a turn.
//
// What a tool is ranked by is not what the model reads. web_search is
// 3,999 characters against an embedder that reads about 512 tokens, so
// its worked examples -- which are the questions it is for -- never
// reach the embedder at all. Ranked on its purpose and those examples
// the right tool is in the nearest twenty on 98% of cases, against 96%
// for the whole description and 79% for the fixed list.
package nearest

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/thinking/embed"
)

// Batch : How many texts the embedding server takes at once.
//
// Thirty-two, which is what it refuses more than: a request for all
// eighty-eight comes back 413 Payload Too Large.
const Batch = 32

// Warming : How long one pass of embedding may take.
//
// Generous because nobody waits for it. The tools are embedded away
// from any turn, and a turn that arrives before they are ready is
// offered everything, which is what happened before this existed.
const Warming = 2 * time.Minute

// Choosing : How long a turn will wait to be told which tools to
// describe.
//
// Short, and a timeout is not a failure: it falls back to the
// hand-written list, which is what the server sent until today. An
// embedding server that has gone away must cost a fraction of a second
// and not a turn.
const Choosing = 900 * time.Millisecond

// Text : What a tool is ranked by -- its name, what it does, and the
// questions it is for.
//
// Not its whole description. The seven longest run past what the
// embedder reads, and what is cut off is the examples, which are the
// most useful part for matching a question to a tool.
func Text(t tool.Tool) string {
	asks := make([]string, 0, len(t.Examples))
	for _, e := range t.Examples {
		asks = append(asks, e.Ask)
	}
	say := strings.TrimSpace(t.Name + ". " + strings.TrimSpace(t.Purpose) + " " + strings.Join(asks, ". "))
	if args := takes(t); args != "" {
		say += " Takes: " + args
	}
	return strings.TrimSpace(say)
}

// takes : What a tool's arguments are called, and what each is for in a
// few words.
//
// The reference implementations rank on the schema as well as the
// description, and measured over the 91 questions in the tool suite
// they are right: with the arguments the right tool is in the nearest
// five on 88% rather than 82%, and in the nearest ten on 95% rather
// than 92%. It is what moves the small numbers, which are the ones
// worth having -- ten tools is where a request gets smaller.
//
// Asked "do I have anything tomorrow", calendar_events came 26th of 88
// without them and 15th with, because a tool that takes 'from', 'to'
// and 'days' looks like a question about tomorrow while a sentence
// about reading calendars does not.
//
// Sorted, because a map's order is not stable and a vector that moves
// between restarts cannot be cached or compared.
func takes(t tool.Tool) string {
	if len(t.Params.Properties) == 0 {
		return ""
	}
	out := make([]string, 0, len(t.Params.Properties))
	for name, p := range t.Params.Properties {
		say := strings.Join(strings.Fields(p.Description), " ")
		if len(say) > Argument {
			say = say[:Argument]
		}
		out = append(out, strings.TrimSpace(name+" "+say))
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// Argument : How much of what an argument is for is ranked on.
//
// A few words. Some run to a paragraph of rules about when to leave
// them out, which is for the model to read and not for this.
const Argument = 60

// Asking : What to search with -- the question, and what was said just
// before it.
//
// Never the question alone. Half of what the person says carries no
// subject of its own: a bare "yes", "the other one", "go on". Measured
// 18 October 2026 on "say Deepika's birthday" / "are you sure, sir?" /
// "yes", against all eighty-eight tools:
//
//	the question itself                calendar_events at #1
//	a bare "yes", alone                calendar_events at #75
//	"yes" with what was just said      calendar_events at #4
//	"yes" with both turns              calendar_events at #2
//
// Alone it finds place_near and call_offer, which is what a vector of
// a word that means nothing looks like. The fix costs nothing: the
// same one call, over a longer string.
//
// Trimmed because the last answer can be a paragraph and the embedder
// reads about 512 tokens: what is wanted from it is the subject, which
// is at the front.
func Asking(say, before string) string {
	before = strings.Join(strings.Fields(before), " ")
	if len(before) > Before {
		before = before[:Before]
	}
	return strings.TrimSpace(before + " " + strings.TrimSpace(say))
}

// Before : How much of the last answer is searched with.
//
// Enough for the subject and not enough to drown the question: an
// answer that ran long would otherwise be most of what is compared,
// and the question is the part that moved.
const Before = 300

// Always : The tools offered whatever the ranking says, beside the
// nearest rather than instead of one of them.
//
// Owner, 18 October 2026, on being told the nearest ten carry the right
// tool on 95% of questions rather than 97%: *"make search tool always
// inside so even if it misses, the job is still done."*
//
// The twentieth question is the one this is for. A ranking that misses
// leaves the model with nothing that fits and no way to ask -- measured
// the same day, it does not call tool_describe when the tool it wants
// is only a name, it reaches for something else and is wrong. With a
// search in the list it can at least go and look.
//
// web_search was the only one, and memory_remember joined it on 21
// October 2026.
//
// web_search is here because it answers a question about anything
// rather than about one thing, so its absence leaves nothing to fall
// back on.
//
// memory_remember is here for the opposite reason: it is never what a
// sentence is about. He said *"54.197.7.79 is my gateway ip in ULaa US
// DC"* and was told "I am afraid I cannot do that, sir." The ranking
// put event_recent first and memory_remember nowhere in the top eight
// of eighty-eight -- an address reads as a place, and the tool's own
// words are about when to decide to keep something rather than about
// the kind of sentence that needs keeping. Asked to remember his
// wife's birthday it loses to place_find as well.
//
// Nothing about that is a wrong answer by the model: it looked at what
// it had, found nothing for keeping a fact, and said so. The tool was
// never on the table.
//
// It is also the one tool that is used without being asked for --
// *"whether or not you were asked to keep it ... do not wait to be
// told"* -- which is an instruction it cannot follow on a turn where
// nobody handed it the tool.
//
// Two and no more. Every name here is a description on every turn of
// every conversation, which is the fixed list this replaced.
var Always = []string{"web_search", "memory_remember"}

// kept : Whether a tool is offered whatever the ranking says.
func kept(name string) bool {
	for _, n := range Always {
		if n == name {
			return true
		}
	}
	return false
}

// Picker : Ranks tools against a question, over vectors it keeps.
//
// Safe to use before it knows anything. A tool it has no vector for is
// never ranked out: it is returned alongside the nearest, so the worst
// a cold or broken Picker can do is offer what the server offered
// before.
type Picker struct {
	// Embed : Where vectors come from. Nil leaves every turn to the
	// hand-written list.
	Embed embed.Embedder
	// Logger : Where a failure is recorded. Nil discards it.
	Logger *slog.Logger

	// Two stores and one engine, which is the owner's decision of 18
	// October 2026. The server's own tools are fixed at a build and
	// embedded once at startup; a device's come and go with the laptop,
	// arrive per chat, and change whenever its own source does. Mixed
	// into one map, a laptop that is away leaves its tools ranked
	// against questions it cannot answer, and nothing can be cleared
	// without clearing the lot.
	mu     sync.RWMutex
	server map[string]embed.Vector
	device map[string]embed.Vector
}

// Where : Which store a tool's vector belongs in.
type Where int

const (
	// Server : The server's own tools, fixed at a build.
	Server Where = iota
	// Device : What the person's laptop or phone offers, which comes
	// and goes.
	Device
)

// store : The map for one side, made if it is not there. Call with the
// write lock held.
func (p *Picker) store(w Where) map[string]embed.Vector {
	if w == Device {
		if p.device == nil {
			p.device = map[string]embed.Vector{}
		}
		return p.device
	}
	if p.server == nil {
		p.server = map[string]embed.Vector{}
	}
	return p.server
}

// vector : What is held for this text, from either store. Call with the
// read lock held.
func (p *Picker) vector(text string) embed.Vector {
	if v := p.server[text]; v != nil {
		return v
	}
	return p.device[text]
}

// Forget : Drops what is held for one side.
//
// The device side only, in practice: the laptop's tools change when its
// own source does, and a vector for a description nobody serves any
// more would rank a tool that cannot be called.
func (p *Picker) Forget(w Where) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if w == Device {
		p.device = nil
		return
	}
	p.server = nil
}

// Held : How many vectors each side holds, for the log.
func (p *Picker) Held() (server, device int) {
	if p == nil {
		return 0, 0
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.server), len(p.device)
}

// Warm : Embeds any of these tools it does not already hold.
//
// Called away from a turn -- at startup for the server's own, and after
// a chat for whatever a device offered that was new. Returns how many
// it added.
func (p *Picker) Warm(ctx context.Context, w Where, tools []tool.Tool) (int, error) {
	if p == nil || p.Embed == nil {
		return 0, nil
	}
	var want []string
	p.mu.RLock()
	for _, t := range tools {
		if text := Text(t); p.vector(text) == nil {
			want = append(want, text)
		}
	}
	p.mu.RUnlock()
	if len(want) == 0 {
		return 0, nil
	}
	// Each text once, however many tools share it.
	want = unique(want)

	ctx, cancel := context.WithTimeout(ctx, Warming)
	defer cancel()
	added := 0
	for i := 0; i < len(want); i += Batch {
		end := min(i+Batch, len(want))
		got, err := p.Embed.Documents(ctx, want[i:end])
		if err != nil {
			return added, fmt.Errorf("nearest: embedding tools: %w", err)
		}
		p.mu.Lock()
		into := p.store(w)
		for k, v := range got {
			into[want[i+k]] = v
			added++
		}
		p.mu.Unlock()
	}
	return added, nil
}

// Keep : The names to describe in full for this question: the n nearest
// of those it can rank, and everything it cannot.
//
// The second half is the safety. A tool with no vector -- one a device
// has just offered, or one embedded while the server was away -- is
// kept rather than hidden, so a question is never answered without a
// tool for a reason nobody can see.
//
// Empty and false where it cannot rank at all, which the caller reads
// as "offer what you would have offered".
func (p *Picker) Keep(ctx context.Context, say string, among []tool.Tool, n int) (map[string]bool, bool) {
	if p == nil || p.Embed == nil || n <= 0 || strings.TrimSpace(say) == "" {
		return nil, false
	}

	p.mu.RLock()
	held := len(p.server) + len(p.device)
	p.mu.RUnlock()
	if held == 0 {
		return nil, false
	}

	ctx, cancel := context.WithTimeout(ctx, Choosing)
	defer cancel()
	q, err := p.Embed.Query(ctx, say)
	if err != nil {
		if p.Logger != nil {
			p.Logger.WarnContext(ctx, "cannot rank the tools, so all the usual ones are offered",
				slog.Any("error", err))
		}
		return nil, false
	}

	type scored struct {
		name string
		sim  float64
	}
	keep := make(map[string]bool, n)
	ranked := make([]scored, 0, len(among))
	p.mu.RLock()
	for _, t := range among {
		v := p.vector(Text(t))
		if v == nil {
			// Not ranked, and therefore not ranked out.
			keep[t.Name] = true
			continue
		}
		ranked = append(ranked, scored{t.Name, embed.Similarity(q, v)})
	}
	p.mu.RUnlock()

	sort.Slice(ranked, func(i, j int) bool { return ranked[i].sim > ranked[j].sim })
	for i := 0; i < n && i < len(ranked); i++ {
		keep[ranked[i].name] = true
	}
	// Beside the nearest rather than instead of one of them: a search
	// that took a place would make the ranking nine deep and not ten.
	for _, t := range among {
		if kept(t.Name) {
			keep[t.Name] = true
		}
	}
	return keep, true
}

// unique : The distinct strings, in the order first seen.
func unique(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
