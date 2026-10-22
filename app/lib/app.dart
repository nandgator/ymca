import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import 'api/ymca_api.dart';
import 'state/app_controller.dart';
import 'ui/login_page.dart';
import 'ui/shell.dart';

/// The root. While signed in, [YmcaApi] is provided ABOVE the Navigator, so
/// every route the app pushes — an invoice, the admission form, a bottom sheet
/// — can read it. Providing it inside `home:` would reach only the home route:
/// a pushed route is a sibling of `home` under the Navigator, not a child, and
/// the first tap on an invoice would throw ProviderNotFoundError. When the
/// session ends the whole subtree is replaced by the sign-in page, so no
/// screen can outlive its credential.
class YmcaApp extends StatelessWidget {
  const YmcaApp({super.key, required this.controller});

  final AppController controller;

  @override
  Widget build(BuildContext context) {
    return ChangeNotifierProvider.value(
      value: controller,
      child: MaterialApp(
        title: 'YMCA',
        theme: ThemeData(
          colorSchemeSeed: const Color(0xFF0B5FA5),
          useMaterial3: true,
        ),
        builder: (context, child) {
          final api = context.watch<AppController>().api;
          if (api == null) return child!;
          return Provider<YmcaApi>.value(value: api, child: child!);
        },
        home: Consumer<AppController>(
          builder: (context, c, _) =>
              c.signedIn ? const AppShell() : const LoginPage(),
        ),
      ),
    );
  }
}
