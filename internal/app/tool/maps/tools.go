package maps

import (
	"context"
	"fmt"
	"strings"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/world/place"
)

// findPlace : Where a named place is.
func findPlace(m Finding) tool.Tool {
	return tool.Tool{
		Name:    "place_find",
		Domain:  "map",
		Lists:   true,
		Purpose: "Find where a named place or an address is, and get its address back in full.",
		UseWhen: "They name somewhere and what is wanted is where it is: an address, which " +
			"of several it is, whether it is the one they mean. " +
			"Also before asking how long something takes, when the thing is named rather " +
			"than one of their own places.",
		Avoid: "Not for anywhere of their own -- home, the office, somewhere they have been. " +
			"Those are their places and place_visits knows them. " +
			"Not for what a place is like, what is on there, or when it opens: that is the web, " +
			"and this returns an address and nothing else.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"place"},
			Properties: map[string]tool.Property{
				"place": {
					Type: "string",
					Description: "The name or address, as they said it. Add the town when " +
						"they left it out and you know which they mean.",
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "where is Phoenix Marketcity", Args: `{"place":"Phoenix Marketcity Chennai"}`},
			{Ask: "what's the address of the airport", Args: `{"place":"Chennai airport"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Place string `json:"place"`
			}
			read(in.Args, &args)
			if m == nil {
				return tool.Failed("Nothing on this server can look a place up on a map.")
			}
			if trimmed(args.Place) == "" {
				return tool.Failed("Finding a place needs its name.")
			}

			found, err := m.Find(ctx, args.Place, Country, 4)
			if err != nil {
				return tool.Failed(tool.Plainly(err))
			}
			if len(found) == 0 {
				return tool.OK(fmt.Sprintf("No place on the map answers to %q. "+
					"That is the map, not the world: say it could not be found rather "+
					"than that it does not exist.", args.Place))
			}

			var b strings.Builder
			fmt.Fprintf(&b, "%d places answer to %q, best first.\n", len(found), args.Place)
			for _, s := range found {
				fmt.Fprintf(&b, "- %s", s.Name)
				if s.Name == "" {
					fmt.Fprintf(&b, "(an address with nothing named on it)")
				}
				fmt.Fprintf(&b, ": %s  [at %.5f,%.5f]\n", s.Address, s.Lat, s.Lon)
			}
			b.WriteString("The coordinates are for you to pass to place_distance. " +
				"Never read one aloud.")
			return tool.OK(b.String())
		},
	}
}

// nearMe : What is around them.
func nearMe(m Nearing, reader Reader, clock Clock) tool.Tool {
	return tool.Tool{
		Name:   "place_near",
		Domain: "map",
		Lists:  true,
		Purpose: "What there is around where they are now -- somewhere to eat, a chemist, a " +
			"cash machine, fuel.",
		UseWhen: "They ask what is near them, round here, close by, or where the nearest " +
			"something is. Always about where they are now; it has no other way of knowing.",
		Avoid: "Not for anywhere they have been, which is place_visits. " +
			"Not for what a particular place is like or when it opens -- that is the web. " +
			"And not when they named a town rather than meaning here: that is place_find.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"sort": {
					Type: "string",
					Enum: sorts(),
					Description: "What sort of place. Leave it out for anything at all, " +
						"which is right when they just asked what is around.",
				},
				"metres": {
					Type: "integer",
					Description: "How far to look. A kilometre by default, which is what " +
						"round here means; widen only when they say so.",
					Default: Around,
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "anywhere to eat near me", Args: `{"sort":"food"}`},
			{Ask: "where's the nearest chemist", Args: `{"sort":"pharmacy","metres":3000}`},
			{Ask: "what's around here", Args: `{}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Sort   string `json:"sort"`
				Metres int    `json:"metres"`
			}
			read(in.Args, &args)
			if m == nil {
				return tool.Failed("Nothing on this server can look at a map.")
			}

			lat, lon, err := here(ctx, reader, clock, in.Caller.UserID)
			if err != nil {
				return tool.Failed(tool.Plainly(err))
			}

			metres := float64(args.Metres)
			if metres <= 0 {
				metres = Around
			}
			if metres > Furthest {
				metres = Furthest
			}

			found, err := m.Near(ctx, lat, lon, args.Sort, metres, 8)
			if err != nil {
				return tool.Broke("Nothing nearby could be looked up.", err)
			}
			if len(found) == 0 {
				return tool.OK(fmt.Sprintf("Nothing of that sort within %s of them. "+
					"The map may simply not have it: say nothing was found nearby rather "+
					"than that there is nothing.", spoken(metres)))
			}

			var b strings.Builder
			fmt.Fprintf(&b, "%d places within %s, nearest first.\n", len(found), spoken(metres))
			for _, s := range found {
				fmt.Fprintf(&b, "- %s, %s away", s.Name, spoken(s.Metres))
				if s.Address != "" {
					fmt.Fprintf(&b, " -- %s", s.Address)
				}
				b.WriteString("\n")
			}
			return tool.OK(strings.TrimRight(b.String(), "\n"))
		},
	}
}

// howLong : How far and how long to somewhere.
func howLong(m Maps, reader Reader, clock Clock) tool.Tool {
	return tool.Tool{
		Name:   "place_distance",
		Domain: "map",
		Purpose: "How far somewhere is and how long it takes to get there, by road and in " +
			"today's traffic.",
		UseWhen: "How far, how long, how much time to allow, whether they can still make " +
			"something. " +
			"From where they are now unless they say otherwise, which is what makes it worth " +
			"asking at all -- the distance between two cities is a thing anybody knows, and " +
			"how long it takes from here at this moment is not. " +
			"Also whenever something in their diary has a place and the question is whether " +
			"they need to leave.",
		Avoid: "Not for a distance they are asking about in the abstract -- between two cities, " +
			"or as a fact about the world -- which you can answer and which does not change. " +
			"Not for walking directions step by step: this gives how far and how long, " +
			"nothing more, and a turn-by-turn route read aloud is useless.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"to"},
			Properties: map[string]tool.Property{
				"to": {
					Type: "string",
					Description: "Where they are going, named or as an address. Add the town " +
						"when they left it out.",
				},
				"from": {
					Type: "string",
					Description: "Where from, when it is not where they are now. Leave it out " +
						"for the usual case, which is from here.",
				},
				"walking": {
					Type:        "boolean",
					Description: "On foot rather than driving. Driving unless they say.",
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "how far is the airport", Args: `{"to":"Chennai airport"}`},
			{Ask: "how long will it take me to get to the office", Args: `{"to":"office"}`},
			{Ask: "can I walk to the supermarket", Args: `{"to":"Muthu Supermarket","walking":true}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				To      string `json:"to"`
				From    string `json:"from"`
				Walking bool   `json:"walking"`
			}
			read(in.Args, &args)
			if m == nil {
				return tool.Failed("Nothing on this server can work out a journey.")
			}
			if trimmed(args.To) == "" {
				return tool.Failed("A journey needs somewhere to go.")
			}

			to, err := one(ctx, m, args.To)
			if err != nil {
				return tool.Failed(tool.Plainly(err))
			}

			var fromLat, fromLon float64
			whence := "where they are now"
			if trimmed(args.From) == "" {
				fromLat, fromLon, err = here(ctx, reader, clock, in.Caller.UserID)
			} else {
				var start place.Spot
				start, err = one(ctx, m, args.From)
				fromLat, fromLon, whence = start.Lat, start.Lon, start.Name
			}
			if err != nil {
				return tool.Failed(tool.Plainly(err))
			}

			mode := place.Driving
			if args.Walking {
				mode = place.Walking
			}
			went, err := m.Route(ctx, fromLat, fromLon, to.Lat, to.Lon, mode)
			if err != nil {
				return tool.Broke("The way there could not be worked out.", err)
			}
			if went.Metres == 0 && went.Takes == 0 {
				return tool.OK(fmt.Sprintf("No road from %s to %s was found. "+
					"Say so rather than guessing a time.", whence, to.Name))
			}

			how := "driving"
			if mode == place.Walking {
				how = "on foot"
			}
			return tool.OK(fmt.Sprintf(
				"%s to %s: %s, about %s %s.\nThe time allows for today's traffic, so it is "+
					"what it would take now and not a best case. %s is at %s.",
				upper(whence), to.Name, spoken(went.Metres), lasting(went.Takes), how,
				to.Name, to.Address))
		},
	}
}

// one : The best match for a named place.
//
// Two failures and only one of them is worth repeating. Nothing on
// the map by that name is the person's answer; the map service being
// unreachable is not, so it is wrapped rather than handed on, and
// what came back from it is kept behind the sentence.
func one(ctx context.Context, f Finding, text string) (place.Spot, error) {
	found, err := f.Find(ctx, text, Country, 1)
	if err != nil {
		return place.Spot{}, fmt.Errorf("the map could not be reached: %w", err)
	}
	if len(found) == 0 {
		return place.Spot{}, fmt.Errorf("no place on the map answers to %q", text)
	}
	if found[0].Name == "" {
		found[0].Name = text
	}
	return found[0], nil
}

// sorts : The sorts of place that may be asked for, as the schema
// offers them. From the map itself, so the two cannot disagree.
func sorts() []string {
	return strings.Split(place.Sorts(), ", ")
}

// upper : A sentence's first letter.
func upper(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
