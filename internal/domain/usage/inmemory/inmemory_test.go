package inmemory_test

import (
	"testing"

	"personal-assistant/internal/domain/usage"
	"personal-assistant/internal/domain/usage/inmemory"
	"personal-assistant/internal/domain/usage/storetest"
)

// The same cases MySQL runs, since the API's tests stand on this one.
func TestItBehavesLikeAStore(t *testing.T) {
	storetest.Run(t, func(t *testing.T) (usage.Store, string, string) {
		return inmemory.New(), "usr_A", "usr_B"
	})
}
