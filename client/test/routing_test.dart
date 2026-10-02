import 'package:flutter_test/flutter_test.dart';
import 'package:personal_assistant_client/routing.dart';
import 'package:personal_assistant_client/screens/settings.dart';

void main() {
  test('a settings page is named by its path', () {
    expect(AppRoute.home.path, '/');
    expect(AppRoute.settings(SettingsModule.account).path, '/settings/account');
    expect(
      AppRoute.settings(SettingsModule.reminders).path,
      '/settings/reminders',
    );
  });

  // Every page has to be reachable by typing it, or the address is
  // decoration. This fails the day a module is added without a path.
  test('every settings page can be reached and read back', () {
    for (final m in SettingsModule.values) {
      final route = AppRoute.settings(m);
      expect(
        AppRoute.parse(route.path),
        route,
        reason: '${m.name} does not survive its own path',
      );
    }
  });

  test('the conversation is the root', () {
    expect(AppRoute.parse('/'), AppRoute.home);
    expect(AppRoute.parse(null), AppRoute.home);
    expect(
      AppRoute.parse('/'),
      isNot(AppRoute.settings(SettingsModule.account)),
    );
  });

  // Settings with nothing after it is settings, not a blank page.
  test('bare settings lands on the first page', () {
    expect(
      AppRoute.parse('/settings'),
      AppRoute.settings(SettingsModule.account),
    );
    expect(
      AppRoute.parse('/settings/'),
      AppRoute.settings(SettingsModule.account),
    );
  });

  // A mistyped or stale address lands somewhere usable rather than on
  // nothing at all.
  test('an address that means nothing lands somewhere', () {
    expect(AppRoute.parse('/nowhere'), AppRoute.home);
    expect(
      AppRoute.parse('/settings/nowhere'),
      AppRoute.settings(SettingsModule.account),
    );
  });

  test('going somewhere else moves the address', () {
    final router = AppRouter(pagesFor: (_) => []);
    var moved = 0;
    router.addListener(() => moved++);

    router.go(AppRoute.settings(SettingsModule.reminders));
    expect(router.currentConfiguration.path, '/settings/reminders');
    expect(moved, 1);

    // Already there. Telling everyone again would rebuild the stack for
    // nothing.
    router.go(AppRoute.settings(SettingsModule.reminders));
    expect(moved, 1);
  });

  // The browser hands back an address; it has to land, or a reload of a
  // settings page opens the conversation.
  test('an address handed in by the browser is taken', () async {
    final router = AppRouter(pagesFor: (_) => []);

    await router.setNewRoutePath(AppRoute.parse('/settings/server'));

    expect(router.route, AppRoute.settings(SettingsModule.server));
  });

  group('where', () {
    test('a plain settings address has no query', () {
      expect(AppRoute.settings(SettingsModule.knows).path, '/settings/knows');
    });

    test('what a page is looking at travels in the address', () {
      final route = AppRoute.settings(
        SettingsModule.knows,
        where: {'section': 'events', 'kind': 'call.missed'},
      );
      expect(route.path, contains('/settings/knows'));
      expect(route.path, contains('section=events'));
      expect(route.path, contains('kind=call.missed'));
    });

    test('and comes back off it', () {
      final route = AppRoute.parse('/settings/knows?section=events&kind=call.made');
      expect(route.module, SettingsModule.knows);
      expect(route.at('section'), 'events');
      expect(route.at('kind'), 'call.made');
      expect(route.at('nothing'), '');
    });

    test('a cleared filter leaves the address rather than sitting in it', () {
      final route = AppRoute.settings(
        SettingsModule.knows,
        where: {'section': 'events', 'kind': ''},
      );
      expect(route.path, contains('section=events'));
      expect(route.path, isNot(contains('kind')));
    });

    test('a dotted kind survives the round trip', () {
      final there = AppRoute.settings(
        SettingsModule.knows,
        where: {'kind': 'place.stayed'},
      );
      expect(AppRoute.parse(there.path).at('kind'), 'place.stayed');
    });

    test('two routes differing only in where are different places', () {
      final a = AppRoute.settings(SettingsModule.knows, where: {'kind': 'x'});
      final b = AppRoute.settings(SettingsModule.knows, where: {'kind': 'y'});
      final c = AppRoute.settings(SettingsModule.knows, where: {'kind': 'x'});
      expect(a, isNot(b));
      expect(a, c);
      expect(a.hashCode, c.hashCode);
    });

    test('looking somewhere else keeps the page', () {
      final route = AppRoute.settings(SettingsModule.knows, where: {'kind': 'x'});
      final next = route.looking({'kind': 'y', 'section': 'events'});
      expect(next.module, SettingsModule.knows);
      expect(next.at('kind'), 'y');
    });

    test('a query on an unknown page still lands somewhere usable', () {
      final route = AppRoute.parse('/settings/nonsense?section=events');
      expect(route.module, SettingsModule.account);
      expect(route.at('section'), 'events');
    });
  });
}
