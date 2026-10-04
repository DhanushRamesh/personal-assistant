package notice_test

import (
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/notice"
)

// TestTheFirstLookCatchesUpOverTheWait : A battery at 25 per cent,
// marked by the phone to be said at once, arrived at 01:39:14
// between two restarts thirty-six seconds apart. The first look
// came at 01:44:42 and reached back to 01:39:42 -- twenty-six
// seconds too late -- and nothing recorded that anything had been
// missed.
//
// The two windows were the same length, so the look opened exactly
// where the silence ended.
func TestTheFirstLookCatchesUpOverTheWait(t *testing.T) {
	w := &notice.Watcher{}
	if w.Catching() <= notice.Behind {
		t.Errorf("the first look reaches back %s, no further than an ordinary one at %s",
			w.Catching(), notice.Behind)
	}
	// Far enough to cover the hold before it, with the ordinary
	// window on top.
	if w.Catching() < notice.Waited+notice.Behind {
		t.Errorf("the first look reaches back %s, short of the %s it waited plus %s",
			w.Catching(), notice.Waited, notice.Behind)
	}
}
