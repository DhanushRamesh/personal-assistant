/// Sending the browser somewhere outside the app.
///
/// Only Google's consent screen needs this, and only on the web: the
/// permission has to be granted at Google and the browser has to come
/// back to the callback, which is a round trip the app cannot make for
/// itself. Off the web there is nowhere to send, so the native side
/// does nothing and the screen offers the address to copy instead.
library;

import 'open_url_stub.dart'
    if (dart.library.js_interop) 'open_url_web.dart'
    as platform;

/// openUrl : Sends the browser to an address, returning whether it
/// could.
///
/// Same tab rather than a new one. Google sends the browser back to the
/// settings screen when it is done, and a new tab would leave the
/// answer in one window and the person looking at another.
bool openUrl(String url) => platform.openUrl(url);
