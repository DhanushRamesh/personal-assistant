package nearest

import "testing"

// TestWhatIsAlwaysOffered : the two, and why there are only two.
//
// Every name here is a description sent on every turn of every
// conversation, so the list is expensive and short on purpose.
func TestWhatIsAlwaysOffered(t *testing.T) {
	for _, name := range []string{"web_search", "memory_remember"} {
		if !kept(name) {
			t.Errorf("%s is not offered whatever the ranking says", name)
		}
	}
	// Not everything useful. The ranking is for the rest.
	for _, name := range []string{"event_recent", "calendar_events", "task_add", "mail_inbox"} {
		if kept(name) {
			t.Errorf("%s is described on every turn, which the ranking exists to avoid", name)
		}
	}
	if len(Always) != 2 {
		t.Errorf("%d tools are always offered: every one is paid for on every turn", len(Always))
	}
}
