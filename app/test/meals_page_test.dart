import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:ymca_app/api/models.dart';
import 'package:ymca_app/api/ymca_api.dart';
import 'package:ymca_app/ui/meals_page.dart';
import 'package:ymca_app/util/dates.dart';

import 'fake_ymca_api.dart';
import 'support.dart';

ConsumptionRecord record(
  String id,
  String day, {
  List<String> options = const [],
  bool voided = false,
}) => ConsumptionRecord(
  id: id,
  typeId: 'dinner',
  subjectPersonId: 'person-me',
  actorPersonId: 'person-me',
  occurredOn: day,
  variantCode: null,
  optionCodes: options,
  supersedesId: null,
  voided: voided,
  recordedAt: '${day}T12:00:00Z',
);

const egg = ConsumptionOption(
  id: 'o1',
  code: 'EGG',
  name: 'Boiled egg',
  chargeKind: 'USAGE_FEE',
  amount: '15.00',
  currency: 'INR',
  status: 'AVAILABLE',
);

FakeYmcaApi apiWith({List<ConsumptionRecord> records = const []}) =>
    FakeYmcaApi(
      me: meWith({}),
      recordable: [
        typeNamed('Dinner', id: 'dinner'),
        typeNamed('Staff desk', id: 'desk', mode: 'STAFF'),
      ],
      optionList: [egg],
      records: records,
    );

Future<void> openSheet(WidgetTester tester) async {
  await tester.tap(find.byKey(const Key('record-dinner')));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('offers only the types a person may record for themselves', (
    tester,
  ) async {
    await pumpScreen(tester, apiWith(), const MealsPage());

    expect(find.byKey(const Key('record-dinner')), findsOneWidget);
    expect(
      find.byKey(const Key('record-desk')),
      findsNothing,
      reason: 'a STAFF-mode type is not for self-service',
    );
  });

  testWidgets('history shows live records with their options, not voids', (
    tester,
  ) async {
    await pumpScreen(
      tester,
      apiWith(
        records: [
          record('r1', '2026-10-02', options: ['EGG']),
          record('r2', '2026-10-01', voided: true),
        ],
      ),
      const MealsPage(),
    );

    expect(find.text('2026-10-02 · EGG'), findsOneWidget);
    expect(find.text('2026-10-01'), findsNothing);
  });

  testWidgets('saving sends the chosen day and options', (tester) async {
    final api = apiWith();
    await pumpScreen(tester, api, const MealsPage());
    await openSheet(tester);

    await tester.tap(find.byKey(const Key('day-1')));
    await tester.tap(find.byKey(const Key('option-EGG')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('save-meal')));
    await tester.pumpAndSettle();

    final call = api.calls.firstWhere((c) => c.startsWith('recordUse'));
    expect(call, contains('type=dinner'));
    expect(call, contains('on=${daysAgo(1)}'));
    expect(call, contains('options=EGG'));
    expect(find.text('Recorded'), findsOneWidget);
    expect(
      find.byKey(const Key('save-meal')),
      findsNothing,
      reason: 'the sheet closes on success',
    );
  });

  testWidgets('a retry of the same meal reuses the same key', (tester) async {
    final api = apiWith();
    await pumpScreen(tester, api, const MealsPage());
    await openSheet(tester);

    api.failCalls['recordUse'] = const ApiException(
      500,
      'internal',
      'lost the response',
    );
    await tester.tap(find.byKey(const Key('save-meal')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('meal-error')), findsOneWidget);

    await tester.tap(find.byKey(const Key('save-meal')));
    await tester.pumpAndSettle();

    expect(callCount(api, 'recordUse'), 2);
    expect(
      keyOfCall(api, 'recordUse', 1),
      keyOfCall(api, 'recordUse', 0),
      reason: 'a retry is the same meal, so it carries the same key',
    );
  });

  testWidgets('a different day after a refusal is a different request', (
    tester,
  ) async {
    final api = apiWith();
    await pumpScreen(tester, api, const MealsPage());
    await openSheet(tester);

    api.failCalls['recordUse'] = const ApiException(
      409,
      'already_recorded',
      'dup',
    );
    await tester.tap(find.byKey(const Key('save-meal')));
    await tester.pumpAndSettle();
    expect(find.text('That day is already recorded.'), findsOneWidget);
    expect(
      find.byKey(const Key('save-meal')),
      findsOneWidget,
      reason: 'the sheet stays open so another day can be chosen',
    );

    await tester.tap(find.byKey(const Key('day-1')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('save-meal')));
    await tester.pumpAndSettle();

    expect(
      keyOfCall(api, 'recordUse', 1),
      isNot(keyOfCall(api, 'recordUse', 0)),
      reason: 'the same key with a different body is idempotency_key_reused',
    );
  });

  testWidgets('a failed load says so and retries', (tester) async {
    final api = apiWith();
    api.failCalls['myRecordableTypes'] = const ApiException(
      500,
      'internal',
      'boom',
    );
    await pumpScreen(tester, api, const MealsPage());

    expect(
      find.text('Something went wrong. Please try again.'),
      findsOneWidget,
    );
    await tester.tap(find.byKey(const Key('retry')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('record-dinner')), findsOneWidget);
  });

  testWidgets(
    'an expired token signs the user out instead of showing an error',
    (tester) async {
      final api = apiWith();
      api.failCalls['myRecordableTypes'] = const ApiException(
        401,
        'token_expired',
        'expired',
      );
      final c = await pumpScreen(tester, api, const MealsPage());

      expect(c.signedIn, isFalse);
    },
  );
}
