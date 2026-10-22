import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:provider/provider.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:ymca_app/api/ymca_api.dart';
import 'package:ymca_app/state/app_controller.dart';

import 'fake_ymca_api.dart';

/// Mounts [screen] the way the app does: a [YmcaApi] and an [AppController]
/// above it. [page] means the screen brings its own Scaffold.
Future<AppController> pumpScreen(
  WidgetTester tester,
  FakeYmcaApi api,
  Widget screen, {
  bool page = false,
}) async {
  SharedPreferences.setMockInitialValues({});
  final controller = AppController(
    await SharedPreferences.getInstance(),
    apiFactory: (_) => api,
  );
  await controller.signIn(
    const Session(baseUrl: 'http://x', tenantId: 't', token: 'k'),
  );
  // The providers sit ABOVE the MaterialApp, as in the real app: a pushed
  // route or a bottom sheet is not a descendant of `home`, and providers
  // placed inside it are invisible to both. Tests that mounted them inside
  // `home` passed while the app threw on the first tap.
  await tester.pumpWidget(
    MultiProvider(
      providers: [
        Provider<YmcaApi>.value(value: api),
        ChangeNotifierProvider<AppController>.value(value: controller),
      ],
      child: MaterialApp(home: page ? screen : Scaffold(body: screen)),
    ),
  );
  await tester.pumpAndSettle();
  return controller;
}

/// The `key=` of the n-th recorded call whose name starts with [name].
String keyOfCall(FakeYmcaApi api, String name, int n) {
  final call = api.calls.where((c) => c.startsWith('$name ')).elementAt(n);
  return RegExp(r'key=(\S+)').firstMatch(call)![1]!;
}

int callCount(FakeYmcaApi api, String name) =>
    api.calls.where((c) => c == name || c.startsWith('$name ')).length;
