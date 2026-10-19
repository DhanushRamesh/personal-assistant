//go:build evals

package evals

import (
	"context"
	"strings"
	"testing"

	"personal-assistant/internal/base/thinking/environment"
)

// TestTheDiaryIsOnlyForWhatHasNotHappened : Whether a question about
// something already past opens the calendar.
//
// The fault, 19 October 2026. He asked who called, then "to whom did i
// make?", then "from and to?" -- meaning when the call started and
// ended. He was told "Your diary shows only Maha Ashtami today, sir --
// nothing scheduled", and then the call times. The diary had nothing to
// do with it.
//
// The rule contradicted itself. It said a time "asked about, mentioned
// in passing, or only discussed" is read from the calendar, and three
// lines later that "the test is whether something is being placed at a
// time, not whether a time was said". A two-hour call at 2am was
// discussed and places nothing, and the model followed the first.
//
// Nothing measured it, which is how a contradiction survived in a
// prompt that has been reworded many times. This is that measurement.
// Owner: *"we cant patch on and on, we need a clear definitive prompt
// that covers, and mostly i will talk like 'from and to' only"*.
func TestTheDiaryIsOnlyForWhatHasNotHappened(t *testing.T) {
	env, specs, _ := harness(t)

	for _, c := range []struct {
		// said : What he says, with the line before it where the
		// question has no subject of its own. Which is most of them.
		before string
		said   string
		// diary : Whether the calendar is the right place to look.
		diary bool
	}{
		// Already happened. The record of what his devices reported,
		// never the diary.
		{
			before: "Alekhya Chintada, sir — two hours nineteen minutes, at two this morning.",
			said:   "from and to?",
			diary:  false,
		},
		{said: "when did I get home yesterday", diary: false},
		{said: "how long was I at the office today", diary: false},
		{
			before: "Your mother rang at twenty-five minutes past eleven, sir.",
			said:   "how long?",
			diary:  false,
		},

		// Still to come. The diary, every time, even unasked.
		{said: "do I have anything tomorrow", diary: true},
		{said: "I am meeting a cousin at six this evening", diary: true},
		{
			before: "There is nothing in your diary on Thursday, sir.",
			said:   "and Friday?",
			diary:  true,
		},
	} {
		t.Run(first(c.said, 28), func(t *testing.T) {
			var history []environment.Turn
			if c.before != "" {
				history = []environment.Turn{{Role: environment.RoleAssistant, Text: c.before}}
			}

			stream, err := env.Run(context.Background(), environment.Request{
				Prompt:  c.said,
				History: history,
				Tools:   specs,
			})
			if err != nil {
				t.Fatalf("asking: %v", err)
			}
			var last *environment.Message
			for msg := range stream {
				m := msg
				last = &m
			}
			if last == nil {
				t.Fatal("nothing came back")
			}

			var called []string
			for _, one := range last.ToolCalls {
				called = append(called, one.Name)
			}
			opened := false
			for _, name := range called {
				if strings.HasPrefix(name, "calendar_") || strings.HasPrefix(name, "coming_") {
					opened = true
				}
			}

			switch {
			case c.diary && !opened:
				t.Errorf("%q did not open the diary; it called %v", c.said, called)
			case !c.diary && opened:
				t.Errorf("%q opened the diary for something already past; it called %v", c.said, called)
			default:
				t.Logf("%q called %v", c.said, called)
			}
		})
	}
}

// TestItLooksThingsUpRatherThanSendingHimAway : Whether a question
// about how to do something reaches web_search.
//
// The fault, 19 October 2026: asked how to see the points on his HDFC
// credit card, it answered "I cannot see your credit card account or
// its balance from here, sir. That is something you would need to
// check through the HDFC Bank app or website". Owner: *"jarvis has no
// limit, it should never say it doesn't know, it should do a
// websearch"*.
//
// Measured against the real model, because the prompt is where this
// was asked to live and a prompt nobody measures is how the diary rule
// contradicted itself for weeks.
func TestItLooksThingsUpRatherThanSendingHimAway(t *testing.T) {
	env, specs, _ := harness(t)

	for _, c := range []struct {
		said string
		// search : Whether the web is where this is answered.
		search bool
	}{
		{said: "i have hdfc credit card ..how to see credit pints", search: true},
		{said: "how do I turn on two factor on my zoho account", search: true},
		{said: "what does the realme buds t310 cost now", search: true},

		// Not everything. A thing it can simply answer needs no
		// search, and a thing about him is in his own record.
		{said: "what is nine times seven", search: false},
		{said: "do I have anything tomorrow", search: false},
	} {
		t.Run(first(c.said, 30), func(t *testing.T) {
			stream, err := env.Run(context.Background(), environment.Request{
				Prompt: c.said,
				Tools:  specs,
			})
			if err != nil {
				t.Fatalf("asking: %v", err)
			}
			var last *environment.Message
			for msg := range stream {
				m := msg
				last = &m
			}
			if last == nil {
				t.Fatal("nothing came back")
			}

			var called []string
			searched := false
			for _, one := range last.ToolCalls {
				called = append(called, one.Name)
				if strings.HasPrefix(one.Name, "web_") {
					searched = true
				}
			}

			switch {
			case c.search && !searched:
				t.Errorf("%q did not look it up; it called %v and said %q",
					c.said, called, first(last.Text, 110))
			case !c.search && searched:
				t.Errorf("%q was searched on the web; it called %v", c.said, called)
			default:
				t.Logf("%q called %v", c.said, called)
			}
		})
	}
}
