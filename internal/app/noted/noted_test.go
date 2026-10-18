package noted_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"personal-assistant/internal/app/noted"
	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/base/thinking/prompt"
	"personal-assistant/internal/base/world/event"
	"personal-assistant/internal/base/world/notice"
)

// model : A model that answers with what answer says, and remembers what
// it was asked and how it was labelled.
type model struct {
	mu     sync.Mutex
	answer string
	asked  []environment.Request
	labels []environment.Label
}

func (m *model) Name() string { return "test" }

func (m *model) Run(ctx context.Context, req environment.Request) (<-chan environment.Message, error) {
	m.mu.Lock()
	m.asked = append(m.asked, req)
	l, _ := environment.LabelOf(ctx)
	m.labels = append(m.labels, l)
	m.mu.Unlock()
	ch := make(chan environment.Message, 1)
	ch <- environment.Final(m.answer)
	close(ch)
	return ch, nil
}

type published struct {
	mu  sync.Mutex
	out []string
}

func (p *published) Publish(_ context.Context, from, userID string, kind conversation.Kind, text string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.out = append(p.out, from+"|"+userID+"|"+string(kind)+"|"+text)
	return nil
}

type notes struct {
	mu   sync.Mutex
	kept []notice.Noted
}

func (n *notes) Note(_ context.Context, x notice.Noted) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.kept = append(n.kept, x)
	return nil
}

func line(space, text string) *event.Event {
	payload, _ := json.Marshal(map[string]string{"space": space, "line": text})
	return &event.Event{Kind: event.NoteAdded, Payload: payload, OccurredAt: time.Date(2026, 10, 18, 7, 0, 0, 0, time.UTC)}
}

func offerer(answer string) (*noted.Offerer, *model, *published, *notes) {
	m := &model{answer: answer}
	p := &published{}
	n := &notes{}
	return &noted.Offerer{
		Answerer: m,
		Model:    "fast",
		Publish:  p,
		Notes:    n,
		Location: time.FixedZone("IST", 5*3600+1800),
		Now:      func() time.Time { return time.Date(2026, 10, 18, 7, 0, 0, 0, time.UTC) },
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, m, p, n
}

// A line that is an event is offered, as a question, to the laptop that
// wrote it, and written down as said.
func TestAnEventIsOffered(t *testing.T) {
	o, m, p, n := offerer(`You noted "call Ravi Friday 4pm" in Home. Shall I put it in your calendar for Friday at four?`)
	o.Noted(context.Background(), "u1", "laptop", []*event.Event{line("Home", "call Ravi Friday 4pm")})
	o.Wait()

	if len(p.out) != 1 || !strings.HasPrefix(p.out[0], "laptop|u1|immediate|You noted") {
		t.Fatalf("published %q", p.out)
	}
	if len(n.kept) != 1 || n.kept[0].Outcome != notice.Said || n.kept[0].Reason != notice.Note {
		t.Errorf("written down %+v", n.kept)
	}
	if m.labels[0].Feature != environment.FeatureNote || m.asked[0].Model != "fast" || len(m.asked[0].Tools) != 0 {
		t.Errorf("asked %+v labelled %+v", m.asked[0], m.labels[0])
	}
}

// A line the model leaves is said to nobody, and written down as declined.
func TestNothingIsSaidOfBacklog(t *testing.T) {
	o, _, p, n := offerer(prompt.Silence)
	o.Noted(context.Background(), "u1", "laptop", []*event.Event{line("Home", "calls and messages")})
	o.Wait()

	if len(p.out) != 0 {
		t.Errorf("published %q", p.out)
	}
	if len(n.kept) != 1 || n.kept[0].Outcome != notice.Declined {
		t.Errorf("written down %+v", n.kept)
	}
}

// Other news, and an empty line, ask the model nothing.
func TestOnlyNotesAreDecidedOn(t *testing.T) {
	o, m, _, _ := offerer("anything")
	o.Noted(context.Background(), "u1", "laptop", []*event.Event{
		{Kind: event.TerminalAsks, Payload: json.RawMessage(`{"terminal":"t1"}`)},
		line("Home", "  "),
	})
	o.Wait()
	if len(m.asked) != 0 {
		t.Errorf("asked %d times", len(m.asked))
	}
}

// The model is told the time in their zone, the space and the line, and
// that backlog for the space's project is left alone.
func TestAskingSaysWhatToLeave(t *testing.T) {
	got := noted.Asking(event.Note{Space: "Home", Line: "call Ravi Friday 4pm"},
		time.Date(2026, 10, 18, 7, 0, 0, 0, time.UTC), time.FixedZone("IST", 5*3600+1800))
	for _, want := range []string{"12:30 pm on Sunday 18 October 2026", `"Home"`, `"call Ravi Friday 4pm"`, "the project they are building", prompt.Silence, "Never say it is done"} {
		if !strings.Contains(got, want) {
			t.Errorf("the ask has no %q:\n%s", want, got)
		}
	}
}
