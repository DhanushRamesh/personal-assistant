"""A local HTTP service that points at the names in a sentence.

GLiNER is given the labels at call time, so the kinds of name this can
find are decided by the caller and never by retraining. It answers spans
cut out of the text it was given, which is the point of it: a span is
either in the sentence or it is not, so nothing here can invent a name.

    python3 gliner-service.py --model <dir-or-hub-id> --port 8092

POST /spans  {"text": "...", "labels": ["person", "place"], "threshold": 0.4}
          -> {"spans": [{"text": "Deepika", "label": "person", "score": 0.95}]}

GET  /health -> {"model": "...", "ready": true}

Loopback only. It has no authentication, and the machine it runs on is
the only thing that should reach it.
"""

import argparse
import json
import logging
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from gliner import GLiNER

# MaxBody : The most that will be read from one request. A sentence is
# small; anything this size is a mistake or a flood.
MaxBody = 64 * 1024

# Labels : What is looked for when a caller names nothing.
Labels = ["person", "place", "app", "song"]

# Threshold : How sure GLiNER must be before a span is reported.
Threshold = 0.4

log = logging.getLogger("gliner")


class Handler(BaseHTTPRequestHandler):
    model = None
    name = ""

    def reply(self, code, body):
        raw = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):
        if self.path.rstrip("/") == "/health":
            self.reply(200, {"model": self.name, "ready": self.model is not None})
        else:
            self.reply(404, {"error": "no such path"})

    def do_POST(self):
        if self.path.rstrip("/") != "/spans":
            self.reply(404, {"error": "no such path"})
            return

        length = int(self.headers.get("Content-Length") or 0)
        if length > MaxBody:
            self.reply(413, {"error": "body too large"})
            return
        try:
            asked = json.loads(self.rfile.read(length) or b"{}")
        except ValueError as err:
            self.reply(400, {"error": f"unreadable body: {err}"})
            return

        text = (asked.get("text") or "").strip()
        if not text:
            self.reply(200, {"spans": []})
            return

        labels = asked.get("labels") or Labels
        threshold = float(asked.get("threshold") or Threshold)
        try:
            found = self.model.predict_entities(text, labels, threshold=threshold)
        except Exception as err:  # a bad label list, a too-long sentence
            log.exception("could not read %r", text[:80])
            self.reply(500, {"error": str(err)})
            return

        self.reply(200, {"spans": [
            {"text": e["text"], "label": e["label"], "score": round(float(e["score"]), 4)}
            for e in found
        ]})

    def log_message(self, fmt, *args):
        log.info(fmt, *args)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--model", default="urchade/gliner_small-v2.1",
                    help="a local directory or a Hugging Face id")
    ap.add_argument("--host", default="127.0.0.1")
    ap.add_argument("--port", type=int, default=8092)
    args = ap.parse_args()

    logging.basicConfig(level=logging.INFO,
                        format="%(asctime)s %(levelname)s %(message)s")
    log.info("loading %s", args.model)
    Handler.model = GLiNER.from_pretrained(args.model)
    Handler.name = args.model

    log.info("listening on %s:%d", args.host, args.port)
    ThreadingHTTPServer((args.host, args.port), Handler).serve_forever()


if __name__ == "__main__":
    main()
