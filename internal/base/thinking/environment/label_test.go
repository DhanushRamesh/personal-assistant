package environment_test

import (
	"context"
	"testing"

	"personal-assistant/internal/base/thinking/environment"
)

// A tool run inside a chat says only what it is, and keeps whose chat
// it is from the label above. Its own feature brings its own detail:
// the chat round's "started early" is not the rule writer's.
func TestALabelKeepsWhoFromAbove(t *testing.T) {
	chat := environment.Labelled(context.Background(), environment.Label{
		UserID: "usr_A", ChatID: "chat_1", ConversationID: "conv_1",
	})
	early := environment.Labelled(chat, environment.Label{
		Feature: environment.FeatureChat, Detail: environment.DetailEarly, Early: true,
	})
	writer := environment.Labelled(early, environment.Label{Feature: environment.FeatureRuleWriter})

	got, ok := environment.LabelOf(writer)
	if !ok {
		t.Fatal("no label")
	}
	want := environment.Label{UserID: "usr_A", ChatID: "chat_1", ConversationID: "conv_1",
		Feature: environment.FeatureRuleWriter}
	if got != want {
		t.Errorf("label = %+v, want %+v", got, want)
	}

	// Naming somebody without naming a feature keeps the feature.
	again, _ := environment.LabelOf(environment.Labelled(early, environment.Label{UserID: "usr_B"}))
	if again.UserID != "usr_B" || again.Feature != environment.FeatureChat || !again.Early ||
		again.Detail != environment.DetailEarly {
		t.Errorf("label = %+v, want the chat's feature kept", again)
	}

	if _, ok := environment.LabelOf(context.Background()); ok {
		t.Error("a bare context claimed a label")
	}
}
