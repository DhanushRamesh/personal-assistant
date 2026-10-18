//go:build evals

package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/domain/persona"
)

// readySaid : A question whose answer is one word however much is sent
// with it.
//
// The measurement is the time to the first word, and a question whose
// answer varies in length varies in when it starts for reasons that
// have nothing to do with the prompt. This one cannot be answered by a
// tool and cannot be answered at length.
const readySaid = "Reply with the single word: ready. Nothing else, no address, no punctuation."

// sizes : How many tools to send, against what the request carries
// today and what retrieval would send instead.
var sizes = []int{0, 20, 50}

// spins : How many times each size is asked.
//
// Five, alternating rather than in blocks, because the gateway is
// slower at some minutes than others and three blocks of five would
// measure the minute as much as the prompt.
const spins = 5

// turns : How many times each size is asked, raised with EVAL_SPINS.
//
// Five was not enough to order them: twenty tools came back slower than
// fifty, which is the shape of noise rather than of a cost.
func turns() int {
	if n := os.Getenv("EVAL_SPINS"); n != "" {
		var got int
		if _, err := fmt.Sscanf(n, "%d", &got); err == nil && got > 0 {
			return got
		}
	}
	return spins
}

// TestWhatTheToolBlockCostsInTime : Whether a smaller prompt reaches
// its first word sooner.
//
// The question the whole of the tool-retrieval work rests on. Tools are
// 117,923 bytes of a 196,382-byte request, so choosing twenty by
// meaning rather than describing fifty takes about a third off every
// call -- which is certainly cheaper and may be no faster at all.
//
// What is already known is that the first word arrives at a median of
// 2.94s whatever is sent, and that the two smallest calls of 70 on 18
// October came back in 1.66s. Two calls is not evidence, and this is
// the evidence.
func TestWhatTheToolBlockCostsInTime(t *testing.T) {
	env, _, cfg := harness(t)
	registry := registered(t)
	address := persona.AddressFor(cfg.Assistant.Persona)
	system := persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name, persona.Spoken)

	all := registry.For(chat.ChannelVoice)
	specs := make([]environment.ToolSpec, 0, len(all))
	for _, x := range all {
		schema, err := tool.Narrated(x.Params, address).MarshalJSON()
		if err != nil {
			t.Fatalf("%s has an unusable schema: %v", x.Name, err)
		}
		specs = append(specs, environment.ToolSpec{
			Name: x.Name, Description: tool.NarratedDescription(x.Description(), address),
			Parameters: schema,
		})
	}
	t.Logf("%d tools available to send", len(specs))

	bytesOf := func(n int) int {
		if n > len(specs) {
			n = len(specs)
		}
		raw, _ := json.Marshal(specs[:n])
		return len(raw)
	}

	first := map[int][]time.Duration{}
	whole := map[int][]time.Duration{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	for spin := 0; spin < turns(); spin++ {
		for _, n := range sizes {
			send := specs
			if n < len(specs) {
				send = specs[:n]
			}
			began := time.Now()
			var seen time.Duration
			stream, err := env.Run(ctx, environment.Request{
				Prompt: readySaid, SystemPrompt: system, Tools: send,
			})
			if err != nil {
				t.Fatalf("%d tools: %v", n, err)
			}
			for msg := range stream {
				if seen == 0 && (msg.Kind == environment.KindPartial || msg.Kind.Terminal()) {
					seen = time.Since(began)
				}
			}
			if seen == 0 {
				seen = time.Since(began)
			}
			first[n] = append(first[n], seen)
			whole[n] = append(whole[n], time.Since(began))
			time.Sleep(Pace)
		}
	}

	median := func(v []time.Duration) time.Duration {
		sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
		return v[len(v)/2]
	}
	fmt.Printf("\n  the same question, %d times at each size:\n\n", turns())
	fmt.Printf("    %6s  %12s  %14s  %12s\n", "tools", "tool bytes", "first word", "whole call")
	var base time.Duration
	for _, n := range sizes {
		f, w := median(first[n]), median(whole[n])
		if n == 0 {
			base = f
		}
		fmt.Printf("    %6d  %11d   %13v  %11v\n", n, bytesOf(n), f.Round(time.Millisecond), w.Round(time.Millisecond))
	}
	fmt.Printf("\n  against no tools at all:\n")
	for _, n := range sizes[1:] {
		fmt.Printf("    %2d tools cost %+v to the first word\n", n, (median(first[n]) - base).Round(time.Millisecond))
	}
	fmt.Println()
}
