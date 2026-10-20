package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	announcementsapi "personal-assistant/internal/app/api/announcements"
	"personal-assistant/internal/app/api/apitest"
	"personal-assistant/internal/base/records/client"
	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/domain/announcement"
	"personal-assistant/internal/domain/outbox"
	"personal-assistant/internal/domain/outbox/inmemory"
)

type noted struct {
	client string
	kind   conversation.Kind
	text   string
}

type heard struct{ got []noted }

func (h *heard) HeardBy(_ context.Context, _ string, c *client.Client, kind conversation.Kind, text string) {
	h.got = append(h.got, noted{c.ID, kind, text})
}

// A client collects what it is owed as it is to be heard, and what it says
// it said is noted for it; what it drops is not.
func TestAClientCollectsAndSaysItsOwn(t *testing.T) {
	announcement.Addressing("sir")
	defer announcement.Addressing("")
	owed := inmemory.New()
	notes := &heard{}
	env := apitest.NewWith(t, apitest.Options{Owed: owed, Heard: notes})
	me := env.Login(t, "laptop")
	at := time.Now().UTC()
	for id, kind := range map[string]conversation.Kind{"o1": conversation.Immediate, "o2": conversation.ReminderAnnouncement} {
		o, err := outbox.New(id, me.User.ID, kind, "The battery is low.", at)
		if err != nil {
			t.Fatal(err)
		}
		if err := owed.Publish(context.Background(), o, []string{me.Client.ID}); err != nil {
			t.Fatal(err)
		}
	}

	rec := env.As(t, me.Token, http.MethodGet, "/v1/announcements")
	var list announcementsapi.PendingResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Announcements) != 2 || list.Announcements[0].Say != "The battery is low." {
		t.Fatalf("collected %+v", list.Announcements)
	}

	if rec := env.AsBody(t, me.Token, http.MethodPost, "/v1/announcements/o1", `{"state":"said"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("marking said: %d %s", rec.Code, rec.Body)
	}
	if rec := env.AsBody(t, me.Token, http.MethodPost, "/v1/announcements/o2", `{"state":"dropped","why":"locked"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("marking dropped: %d %s", rec.Code, rec.Body)
	}
	if len(notes.got) != 1 || notes.got[0].kind != conversation.Immediate || notes.got[0].client != me.Client.ID {
		t.Fatalf("noted %+v", notes.got)
	}
}
