package places

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"personal-assistant/internal/base/world/event"
)

// Amender : Where stays are read from and rewritten.
type Amender interface {
	Reader
	Amend(ctx context.Context, userID string, events []*event.Event) (int64, error)
}

// RenameLimit : How many of the latest stays are looked at when a place
// is kept or changed -- years of them at a few a day.
const RenameLimit = 5000

// Rename : Gives a place's name to every stay already written whose
// position lies inside it, and says how many changed.
//
// Owner, 16 October 2026, of a stop the server had named for the
// nearest restaurant: *"I should be able to edit it"* -- by keeping it
// as one of their places, which names it, the stays there before as
// well as those to come. A stay elsewhere, or already called that, is
// left as it is.
func Rename(ctx context.Context, events Amender, userID string, p Place) (int, error) {
	stays, err := events.Recent(ctx, userID, event.Query{Kind: event.Stayed, Limit: RenameLimit})
	if err != nil {
		return 0, fmt.Errorf("places: reading the stays to rename: %w", err)
	}
	var again []*event.Event
	for i := range stays {
		e := stays[i]
		var body map[string]any
		if err := json.Unmarshal(e.Payload, &body); err != nil {
			continue
		}
		at, _ := body["at"].(string)
		lat, lng, ok := event.LatLon(at)
		if !ok || Metres(lat, lng, p.Lat, p.Lng) > float64(p.Radius) {
			continue
		}
		if name, _ := body["value"].(string); name == p.Name {
			continue
		}
		body["value"] = p.Name
		payload, err := plain(body)
		if err != nil {
			continue
		}
		e.Payload = payload
		again = append(again, &e)
	}
	if len(again) == 0 {
		return 0, nil
	}
	n, err := events.Amend(ctx, userID, again)
	if err != nil {
		return 0, fmt.Errorf("places: renaming the stays in %s: %w", p.Name, err)
	}
	return int(n), nil
}

// plain : JSON without HTML escaping, so "Mum & Dad's" is written as it is
// and still matches the name it is looked for by.
func plain(v any) (json.RawMessage, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return json.RawMessage(bytes.TrimSpace(b.Bytes())), nil
}
