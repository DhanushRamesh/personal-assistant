/// Sending the browser somewhere, in a browser.
library;

import 'package:web/web.dart' as web;

/// openUrl : Replaces the current page.
bool openUrl(String url) {
  web.window.location.href = url;
  return true;
}
