import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import 'api/ymca_api.dart';
import 'state/app_controller.dart';
import 'ui/login_page.dart';
import 'ui/shell.dart';

/// The root. While signed in, [YmcaApi] and [Me] are provided below the
/// controller so a screen reads `context.read<YmcaApi>()` and nothing about
/// HTTP; when the session ends the whole subtree is replaced by the sign-in
/// page, so no screen can outlive its credential.
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
        home: Consumer<AppController>(
          builder: (context, c, _) {
            if (!c.signedIn) return const LoginPage();
            return MultiProvider(
              providers: [
                Provider<YmcaApi>.value(value: c.api!),
              ],
              child: const AppShell(),
            );
          },
        ),
      ),
    );
  }
}
