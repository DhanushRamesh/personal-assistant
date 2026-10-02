/// Where in the app the address bar is pointing.
///
/// Small and closed rather than a general router: there are two screens
/// and a handful of settings pages, and a routing package would be more
/// code than this to configure.
library;

import 'package:flutter/material.dart';

import 'screens/settings.dart';

/// AppRoute : A place in the app, and the address that names it.
class AppRoute {
  const AppRoute._(this.module, [this.where = const {}]);

  /// module : Which settings page, or null for the conversation.
  final SettingsModule? module;

  /// where : Where a page is looking, as query parameters.
  ///
  /// Which part of a page is open and what it is filtered to. It lives
  /// in the address so a refresh lands where the person was: the events
  /// list is read by filtering it down and then going away to check
  /// something, and coming back to the top of an unfiltered list each
  /// time makes it unusable for the one thing it is for.
  ///
  /// A plain map rather than a field per page. The settings screens are
  /// the only things with state worth keeping, each wants something
  /// different, and a closed type here would have to grow every time
  /// one of them learns a new filter.
  final Map<String, String> where;

  /// home : The conversation.
  static const home = AppRoute._(null);

  /// settings : One settings page, looking at whatever it was looking at.
  ///
  /// Empty values are dropped, so a cleared filter leaves the address
  /// rather than sitting in it as "kind=".
  factory AppRoute.settings(
    SettingsModule module, {
    Map<String, String> where = const {},
  }) => AppRoute._(module, {
    for (final e in where.entries)
      if (e.value.isNotEmpty) e.key: e.value,
  });

  /// inSettings : Whether the settings screen is showing.
  bool get inSettings => module != null;

  /// at : What the page was looking at under one name, or empty.
  String at(String name) => where[name] ?? '';

  /// looking : The same route, looking somewhere else.
  AppRoute looking(Map<String, String> next) =>
      module == null ? this : AppRoute.settings(module!, where: next);

  /// uri : The address this shows in the bar.
  Uri get uri => module == null
      ? Uri(path: '/')
      : Uri(
          path: '/settings/${module!.name.toLowerCase()}',
          queryParameters: where.isEmpty ? null : where,
        );

  /// path : The address as written.
  String get path => uri.toString();

  /// parse : The route an address names.
  ///
  /// Anything unrecognised is the conversation. A mistyped address should
  /// land somewhere usable rather than on a blank page, and the address
  /// bar is corrected to match.
  static AppRoute parse(String? location) {
    final uri = Uri.parse(location ?? '/');
    final parts = uri.pathSegments;
    if (parts.isEmpty || parts.first != 'settings') return home;

    final where = uri.queryParameters;
    if (parts.length == 1) {
      return AppRoute.settings(SettingsModule.account, where: where);
    }
    for (final m in SettingsModule.values) {
      if (m.name.toLowerCase() == parts[1]) {
        return AppRoute.settings(m, where: where);
      }
    }
    return AppRoute.settings(SettingsModule.account, where: where);
  }

  @override
  bool operator ==(Object other) =>
      other is AppRoute &&
      other.module == module &&
      _sameWhere(other.where, where);

  @override
  int get hashCode => Object.hash(
    module,
    Object.hashAllUnordered(where.entries.map((e) => Object.hash(e.key, e.value))),
  );

  static bool _sameWhere(Map<String, String> a, Map<String, String> b) {
    if (a.length != b.length) return false;
    for (final e in a.entries) {
      if (b[e.key] != e.value) return false;
    }
    return true;
  }
}

/// AppRouteParser : Turns the address into a route and back.
class AppRouteParser extends RouteInformationParser<AppRoute> {
  const AppRouteParser();

  @override
  Future<AppRoute> parseRouteInformation(
    RouteInformation routeInformation,
  ) async => AppRoute.parse(routeInformation.uri.toString());

  @override
  RouteInformation restoreRouteInformation(AppRoute configuration) =>
      RouteInformation(uri: configuration.uri);
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
