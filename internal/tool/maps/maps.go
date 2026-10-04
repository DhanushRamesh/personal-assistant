// Package maps : Lets the assistant answer about the world's
// geography rather than about where the person has been.
//
// Four questions, which were the owner's own list: how far is the
// airport, what is near me, how long will it take to get there,
// and where is this address. Three endpoints answer them, all on
// the key already used to name stays.
//
// The useful half of all of them is "from here". A distance
// between two cities is a thing anybody knows; how long it takes
// *now*, from where they are standing, is the thing nothing else
// can say.
package maps

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/event"
	"github.com/DhanushRamesh/personal-assistant/internal/place"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

const (
	// Around : How far "near me" reaches, in metres.
	//
	// A kilometre. Walking distance is a few hundred metres and a
	// drive is anywhere; a kilometre is the stretch somebody means
	// by "round here".
	Around = 1000
	// Furthest : The most "near me" may be widened to.
	Furthest = 5000
	// Stale : How old a position may be and still be where they are.
	//
	// The phone reports every five minutes, so anything older than
	// this means it has been off or out of signal, and answering
	// "twenty minutes away" from a position an hour old is worse
	// than saying it cannot tell.
	Stale = 20 * time.Minute
	// Country : Where a named place is looked for first.
	//
	// Without it "Cambridge" is in England and "Springfield" is
	// anywhere. Their own country is the right guess and the
	// description never says where they live.
	Country = "in"
)

// Finding, Nearing, Routing : What this needs of a map.
type Finding interface {
	Find(ctx context.Context, text, country string, most int) ([]place.Spot, error)
}
type Nearing interface {
	Near(ctx context.Context, lat, lon float64, what string, metres float64, most int) ([]place.Spot, error)
}
type Routing interface {
	Route(ctx context.Context, fromLat, fromLon, toLat, toLon float64, mode string) (place.Journey, error)
}

// Maps : A service that can do all three.
type Maps interface {
	Finding
	Nearing
	Routing
}

// Reader : Where the person's own positions come from.
type Reader interface {
	Recent(ctx context.Context, userID string, q event.Query) ([]event.Event, error)
}

// Clock : Now, and their zone.
type Clock struct {
	Now      func() time.Time
	Location *time.Location
}

func (c Clock) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

// Tools : What the assistant can answer about places.
func Tools(m Maps, reader Reader, clock Clock) []tool.Tool {
	return []tool.Tool{findPlace(m), nearMe(m, reader, clock), howLong(m, reader, clock)}
}

// here : Where they are now, from the last position their phone
// reported.
//
// Refused rather than guessed when it is old. A journey time from
// an hour-old position is wrong in the one way that matters: it is
// believed and acted on.
func here(ctx context.Context, reader Reader, clock Clock, userID string) (float64, float64, error) {
	if reader == nil {
		return 0, 0, fmt.Errorf("nothing on this server knows where you are")
	}
	now := clock.now()
	seen, err := reader.Recent(ctx, userID, event.Query{
		Kind: event.Fixed, Since: now.Add(-Stale), Limit: 1,
	})
	if err != nil {
		return 0, 0, err
	}
	if len(seen) == 0 {
		return 0, 0, fmt.Errorf("your phone has not reported where it is for over %d minutes, "+
			"so I cannot work this out from where you are", int(Stale.Minutes()))
	}
	fix, ok := event.ReadFix(seen[0])
	if !ok {
		return 0, 0, fmt.Errorf("the last position your phone reported could not be read")
	}
	return fix.Lat, fix.Lon, nil
}

// spoken : A distance as somebody would say it.
func spoken(metres float64) string {
	if metres < 950 {
		return fmt.Sprintf("%d metres", int((metres+25)/50)*50)
	}
	return fmt.Sprintf("%.1f km", metres/1000)
}

// lasting : A duration as somebody would say it.
func lasting(d time.Duration) string {
	mins := int(d.Round(time.Minute).Minutes())
	switch {
	case mins < 1:
		return "under a minute"
	case mins < 60:
		return fmt.Sprintf("%d minutes", mins)
	case mins%60 == 0:
		return fmt.Sprintf("%d hours", mins/60)
	default:
		return fmt.Sprintf("%d hours %d minutes", mins/60, mins%60)
	}
}

// args : The arguments all three take, read once.
func read(raw json.RawMessage, into any) { _ = json.Unmarshal(raw, into) }

// trimmed : A string argument, tidied.
func trimmed(s string) string { return strings.TrimSpace(s) }

var _ = chat.ChannelVoice
