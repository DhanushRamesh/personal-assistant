package inmemory_test

import (
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/remind"
	"github.com/DhanushRamesh/personal-assistant/internal/remind/inmemory"
	"github.com/DhanushRamesh/personal-assistant/internal/remind/storetest"
)

// The in-memory store stands in for the real one, so it has to behave as
// the real one does. Running the same cases over both is what stops a
// test passing here and the server failing in use.
func TestItBehavesLikeAStore(t *testing.T) {
	storetest.Run(t, func(*testing.T) (remind.Store, string) {
		return inmemory.New(), "usr_tester"
	})
}
