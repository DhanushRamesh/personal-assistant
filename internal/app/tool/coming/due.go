package coming

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/thinking/prompt"
	"personal-assistant/internal/domain/coming"
)

// Looking : What the diary needs to work out what is due.
//
// The loop's own dependencies, held here instead. What moved onto
// the engine is when to look; the judging -- how much warning a
// thing needs, and in what words -- is unchanged and still here.
type Looking struct {
	// Diary : The calendar. Required.
	Diary coming.Diary
	// Store : Where the judgements are kept. Required.
	Store coming.Store
	// Ask : How a lead is worked out. Required.
	Ask func(ctx context.Context, userID, ask string) (string, error)
	// Location : The person's zone.
	Location *time.Location
	// Now : The clock.
	Now func() time.Time
	// Logger : Where failures go.
	Logger *slog.Logger
}

// Due : What the diary has to say now, if anything.
func Due(look Looking) tool.Tool {
	return tool.Tool{
		Name:    "coming_due",
		Domain:  "coming",
		Lists:   true,
		Purpose: "Work out whether anything in the diary needs saying now, and give the words for it.",
		UseWhen: "A standing rule asks this on a schedule. The answer is the sentence to say, " +
			"already written.",
		Avoid: "Not for what is in the diary -- that is calendar_events. This answers only " +
			"whether a warning is due at this minute, and says nothing the rest of the time.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params:   tool.Schema{Properties: map[string]tool.Property{}},
		Examples: []tool.Example{
			{Ask: "is anything coming up that I should know about", Args: `{"saying":"looking ahead"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			if look.Diary == nil || look.Store == nil || look.Ask == nil {
				return tool.OK(prompt.Silence)
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}

			// A loop per call, with somewhere to put the words
			// instead of a way to say them. Everything else about
			// the pass -- reading ahead, judging what is new,
			// marking what was said, forgetting what left the
			// diary -- is the loop's own and unchanged.
			var said []string
			loop := &coming.Loop{
				Diary: look.Diary,
				Store: look.Store,
				Ask:   look.Ask,
				Say: func(_ context.Context, _, text string) error {
					said = append(said, strings.TrimSpace(text))
					return nil
				},
				Location: look.Location,
				Now:      look.Now,
				Logger:   look.Logger,
			}
			loop.Once(ctx, in.Caller.UserID)

			if len(said) == 0 {
				return tool.OK(prompt.Silence)
			}
			// Two warnings due at the same minute is rare and is
			// one announcement, not two interruptions.
			return tool.OK(strings.Join(said, " "))
		},
	}
}
