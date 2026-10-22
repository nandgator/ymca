import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:ymca_app/api/ymca_api.dart';
import 'package:ymca_app/api/models.dart';
import 'package:ymca_app/ui/admit_member_page.dart';

import 'fake_ymca_api.dart';
import 'support.dart';

FakeYmcaApi apiWithPlans() => FakeYmcaApi(
  me: meWith({'tenant:may_approve_membership'}),
  planList: const [
    MembershipPlan(id: 'open', code: 'RES', name: 'Resident', status: 'OPEN'),
    MembershipPlan(
      id: 'closed',
      code: 'OLD',
      name: 'Legacy',
      status: 'CLOSED_TO_NEW',
    ),
  ],
);

Future<void> fill(
  WidgetTester tester, {
  String name = 'Dev User',
  String number = 'H-1',
}) async {
  await tester.enterText(find.byKey(const Key('person-name')), name);
  await tester.enterText(find.byKey(const Key('member-number')), number);
  await tester.tap(find.byKey(const Key('plan')));
  await tester.pumpAndSettle();
  await tester.tap(find.text('Resident').last);
  await tester.pumpAndSettle();
}

Future<void> submit(WidgetTester tester) async {
  await tester.tap(find.byKey(const Key('submit-admit')));
  await tester.pumpAndSettle();
}

Future<void> open(WidgetTester tester, FakeYmcaApi api) =>
    pumpScreen(tester, api, const AdmitMemberPage(), page: true);

void main() {
  testWidgets('offers only plans that are open to new members', (tester) async {
    await open(tester, apiWithPlans());
    await tester.tap(find.byKey(const Key('plan')));
    await tester.pumpAndSettle();

    expect(find.text('Resident'), findsWidgets);
    expect(find.text('Legacy'), findsNothing);
  });

  testWidgets('cannot be submitted until everything is given', (tester) async {
    await open(tester, apiWithPlans());
    FilledButton button() =>
        tester.widget<FilledButton>(find.byKey(const Key('submit-admit')));

    expect(button().onPressed, isNull);
    await tester.enterText(find.byKey(const Key('person-name')), 'Dev User');
    await tester.enterText(find.byKey(const Key('member-number')), 'H-1');
    await tester.pump();
    expect(button().onPressed, isNull, reason: 'no plan chosen yet');
  });

  testWidgets('registers the person, then admits them, with a key', (
    tester,
  ) async {
    final api = apiWithPlans();
    await open(tester, api);
    await fill(tester);
    await submit(tester);

    expect(callCount(api, 'registerPerson'), 1);
    expect(callCount(api, 'admit'), 1);
    final admit = api.calls.firstWhere((c) => c.startsWith('admit'));
    expect(admit, contains('plan=open'));
    expect(admit, contains('number=H-1'));
    expect(
      api.calls.indexWhere((c) => c.startsWith('registerPerson')),
      lessThan(api.calls.indexWhere((c) => c.startsWith('admit'))),
    );
  });

  testWidgets(
    'a failed admit retried does not register the person twice, and keeps the key',
    (tester) async {
      final api = apiWithPlans();
      await open(tester, api);
      await fill(tester);

      api.failCalls['admit'] = const ApiException(500, 'internal', 'lost');
      await submit(tester);
      await submit(tester);

      expect(
        callCount(api, 'registerPerson'),
        1,
        reason:
            'the person exists already; registering again would duplicate them',
      );
      expect(callCount(api, 'admit'), 2);
      expect(keyOfCall(api, 'admit', 1), keyOfCall(api, 'admit', 0));
    },
  );

  testWidgets(
    'a taken number is shown beside the number; fixing it is a new request',
    (tester) async {
      final api = apiWithPlans();
      await open(tester, api);
      await fill(tester);

      api.failCalls['admit'] = const ApiException(
        409,
        'membership_number_taken',
        'taken',
      );
      await submit(tester);
      expect(
        find.text('That membership number is already in use.'),
        findsOneWidget,
      );

      await tester.enterText(find.byKey(const Key('member-number')), 'H-2');
      await submit(tester);

      expect(callCount(api, 'registerPerson'), 1);
      expect(
        keyOfCall(api, 'admit', 1),
        isNot(keyOfCall(api, 'admit', 0)),
        reason:
            'the same key with a different number is idempotency_key_reused',
      );
      expect(api.calls.last, contains('number=H-2'));
    },
  );

  testWidgets('an existing membership is reported plainly', (tester) async {
    final api = apiWithPlans();
    await open(tester, api);
    await fill(tester);

    api.failCalls['admit'] = const ApiException(
      409,
      'already_active_member',
      'dup',
    );
    await submit(tester);
    expect(find.text('That person is already a member.'), findsOneWidget);
  });

  testWidgets('changing the name registers a different person', (tester) async {
    final api = apiWithPlans();
    await open(tester, api);
    await fill(tester);

    api.failCalls['admit'] = const ApiException(500, 'internal', 'lost');
    await submit(tester);
    await tester.enterText(
      find.byKey(const Key('person-name')),
      'Someone Else',
    );
    await submit(tester);

    expect(callCount(api, 'registerPerson'), 2);
  });

  testWidgets('plans that fail to load can be retried', (tester) async {
    final api = apiWithPlans();
    api.failCalls['plans'] = const ApiException(500, 'internal', 'boom');
    await open(tester, api);

    await tester.tap(find.byKey(const Key('retry')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('plan')), findsOneWidget);
  });
}
