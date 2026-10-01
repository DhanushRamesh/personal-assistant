package runner

import (
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	utterance "github.com/DhanushRamesh/personal-assistant/internal/speech"
)

func TestACutOffVoiceChatIsToldSo(t *testing.T) {
	cut := utterance.New()
	cut.Note("Radish is 35. Recharge is", 15.0)
	r := &Runner{cut: cut}

	said := r.heard(&chat.Chat{Channel: chat.ChannelVoice, Prompt: "Radish is 35. Recharge is"})

	if !strings.Contains(said, "15 seconds") {
		t.Fatalf("the prompt does not say how long the recording ran:\n%s", said)
	}
	if !strings.Contains(said, "go on") {
		t.Fatalf("the prompt does not ask them to continue:\n%s", said)
	}
}

func TestACompleteVoiceChatIsNotToldItWasCut(t *testing.T) {
	cut := utterance.New()
	cut.Note("something else entirely", 15.0)
	r := &Runner{cut: cut}

	said := r.heard(&chat.Chat{Channel: chat.ChannelVoice, Prompt: "What is the time?"})

	if strings.Contains(said, "stopped after") {
		t.Fatalf("a complete question was reported as cut off:\n%s", said)
	}
	if !strings.Contains(said, "speech turned into text") {
		t.Fatal("a voice chat lost its usual speech guidance")
	}
}

// Typed input is never cut off by a recording limit, and must not be told it
// might have been.
func TestDirectChatsSayNothingAboutSpeech(t *testing.T) {
	cut := utterance.New()
	cut.Note("Radish is 35.", 15.0)
	r := &Runner{cut: cut}

	if said := r.heard(&chat.Chat{Channel: chat.ChannelDirect, Prompt: "Radish is 35."}); said != "" {
		t.Fatalf("a typed chat was given speech guidance: %s", said)
	}
}

// A server reached only by typing has no bridge reporting to it.
func TestNoStoreIsNotACrash(t *testing.T) {
	r := &Runner{}
	if said := r.heard(&chat.Chat{Channel: chat.ChannelVoice, Prompt: "anything"}); said == "" {
		t.Fatal("voice guidance disappeared when no cut store was configured")
	}
}
