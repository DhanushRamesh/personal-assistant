# Prompt caching: please move the cache breakpoint

**To:** the PlatformAI gateway team
**About:** `POST /internalapi/v2/ai/chat`, `ai_vendor: anthropic`
**Asking for:** the cache mark placed after `tools` and `context`,
instead of after `messages`.

## What works today

`prompt_caching` is accepted and does what it says. On a 4,523-token
request:

| | input | cache write | cache read | credits |
| --- | --- | --- | --- | --- |
| no `prompt_caching` | 4,523 | 0 | 0 | 13.64 |
| `{"ttl":"1h"}`, first | 3 | 4,520 | 0 | 27.20 |
| `{"ttl":"1h"}`, again | 3 | 0 | 4,520 | **1.44** |

A write costs twice and a read costs a tenth, which is exactly what is
wanted. Tool schemas cache the same way: 8,310 of 8,313 tokens, 25.01
credits down to 2.58.

## What stops us using it

The mark appears to sit at the end of the whole payload, the user's
message included. Same `context`, same `tools`, only the question
changed:

```
Q1 first    write 4517   27.19
Q1 again    read  4517    1.44
Q2 new      write 4517   27.19     <- a second entry, not a read
Q1 again    read  4517    1.44
Q2 again    read  4517    1.44
```

Each distinct request gets its own entry. In an assistant, every turn
carries a question nobody has asked before, so every turn would write
and none would read. Switching it on is not a saving; it is close to
double.

## What we are asking for

One cache breakpoint after `tools` and `context` -- the part that is
byte-identical from turn to turn -- rather than only after `messages`.
Anthropic allows up to four, so this need not replace the existing one.

## What it is worth

A measured turn of ours: 46,633 tokens in, of which the persona is
45,362 bytes and the tool schemas 120,397 bytes -- about 90% of the
request, unchanged between turns. Read at a tenth, that is roughly
**75-80% off the input cost of every turn**, and most of the pre-fill
latency with it.

Our traffic for two days was 21.5M input tokens over 255 turns and 553
background jobs. The same work with a prefix breakpoint is in the order
of 5M.

## What we have already done

`prompt_caching` is wired behind a configuration key
(`platformai.prompt_cache_ttl`) and left empty, so the day the
breakpoint moves this is one line of configuration rather than a
release. We have not enabled it, because today it would cost more.

## How these numbers were taken

Direct calls to the endpoint with a fixed 4,523-token `context`,
reading `data.usage.ai_stats` and `data.usage.zoho_total_credits` from
the reply. Happy to share the script.
