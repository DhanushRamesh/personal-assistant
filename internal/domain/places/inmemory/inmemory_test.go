package inmemory_test

import (
	"testing"

	"personal-assistant/internal/domain/places"
	"personal-assistant/internal/domain/places/inmemory"
	"personal-assistant/internal/domain/places/storetest"
)

// The in-memory store stands in for MySQL in the API's tests, so it has
// to refuse what MySQL refuses.
func TestItBehavesLikeAStore(t *testing.T) {
	storetest.Run(t, func(*testing.T) (places.Store, string, string) {
		return inmemory.New(), "usr_tester", "usr_other"
	})
}
