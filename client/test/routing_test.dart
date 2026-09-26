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
}
