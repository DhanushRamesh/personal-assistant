package platformai

import (
	"encoding/json"
	"testing"
)

// TestTheGatewaysOwnFiguresAreRead : The credits and cache counts are
// taken from the reply, not worked out here.
//
// The bytes are a real final chunk from /internalapi/v2/ai/chat on 20
// October 2026, trimmed only of the other credit lines. Until that day
// the parser read three fields of ai_stats and dropped the rest,
// including the only priced numbers anywhere in this server.
func TestTheGatewaysOwnFiguresAreRead(t *testing.T) {
	const final = `{"data":{"stream":true,"is_final_chunk":true,"usage":{
		"zoho_cache_read_credits":0,"zoho_available_credits":151827.9,
		"zoho_input_credits":0.048,"zoho_output_credits":0.074999996,
		"zoho_total_credits":0.122999996,
		"ai_stats":{"total_tokens":21,"output_tokens":5,
			"cached_5min_input_tokens":0,"cached_1hour_input_tokens":0,
			"cache_read_tokens":0,"input_tokens":16,"cached_tokens":0}}}}`

	var chunk struct {
		Data *struct {
			Final bool     `json:"is_final_chunk"`
			Usage wireCost `json:"usage"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(final), &chunk); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if chunk.Data == nil || !chunk.Data.Final {
		t.Fatal("the final chunk did not read as final")
	}
	got := chunk.Data.Usage
	if got.Spent != 0.122999996 {
		t.Errorf("spent = %v, want the gateway's own figure", got.Spent)
	}
	if got.Available != 151827.9 {
		t.Errorf("available = %v, want the balance after the call", got.Available)
	}
	if got.Stats.Input != 16 || got.Stats.Output != 5 {
		t.Errorf("tokens = %d in, %d out", got.Stats.Input, got.Stats.Output)
	}
	if got.Stats.CacheRead != 0 || got.Stats.CacheWrite() != 0 {
		t.Errorf("cache = %d read, %d written, want none while caching is off",
			got.Stats.CacheRead, got.Stats.CacheWrite())
	}
}

// TestACachedCallIsCountedBothWays : A write and a read are told apart,
// because they are priced differently -- twice and a tenth.
func TestACachedCallIsCountedBothWays(t *testing.T) {
	var write wireUsage
	if err := json.Unmarshal([]byte(`{"input_tokens":3,"output_tokens":5,
		"cached_1hour_input_tokens":4520,"cached_5min_input_tokens":0,
		"cache_read_tokens":0}`), &write); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if write.CacheWrite() != 4520 || write.CacheRead != 0 {
		t.Errorf("a write read as %d written, %d read", write.CacheWrite(), write.CacheRead)
	}

	var read wireUsage
	if err := json.Unmarshal([]byte(`{"input_tokens":3,"output_tokens":5,
		"cached_1hour_input_tokens":0,"cached_5min_input_tokens":0,
		"cache_read_tokens":4520}`), &read); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if read.CacheRead != 4520 || read.CacheWrite() != 0 {
		t.Errorf("a read read as %d read, %d written", read.CacheRead, read.CacheWrite())
	}
}
