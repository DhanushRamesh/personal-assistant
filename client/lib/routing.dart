/// Where in the app the address bar is pointing.
///
/// Small and closed rather than a general router: there are two screens
/// and a handful of settings pages, and a routing package would be more
/// code than this to configure.
library;

import 'package:flutter/material.dart';

import 'screens/settings.dart';

/// AppRoute : A place in the app, and the path that names it.
class AppRoute {
  const AppRoute._(this.module);

  /// module : Which settings page, or null for the conversation.
  final SettingsModule? module;

  /// home : The conversation.
  static const home = AppRoute._(null);

  /// settings : One settings page.
  factory AppRoute.settings(SettingsModule module) => AppRoute._(module);

  /// inSettings : Whether the settings screen is showing.
  bool get inSettings => module != null;

  /// path : The address this shows in the bar.
  String get path =>
      module == null ? '/' : '/settings/${module!.name.toLowerCase()}';

  /// parse : The route a path names.
  ///
  /// Anything unrecognised is the conversation. A mistyped address should
  /// land somewhere usable rather than on a blank page, and the address
  /// bar is corrected to match.
  static AppRoute parse(String? location) {
    final parts = Uri.parse(location ?? '/').pathSegments;
    if (parts.isEmpty || parts.first != 'settings') return home;
    if (parts.length == 1) return AppRoute.settings(SettingsModule.account);

    for (final m in SettingsModule.values) {
      if (m.name.toLowerCase() == parts[1]) return AppRoute.settings(m);
    }
    return AppRoute.settings(SettingsModule.account);
  }

  @override
  bool operator ==(Object other) => other is AppRoute && other.module == module;

  @override
  int get hashCode => module.hashCode;
}

/// AppRouteParser : Turns the address into a route and back.
class AppRouteParser extends RouteInformationParser<AppRoute> {
  const AppRouteParser();

  @override
  Future<AppRoute> parseRouteInformation(
    RouteInformation routeInformation,
  ) async => AppRoute.parse(routeInformation.uri.path);

  @override
  RouteInformation restoreRouteInformation(AppRoute configuration) =>
      RouteInformation(uri: Uri.parse(configuration.path));
}

/// AppRouter : Holds where the app is, and rebuilds when it moves.
///
/// A ChangeNotifier as well as a delegate, so anything can push a route
/// without reaching for a context.
class AppRouter extends RouterDelegate<AppRoute>
    with ChangeNotifier, PopNavigatorRouterDelegateMixin<AppRoute> {
  AppRouter({required this.pagesFor});

  /// pagesFor : What to show for a route. The screens need know nothing
  /// about routing; this is the only place that maps one to the other.
  final List<Page<dynamic>> Function(AppRoute route) pagesFor;

  AppRoute _route = AppRoute.home;

  /// route : Where the app is.
  AppRoute get route => _route;

  @override
  AppRoute get currentConfiguration => _route;

  @override
  final GlobalKey<NavigatorState> navigatorKey = GlobalKey<NavigatorState>();

  /// go : Moves to a route, and with it the address bar.
  void go(AppRoute next) {
    if (next == _route) return;
    _route = next;
    notifyListeners();
  }

  /// refresh : Rebuilds where the app is without moving it, for when what
  /// belongs at the address has changed.
  void refresh() => notifyListeners();

  @override
  Future<void> setNewRoutePath(AppRoute route) async {
    _route = route;
    notifyListeners();
  }

  @override
  Widget build(BuildContext context) => Navigator(
    key: navigatorKey,
    pages: pagesFor(_route),
    onDidRemovePage: (page) {
      // The only thing above home is settings, so going back from
      // anywhere is going home.
      if (_route.inSettings) go(AppRoute.home);
    },
  );
}
