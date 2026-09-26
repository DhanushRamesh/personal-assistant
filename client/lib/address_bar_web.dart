/// The address bar in a browser.
library;

import 'package:flutter_web_plugins/url_strategy.dart';

/// usePaths : Drops the hash, so a route is a real path.
void usePaths() => usePathUrlStrategy();
