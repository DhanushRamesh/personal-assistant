package client_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/records/client"
)

// TestAClientMustBelongToSomebody : A client with no user cannot
// authenticate and cannot be revoked by anybody, because there is
// nobody it answers to.
func TestAClientMustBelongToSomebody(t *testing.T) {
	if _, err := client.NewClient("", "a phone", "hash", chat.ChannelVoice); err == nil {
		t.Fatal("a client with no user was created")
	}
}

// TestAClientSaysHowItListens : The channel decides what may be said
// to it -- a satellite with a microphone cannot show what is about to
// happen and wait -- so an unknown one is refused rather than guessed.
func TestAClientSaysHowItListens(t *testing.T) {
	for _, ch := range []chat.Channel{chat.ChannelVoice, chat.ChannelDirect} {
		if _, err := client.NewClient("usr_1", "a phone", "hash", ch); err != nil {
			t.Errorf("channel %q was refused: %v", ch, err)
		}
	}
	for _, bad := range []chat.Channel{"", "sms", "Voice", "voice "} {
		_, err := client.NewClient("usr_1", "a phone", "hash", bad)
		if !errors.Is(err, chat.ErrUnknownChannel) {
			t.Errorf("channel %q was accepted (err = %v)", bad, err)
		}
	}
}

// TestAClientNameIsCountedInRunes : Not bytes. A name of emoji is
// shorter than its length in bytes says, and refusing it would be
// refusing a name that fits.
func TestAClientNameIsCountedInRunes(t *testing.T) {
	// Four bytes each, so a byte count would refuse this at a quarter
	// of the real limit.
	emoji := strings.Repeat("😀", client.MaxClientNameRunes)
	if _, err := client.NewClient("usr_1", emoji, "hash", chat.ChannelVoice); err != nil {
		t.Errorf("a name of %d runes was refused: %v", client.MaxClientNameRunes, err)
	}

	tooLong := strings.Repeat("d", client.MaxClientNameRunes+1)
	if _, err := client.NewClient("usr_1", tooLong, "hash", chat.ChannelVoice); !errors.Is(err, client.ErrClientNameTooLong) {
		t.Errorf("a name past the limit was accepted (err = %v)", err)
	}
}

// TestAClientNameIsTrimmedAndMayBeNothing : The name is for a person
// reading a list of their own devices, so it is optional.
func TestAClientNameIsTrimmedAndMayBeNothing(t *testing.T) {
	made, err := client.NewClient("usr_1", "  the kitchen speaker  ", "hash", chat.ChannelVoice)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if made.Name != "the kitchen speaker" {
		t.Errorf("name = %q, want it trimmed", made.Name)
	}
	if _, err := client.NewClient("usr_1", "", "hash", chat.ChannelVoice); err != nil {
		t.Errorf("a client with no name was refused: %v", err)
	}
}

// TestRevokedIsAboutTheMomentAndNotAFlag : Revoked() reads a
// timestamp, so a client revoked and then written back without one
// would quietly work again.
func TestRevokedIsAboutTheMomentAndNotAFlag(t *testing.T) {
	made, err := client.NewClient("usr_1", "a phone", "hash", chat.ChannelDirect)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if made.Revoked() {
		t.Error("a client was revoked the moment it was made")
	}

	at := time.Now().UTC()
	made.RevokedAt = &at
	if !made.Revoked() {
		t.Error("a client with a revocation time does not read as revoked")
	}
}

// TestAnIdentifierSaysWhatItBelongsTo : And is not mistaken for one
// belonging to something else.
func TestAnIdentifierSaysWhatItBelongsTo(t *testing.T) {
	id := client.NewClientID()
	if !strings.HasPrefix(id, client.ClientIDPrefix) {
		t.Errorf("%q does not begin with %q", id, client.ClientIDPrefix)
	}
	if !client.ValidClientID(id) {
		t.Errorf("%q was minted and then refused", id)
	}
	for _, bad := range []string{"", "cli_", "cli_short", id[4:], "usr_" + id[4:], id + "x"} {
		if client.ValidClientID(bad) {
			t.Errorf("%q was accepted as a client identifier", bad)
		}
	}
}
