/// Whether the address bar shows a path or a fragment.
///
/// Flutter's default on the web is a fragment, so a settings page reads
/// as localhost:8000/#/settings/account. That is not what anybody types,
/// and it never reaches the static server, so nothing can be served for
/// it. Paths need the web implementation; the native one does nothing,
/// since there is no address bar off the web.
library;

import 'address_bar_stub.dart'
    if (dart.library.js_interop) 'address_bar_web.dart'
    as platform;

/// usePaths : Puts routes in the address itself rather than after a hash.
void usePaths() => platform.usePaths();
