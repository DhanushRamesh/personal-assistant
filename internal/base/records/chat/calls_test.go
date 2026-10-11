package chat_test

import (
	"testing"
	"time"

	"personal-assistant/internal/base/records/chat"
)

func TestACallIsStampedOnceAtEachPart(t *testing.T) {
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	c := &chat.Chat{}
	i := c.Asking(at, true)
	c.Heard(i, at.Add(time.Second))
	c.Heard(i, at.Add(2*time.Second))
	c.Ended(i, chat.CallTools, at.Add(3*time.Second))
	c.Ended(i, chat.CallUnused, at.Add(4*time.Second))
	c.Heard(-1, at)
	c.Ended(5, chat.CallFailed, at)

	got := c.Calls[i]
	if !got.Early || !got.HeardAt.Equal(at.Add(time.Second)) || !got.DoneAt.Equal(at.Add(3*time.Second)) || got.Ended != chat.CallTools {
		t.Fatalf("call is %+v; want early, heard at 1s, ended with tools at 3s", got)
	}
}
