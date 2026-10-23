import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:ymca_app/api/models.dart';
import 'package:ymca_app/api/ymca_api.dart';
import 'package:ymca_app/ui/record_for_member_sheet.dart';
import 'package:ymca_app/util/dates.dart';

import 'fake_ymca_api.dart';
import 'support.dart';

const alice = DirectoryEntry(
  membershipId: 'm1',
  personId: 'person-alice',
  displayName: 'Alice',
  number: 'H-1',
  state: 'ACTIVE',
  planId: 'plan',
);

FakeYmcaApi apiOf({int utcOffsetSeconds = 0}) => FakeYmcaApi(
  me: meWith({
    'tenant:may_approve_membership',
  }, utcOffsetSeconds: utcOffsetSeconds),
  recordable: [
    typeNamed('Dinner', id: 'dinner'),
    typeNamed('Lunch', id: 'lunch'),
    typeNamed('Self check-in', id: 'self', mode: 'SELF'),
  ],
);

Future<void> open(WidgetTester tester, FakeYmcaApi api) async {
  await pumpScreen(
    tester,
    api,
    Builder(
      builder: (context) => TextButton(
        onPressed: () => showRecordForMemberSheet(context, alice),
        child: const Text('open'),
      ),
    ),
  );
  await tester.tap(find.text('open'));
  await tester.pumpAndSettle();
}

Future<void> save(WidgetTester tester) async {
  await tester.tap(find.byKey(const Key('rfm-save')));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('offers only the types staff may record for others', (
    tester,
  ) async {
    await open(tester, apiOf());
    await tester.tap(find.byKey(const Key('rfm-type')));
    await tester.pumpAndSettle();

    expect(find.text('Dinner'), findsWidgets);
    expect(find.text('Lunch'), findsWidgets);
    expect(
      find.text('Self check-in'),
      findsNothing,
      reason: 'a SELF-mode type cannot be recorded for somebody else',
    );
  });

  testWidgets('saves for the member, on the chosen day, with a key', (
    tester,
  ) async {
    final api = apiOf();
    await open(tester, api);
    await tester.tap(find.byKey(const Key('rfm-day-1')));
    await tester.pump();
    await save(tester);

    final call = api.calls.firstWhere((c) => c.startsWith('recordUse'));
    expect(call, contains('type=dinner'));
    expect(call, contains('subject=person-alice'));
    expect(call, contains('on=${daysAgo(1)}'));
    expect(find.text('Recorded for Alice'), findsOneWidget);
    expect(find.byKey(const Key('rfm-save')), findsNothing);
  });

  for (final offset in [14 * 3600, -11 * 3600]) {
    testWidgets('records on the tenant\'s day (offset $offset)', (
      tester,
    ) async {
      final api = apiOf(utcOffsetSeconds: offset);
      await open(tester, api);
      await tester.tap(find.byKey(const Key('rfm-day-1')));
      await tester.pump();
      await save(tester);

      final call = api.calls.firstWhere((c) => c.startsWith('recordUse'));
      expect(call, contains('on=${Calendar(offset).daysAgo(1)}'));
    });
  }

  testWidgets('a refusal leaves the form open; the retry reuses the key', (
    tester,
  ) async {
    final api = apiOf();
    await open(tester, api);

    api.failCalls['recordUse'] = const ApiException(500, 'internal', 'lost');
    await save(tester);
    expect(find.byKey(const Key('rfm-error')), findsOneWidget);
    expect(
      find.byKey(const Key('rfm-save')),
      findsOneWidget,
      reason: 'Save must still be there to press again',
    );

    await save(tester);
    expect(keyOfCall(api, 'recordUse', 1), keyOfCall(api, 'recordUse', 0));
  });

  testWidgets(
    'already recorded is said plainly, and another day is a new request',
    (tester) async {
      final api = apiOf();
      await open(tester, api);

      api.failCalls['recordUse'] = const ApiException(
        409,
        'already_recorded',
        'dup',
      );
      await save(tester);
      expect(
        find.text('That day is already recorded for them.'),
        findsOneWidget,
      );

      await tester.tap(find.byKey(const Key('rfm-day-2')));
      await tester.pump();
      await save(tester);
      expect(
        keyOfCall(api, 'recordUse', 1),
        isNot(keyOfCall(api, 'recordUse', 0)),
      );
    },
  );

  testWidgets('a forbidden answer does not claim to know which side refused', (
    tester,
  ) async {
    final api = apiOf();
    await open(tester, api);
    api.failCalls['recordUse'] = const ApiException(403, 'forbidden', 'no');
    await save(tester);

    expect(find.textContaining("can't record that for them"), findsOneWidget);
  });
}
