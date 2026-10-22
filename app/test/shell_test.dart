import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:ymca_app/api/ymca_api.dart';
import 'package:ymca_app/app.dart';
import 'package:ymca_app/state/app_controller.dart';

import 'fake_ymca_api.dart';

Future<AppController> controllerFor(FakeYmcaApi api) async {
  SharedPreferences.setMockInitialValues({});
  return AppController(await SharedPreferences.getInstance(),
      apiFactory: (_) => api);
}

Future<void> signIn(WidgetTester tester, AppController c) async {
  await tester.pumpWidget(YmcaApp(controller: c));
  await tester.enterText(find.byKey(const Key('tenant')), 'tenant-1');
  await tester.enterText(find.byKey(const Key('token')), 'token');
  await tester.tap(find.byKey(const Key('sign-in')));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('a member sees Meals and Bills and nothing administrative',
      (tester) async {
    final c = await controllerFor(FakeYmcaApi(me: meWith({})));
    await signIn(tester, c);

    expect(find.text('Meals'), findsWidgets);
    expect(find.text('Bills'), findsOneWidget);
    expect(find.text('Members'), findsNothing);
    expect(find.text('Invoices'), findsNothing);
    expect(find.text('Close'), findsNothing);
  });

  testWidgets('each permission reveals exactly its own destination',
      (tester) async {
    final c = await controllerFor(FakeYmcaApi(
        me: meWith({'tenant:may_approve_membership', 'tenant:finance_reader'})));
    await signIn(tester, c);

    expect(find.text('Members'), findsOneWidget);
    expect(find.text('Invoices'), findsOneWidget);
    expect(find.text('Close'), findsNothing,
        reason: 'closing a period needs tenant:admin, which was not granted');
  });

  testWidgets('an administrator sees every destination', (tester) async {
    final c = await controllerFor(FakeYmcaApi(
        me: meWith({
      'tenant:admin',
      'tenant:may_approve_membership',
      'tenant:finance_reader',
      'tenant:finance_manager',
    })));
    await signIn(tester, c);

    for (final label in ['Meals', 'Bills', 'Members', 'Invoices', 'Close']) {
      expect(find.text(label), findsWidgets, reason: label);
    }
  });

  testWidgets('a refused sign-in shows a human message, not the server\'s',
      (tester) async {
    final api = FakeYmcaApi(me: meWith({}))
      ..failWith = const ApiException(401, 'unauthenticated', 'bad signature');
    final c = await controllerFor(api);
    await signIn(tester, c);

    expect(find.byKey(const Key('login-error')), findsOneWidget);
    expect(find.textContaining('bad signature'), findsNothing);
    expect(find.text('Please sign in again.'), findsOneWidget);
  });

  testWidgets('signing out returns to the sign-in page', (tester) async {
    final c = await controllerFor(FakeYmcaApi(me: meWith({})));
    await signIn(tester, c);
    await tester.tap(find.byKey(const Key('sign-out')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('sign-in')), findsOneWidget);
  });
}
