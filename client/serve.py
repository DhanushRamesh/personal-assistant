"""Serve the built web client, with deep links.

A plain file server answers /settings/account with 404: there is no such
file, only an app that knows the address. Anything that is not a file on
disk is handed to index.html, which is what makes a reload of a settings
page work.
"""

import functools
import http.server
import os
import sys


class App(http.server.SimpleHTTPRequestHandler):
    def send_head(self):
        path = self.translate_path(self.path)
        if not os.path.exists(path) and "." not in os.path.basename(path):
            self.path = "/"
        return super().send_head()

    def log_message(self, *args):
        pass


def main():
    port = int(sys.argv[1])
    directory = sys.argv[2]
    handler = functools.partial(App, directory=directory)
    http.server.ThreadingHTTPServer(("127.0.0.1", port), handler).serve_forever()


if __name__ == "__main__":
    main()
