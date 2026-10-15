package aloud

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
)

type home struct {
	mu                  sync.Mutex
	said, reached, rang []string
}

func (h *home) Available() bool { return true }
func (h *home) Say(_ context.Context, m string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.said = append(h.said, m)
	return nil
}
func (h *home) Reach(_ context.Context, m string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.reached = append(h.reached, m)
	return nil
}
func (h *home) Notify(_ context.Context, m string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rang = append(h.rang, m)
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// app : Joins an app that answers every announcement with failure, after noting it.
func app(v *Voices, failure string) (*[]string, func()) {
	var heard []string
	var done func(string, string)
	done, leave := v.Join("u", "laptop", func(_ context.Context, request, text string) error {
		heard = append(heard, text)
		go done(request, failure)
		return nil
	})
	return &heard, leave
}

func TestNoAppIsHomeAssistant(t *testing.T) {
	h := &home{}
	v := New(h, quiet())
	if err := v.Say(context.Background(), "Sir. The battery is low."); err != nil {
		t.Fatal(err)
	}
	if len(h.said) != 1 {
		t.Fatalf("home said %v", h.said)
	}
}

func TestTheAppSaysIt(t *testing.T) {
	h := &home{}
	v := New(h, quiet())
	heard, _ := app(v, "")
	if err := v.Say(context.Background(), "Sir. The battery is low."); err != nil {
		t.Fatal(err)
	}
	if len(*heard) != 1 || len(h.said) != 0 {
		t.Fatalf("app heard %v, home said %v", *heard, h.said)
	}
}

func TestReachRingsThePhoneToo(t *testing.T) {
	h := &home{}
	v := New(h, quiet())
	app(v, "")
	if err := v.Reach(context.Background(), "Sir. Leave now."); err != nil {
		t.Fatal(err)
	}
	if len(h.reached) != 0 || len(h.rang) != 1 {
		t.Fatalf("reached %v, rang %v", h.reached, h.rang)
	}
}

func TestNoVoiceIsHomeAssistant(t *testing.T) {
	h := &home{}
	v := New(h, quiet())
	app(v, NoVoice)
	if err := v.Say(context.Background(), "Sir. The battery is low."); err != nil {
		t.Fatal(err)
	}
	if len(h.said) != 1 {
		t.Fatalf("home said %v", h.said)
	}
}

func TestAnAppThatLeftIsHomeAssistant(t *testing.T) {
	h := &home{}
	v := New(h, quiet())
	_, leave := app(v, "")
	leave()
	if err := v.Say(context.Background(), "Sir. The battery is low."); err != nil {
		t.Fatal(err)
	}
	if len(h.said) != 1 {
		t.Fatalf("home said %v", h.said)
	}
}

func TestLeavingMidwayIsHomeAssistant(t *testing.T) {
	h := &home{}
	v := New(h, quiet())
	var leave func()
	_, leave = v.Join("u", "laptop", func(context.Context, string, string) error {
		go leave()
		return nil
	})
	if err := v.Say(context.Background(), "Sir. The battery is low."); err != nil {
		t.Fatal(err)
	}
	if len(h.said) != 1 {
		t.Fatalf("home said %v", h.said)
	}
}

func TestAFailureIsReported(t *testing.T) {
	v := New(&home{}, quiet())
	app(v, "still speaking after 90s")
	if err := v.Say(context.Background(), "Sir. The battery is low."); err == nil {
		t.Fatal("a failure was not reported")
	}
}
