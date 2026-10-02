/// A thin web client for the personal assistant server.
library;

import 'package:flutter/material.dart';

import 'address_bar.dart';
import 'api/client.dart';
import 'api/stored_token.dart';
import 'design/design.dart';
import 'routing.dart';
import 'screens/home.dart';
import 'screens/login.dart';
import 'screens/settings.dart';
import 'state/app_state.dart';

void main() {
  usePaths();
  runApp(
    ClientApp(
      state: AppState(
        api: AssistantApi(baseUrl: resolveServerUrl(), tokens: StoredToken()),
      ),
    ),
  );
}

/// ClientApp : The root. Holds the state and decides which screen is showing.
class ClientApp extends StatefulWidget {
  const ClientApp({super.key, required this.state});

  final AppState state;

  @override
  State<ClientApp> createState() => _ClientAppState();
}

class _ClientAppState extends State<ClientApp> {
  /// _started : Whether the attempt to reuse a kept token has finished.
  ///
  /// Until it has, neither screen is right: showing the login would make
  /// someone who is already signed in watch it flash past.
  bool _started = false;

  late final AppRouter _router = AppRouter(pagesFor: _pagesFor);

  @override
  void initState() {
    super.initState();
    // Signing in and out changes which page belongs at the address, and
    // the router hears about it from nowhere else.
    widget.state.addListener(_signedInOut);
    widget.state.start().whenComplete(() {
      if (mounted) setState(() => _started = true);
    });
  }

  @override
  void dispose() {
    widget.state.removeListener(_signedInOut);
    widget.state.dispose();
    _router.dispose();
    super.dispose();
  }

  /// _signedInOut : Keeps the address answerable after signing in or out.
  ///
  /// Signing out from settings leaves a settings address showing a login,
  /// and the next sign-in would open settings rather than the
  /// conversation.
  void _signedInOut() {
    if (!widget.state.signedIn && _router.route.inSettings) {
      _router.go(AppRoute.home);
      return;
    }
    _router.refresh();
  }

  /// _pagesFor : The stack an address stands for.
  ///
  /// Settings sits on top of the conversation rather than replacing it,
  /// so closing it goes back to the conversation even when the address
  /// was opened cold.
  List<Page<dynamic>> _pagesFor(AppRoute route) {
    if (!_started) {
      return const [
        MaterialPage(key: ValueKey('starting'), child: _Starting()),
      ];
    }
    if (!widget.state.signedIn) {
      return [
        MaterialPage(
          key: const ValueKey('login'),
          child: LoginScreen(state: widget.state),
        ),
      ];
    }
    return [
      MaterialPage(
        key: const ValueKey('home'),
        child: HomeScreen(
          state: widget.state,
          onSettings: () =>
              _router.go(AppRoute.settings(SettingsModule.account)),
        ),
      ),
      if (route.module case final module?)
        MaterialPage(
          key: const ValueKey('settings'),
          child: SettingsScreen(
            state: widget.state,
            module: module,
            onModule: (m) => _router.go(AppRoute.settings(m)),
            // What the page is looking at travels in the address, so a
            // refresh lands where the person was rather than at the top
            // of an unfiltered list.
            where: route.where,
            onWhere: (w) => _router.go(route.looking(w)),
          ),
        ),
    ];
  }

  @override
  Widget build(BuildContext context) {
    return MaterialApp.router(
      title: 'Assistant',
      debugShowCheckedModeBanner: false,
      theme: AppTheme.light,
      darkTheme: AppTheme.dark,
      routeInformationParser: const AppRouteParser(),
      routerDelegate: _router,
    );
  }
}

/// _Starting : What is shown while the kept token is being checked.
class _Starting extends StatelessWidget {
  const _Starting();

  @override
  Widget build(BuildContext context) => Scaffold(
    backgroundColor: context.colors.background,
    body: const Center(child: AppSpinner(size: 24)),
  );
}
