package remind_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/remind"
	"github.com/DhanushRamesh/personal-assistant/internal/remind/inmemory"
)

// spoken : A reminder said at the given moment.
func spokenAt(title, body string, at time.Time) remind.Reminder {
	fired := at.UTC()
	return remind.Reminder{
		ID: remind.NewID(), UserID: "usr_1", Scope: remind.ScopeUser,
		Title: title, Body: body, Status: remind.Done, LastFiredAt: &fired, Fires: 1,
	}
}

// Nothing said adds nothing to the prompt, rather than an empty heading.
func TestNothingSaidIsNoBlock(t *testing.T) {
	if got := remind.JustSaid(nil, time.Now(), time.UTC); got != "" {
		t.Errorf("JustSaid = %q, want empty", got)
	}
}

// The block says what was said, and that it happened outside the
// conversation. Without the second part the model reads its own reminder
// as something the person said.
func TestItSaysWhatWasSaidAndThatItWasNotATurn(t *testing.T) {
	now := time.Now().UTC()
	got := remind.JustSaid([]remind.Reminder{
		spokenAt("Tablets", "Time to take your tablets.", now.Add(-4*time.Minute)),
	}, now, time.UTC)

	for _, want := range []string{
		"said out loud a short time ago",
		"not a turn in this conversation",
		"4 minutes ago",
		"Time to take your tablets.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the block is missing %q:\n%s", want, got)
		}
	}
}

// "That" has to resolve, or the block is decoration.
func TestItSaysWhatThatMeans(t *testing.T) {
	now := time.Now().UTC()
	got := remind.JustSaid([]remind.Reminder{
		spokenAt("Tablets", "Time to take your tablets.", now.Add(-time.Minute)),
	}, now, time.UTC)

	for _, want := range []string{"mean this one", "what you just said, this is it"} {
		if !strings.Contains(got, want) {
			t.Errorf("the block does not say what \"that\" means:\n%s", got)
		}
	}
}

// Two said together and "that" picks out nothing. Telling the model to
// take the most recent would have it choose by which was created first,
// which is no answer at all, and the tool refuses to choose anyway.
func TestWithTwoItIsToldToAskRatherThanChoose(t *testing.T) {
	now := time.Now().UTC()
	at := now.Add(-time.Minute)
	got := remind.JustSaid([]remind.Reminder{
		spokenAt("Tablets", "Take your tablets.", at),
		spokenAt("Mum", "Call your mother.", at),
	}, now, time.UTC)

	if !strings.Contains(got, "ask which they mean rather than choosing") {
		t.Errorf("the block does not say to ask:\n%s", got)
	}
	if strings.Contains(got, "mean this one") {
		t.Errorf("the block points at one of two:\n%s", got)
	}
}

// And the single case is not made to ask about a choice that does not
// exist. One said is not ambiguous.
func TestWithOneItDoesNotAsk(t *testing.T) {
	now := time.Now().UTC()
	got := remind.JustSaid([]remind.Reminder{
		spokenAt("Tablets", "Take your tablets.", now.Add(-time.Minute)),
	}, now, time.UTC)

	if strings.Contains(got, "ask which") {
		t.Errorf("one reminder was made into a question:\n%s", got)
	}
}

// Being told is not being told twice. Without this the assistant opens
// every turn by repeating a reminder the person heard a minute ago.
func TestItIsToldNotToRaiseIt(t *testing.T) {
	now := time.Now().UTC()
	got := remind.JustSaid([]remind.Reminder{
		spokenAt("Tablets", "Time to take your tablets.", now.Add(-time.Minute)),
	}, now, time.UTC)

	if !strings.Contains(got, "Do not raise any of this yourself") {
		t.Errorf("nothing stops it repeating what was just said:\n%s", got)
	}
}

// This is the opposite of the missed block, which must be volunteered. One
// happened and one did not, and the difference is the whole of it.
func TestItIsNotTheMissedBlock(t *testing.T) {
	now := time.Now().UTC()
	got := remind.JustSaid([]remind.Reminder{
		spokenAt("Tablets", "Time to take your tablets.", now.Add(-time.Minute)),
	}, now, time.UTC)

	if strings.Contains(got, "Begin your reply") {
		t.Errorf("a reminder that was said is being volunteered like one that was not:\n%s", got)
	}
}

// How long ago, in words somebody would use.
func TestHowLongAgoIsSaidInWords(t *testing.T) {
	now := time.Now().UTC()

	for _, c := range []struct {
		since time.Duration
		want  string
	}{
		{10 * time.Second, "just now"},
		{time.Minute, "a minute ago"},
		{4 * time.Minute, "4 minutes ago"},
		{14 * time.Minute, "14 minutes ago"},
	} {
		got := remind.JustSaid([]remind.Reminder{
			spokenAt("Tablets", "Take your tablets.", now.Add(-c.since)),
		}, now, time.UTC)

		if !strings.Contains(got, c.want) {
			t.Errorf("%v ago is not %q:\n%s", c.since, c.want, got)
		}
	}
}

// Every one is listed. Leaving one out is how "that" comes to mean the
// wrong reminder.
func TestEveryOneSaidIsListed(t *testing.T) {
	now := time.Now().UTC()
	got := remind.JustSaid([]remind.Reminder{
		spokenAt("Tablets", "Take your tablets.", now.Add(-time.Minute)),
		spokenAt("Mum", "Call your mother.", now.Add(-time.Minute)),
	}, now, time.UTC)

	for _, want := range []string{"Take your tablets.", "Call your mother."} {
		if !strings.Contains(got, want) {
			t.Errorf("the block is missing %q:\n%s", want, got)
		}
	}
}

// When it was said is in the person's own zone, not the server's.
func TestTheClockTimeIsTheirs(t *testing.T) {
	india := time.FixedZone("IST", 5*3600+1800)
	now := time.Date(2026, 9, 27, 17, 0, 0, 0, time.UTC)

	// Two hours back, which is past where the relative form stops.
	got := remind.JustSaid([]remind.Reminder{
		spokenAt("Tablets", "Take your tablets.", now.Add(-2*time.Hour)),
	}, now, india)

	if !strings.Contains(got, "at 8:30 pm") {
		t.Errorf("said at the server's hour rather than theirs:\n%s", got)
	}
}

// Recently reads the store and hands back a block.
func TestRecentlyReadsWhatWasSaid(t *testing.T) {
	ctx := context.Background()
	store := inmemory.New()
	now := time.Now().UTC()

	r, err := remind.New("usr_1", "", remind.ScopeUser, "Tablets", "Take your tablets.",
		now.Add(-time.Minute), remind.Once)
	if err != nil {
		t.Fatalf("remind.New: %v", err)
	}
	if err := store.Create(ctx, r); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Fired(ctx, r.ID, now.Add(-time.Minute), time.Time{}); err != nil {
		t.Fatalf("Fired: %v", err)
	}

	recently := &remind.Recently{Store: store, Now: func() time.Time { return now }}
	got, err := recently.Block(ctx, "usr_1")
	if err != nil {
		t.Fatalf("Block: %v", err)
	}
	if !strings.Contains(got, "Take your tablets.") {
		t.Errorf("Block = %q, want what was said", got)
	}
}

// Past the window it is not "that" any more.
func TestRecentlyForgets(t *testing.T) {
	ctx := context.Background()
	store := inmemory.New()
	now := time.Now().UTC()
	said := now.Add(-remind.JustSaidWindow - time.Minute)

	r, err := remind.New("usr_1", "", remind.ScopeUser, "Tablets", "Take your tablets.",
		said, remind.Once)
	if err != nil {
		t.Fatalf("remind.New: %v", err)
	}
	if err := store.Create(ctx, r); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Fired(ctx, r.ID, said, time.Time{}); err != nil {
		t.Fatalf("Fired: %v", err)
	}

	recently := &remind.Recently{Store: store, Now: func() time.Time { return now }}
	got, err := recently.Block(ctx, "usr_1")
	if err != nil {
		t.Fatalf("Block: %v", err)
	}
	if got != "" {
		t.Errorf("Block = %q, want nothing that old", got)
	}
}

// No store, no person, no block. Neither is a failure.
func TestRecentlyIsQuietWithNothingBehindIt(t *testing.T) {
	var none *remind.Recently
	if got, err := none.Block(context.Background(), "usr_1"); err != nil || got != "" {
		t.Errorf("Block = %q, %v, want nothing", got, err)
	}

	empty := &remind.Recently{Store: inmemory.New()}
	if got, err := empty.Block(context.Background(), ""); err != nil || got != "" {
		t.Errorf("Block = %q, %v, want nothing", got, err)
	}
}
