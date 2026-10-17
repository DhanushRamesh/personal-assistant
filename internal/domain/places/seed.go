package places

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"personal-assistant/internal/base/world/event"
)

// seedStays : How many stays under a name are read to find its middle.
const seedStays = 500

// Seed : Makes Home and Office from the stays already written there,
// the first time somebody's places are asked for, and never again.
//
// Each sits at the median of the stays named "home" or "office",
// whatever their case, with SeedRadius. The median rather than the
// mean, so one stay named home by a crossing that came late -- the
// middle of it in the road outside -- does not drag the centre out.
// A name with no stays is skipped, and the user is marked seeded
// either way, so deleting Home later does not bring it back.
//
// Returns what was made.
func Seed(ctx context.Context, store Store, events Reader, userID string, now time.Time) ([]Place, error) {
	done, err := store.Seeded(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("places: asking whether they were seeded: %w", err)
	}
	if done {
		return nil, nil
	}

	var made []Place
	for _, name := range []string{Home, Office} {
		stays, err := events.Recent(ctx, userID, event.Query{
			Kind: event.Stayed, Contains: name, Limit: seedStays})
		if err != nil {
			return nil, fmt.Errorf("places: reading the stays at %s: %w", name, err)
		}
		lat, lng, ok := Middle(stays, name)
		if !ok {
			continue
		}
		p := Place{
			ID: NewID(), UserID: userID, Name: name, Lat: lat, Lng: lng,
			Radius: SeedRadius, CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
		}
		if err := store.Add(ctx, p); err != nil {
			if errors.Is(err, ErrTaken) {
				// Drawn already, by hand or by a seed racing this one.
				continue
			}
			return nil, fmt.Errorf("places: making %s: %w", name, err)
		}
		made = append(made, p)
	}
	if err := store.MarkSeeded(ctx, userID, now); err != nil {
		return made, fmt.Errorf("places: marking them seeded: %w", err)
	}
	return made, nil
}

// Middle : The median position of the stays called by a name, if
// there are any.
func Middle(stays []event.Event, name string) (lat, lng float64, ok bool) {
	var lats, lngs []float64
	for _, e := range stays {
		if e.Kind != event.Stayed {
			continue
		}
		var into struct {
			Value string `json:"value"`
			At    string `json:"at"`
		}
		if err := json.Unmarshal(e.Payload, &into); err != nil || !Same(into.Value, name) {
			continue
		}
		la, lo, good := event.LatLon(into.At)
		if !good {
			continue
		}
		lats, lngs = append(lats, la), append(lngs, lo)
	}
	if len(lats) == 0 {
		return 0, 0, false
	}
	return median(lats), median(lngs), true
}

// median : The middle of some numbers.
func median(xs []float64) float64 {
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
