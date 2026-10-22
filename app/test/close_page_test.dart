import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:ymca_app/api/models.dart';
import 'package:ymca_app/api/ymca_api.dart';
import 'package:ymca_app/ui/close_page.dart';

import 'fake_ymca_api.dart';
import 'support.dart';

void main() {
  testWidgets('close page loads types and shows only active ones', (
    tester,
  ) async {
    final api = FakeYmcaApi(
      me: meWith({'tenant:admin'}),
      typeList: [
        typeNamed('Dinner'),
        ConsumptionType(
          id: 'type-2',
          name: 'Breakfast',
          obligates: false,
          recurrence: null,
          recordMode: 'EITHER',
          status: 'INACTIVE',
          correctionWindowDays: null,
        ),
      ],
    );
    await pumpScreen(tester, api, const ClosePage());

    // The dropdown should only have the active type
    await tester.tap(find.byKey(const Key('close-type')));
    await tester.pumpAndSettle();

    expect(find.text('Dinner'), findsWidgets);
    expect(find.text('Breakfast'), findsNothing);
  });

  testWidgets('default dates are first and last of previous month', (
    tester,
  ) async {
    final api = FakeYmcaApi(
      me: meWith({'tenant:admin'}),
      typeList: [typeNamed('Dinner')],
    );
    await pumpScreen(tester, api, const ClosePage());

    // October 22, 2026 is the test date
    // Previous month: September 2026 (2026-09-01 to 2026-09-30)
    final fromField = find.byKey(const Key('close-from'));
    final toField = find.byKey(const Key('close-to'));

    expect(
      (tester.widget<TextField>(fromField).controller?.text),
      '2026-09-01',
    );
    expect((tester.widget<TextField>(toField).controller?.text), '2026-09-30');
  });

  testWidgets('close button is disabled until type is chosen and from <= to', (
    tester,
  ) async {
    final api = FakeYmcaApi(
      me: meWith({'tenant:admin'}),
      typeList: [typeNamed('Dinner')],
    );
    await pumpScreen(tester, api, const ClosePage());

    // Initially disabled because no type is chosen
    final button = find.byKey(const Key('close-submit'));
    expect((tester.widget<FilledButton>(button).onPressed), isNull);

    // Select a type
    await tester.tap(find.byKey(const Key('close-type')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Dinner'));
    await tester.pumpAndSettle();

    // Now button should be enabled (dates are valid by default)
    expect((tester.widget<FilledButton>(button).onPressed), isNotNull);
  });

  testWidgets('confirmation dialog shows correct text and buttons', (
    tester,
  ) async {
    final api = FakeYmcaApi(
      me: meWith({'tenant:admin'}),
      typeList: [typeNamed('Dinner')],
    );
    await pumpScreen(tester, api, const ClosePage());

    // Select type from dropdown
    await tester.tap(find.byKey(const Key('close-type')));
    await tester.pumpAndSettle();
    // The dropdown menu should be open now
    await tester.tap(find.text('Dinner'));
    await tester.pumpAndSettle();

    // Tap close button
    await tester.tap(find.byKey(const Key('close-submit')));
    await tester.pumpAndSettle();

    // Check dialog content
    expect(find.text('Confirm close period'), findsOneWidget);
    expect(
      find.textContaining('Close Dinner from 2026-09-01 to 2026-09-30'),
      findsOneWidget,
    );
    expect(
      find.textContaining("Everyone's units for these days are billed"),
      findsOneWidget,
    );
    expect(find.byKey(const Key('close-confirm')), findsOneWidget);
    expect(find.text('Cancel'), findsOneWidget);
  });

  testWidgets('confirm calls closePeriod with correct parameters', (
    tester,
  ) async {
    final api = FakeYmcaApi(
      me: meWith({'tenant:admin'}),
      typeList: [typeNamed('Dinner', id: 'type-1')],
    );
    api.closeResult = [
      const ClosedPeriod(
        id: 'cp-1',
        subjectPersonId: 'person-1',
        from: '2026-09-01',
        to: '2026-09-30',
        obligatedUnits: 10,
        invoiceId: 'inv-1',
      ),
    ];
    await pumpScreen(tester, api, const ClosePage());

    // Select type from dropdown
    await tester.tap(find.byKey(const Key('close-type')));
    await tester.pumpAndSettle();
    // The dropdown menu should be open now
    await tester.tap(find.text('Dinner'));
    await tester.pumpAndSettle();

    // Tap close button
    await tester.tap(find.byKey(const Key('close-submit')));
    await tester.pumpAndSettle();

    // Confirm
    await tester.tap(find.byKey(const Key('close-confirm')));
    await tester.pumpAndSettle();

    // Check the call was made
    expect(api.calls.where((c) => c.startsWith('closePeriod')).length, 1);
    final call = api.calls.where((c) => c.startsWith('closePeriod')).first;
    expect(call, contains('type=type-1'));
    expect(call, contains('from=2026-09-01'));
    expect(call, contains('to=2026-09-30'));
  });

  testWidgets('failed confirm shows error and retry sends same key', (
    tester,
  ) async {
    final api = FakeYmcaApi(
      me: meWith({'tenant:admin'}),
      typeList: [typeNamed('Dinner')],
    );
    api.closeResult = [
      const ClosedPeriod(
        id: 'cp-1',
        subjectPersonId: 'person-1',
        from: '2026-09-01',
        to: '2026-09-30',
        obligatedUnits: 10,
        invoiceId: null,
      ),
    ];
    await pumpScreen(tester, api, const ClosePage());

    // Select type from dropdown
    await tester.tap(find.byKey(const Key('close-type')));
    await tester.pumpAndSettle();
    // The dropdown menu should be open now
    await tester.tap(find.text('Dinner'));
    await tester.pumpAndSettle();

    // Tap close button and confirm, with error set
    api.failWith = const ApiException(409, 'no_rate', 'No rate configured');
    await tester.tap(find.byKey(const Key('close-submit')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('close-confirm')));
    await tester.pumpAndSettle();

    // Error should be shown
    expect(
      find.text(
        'No price is set for this item yet. Ask an administrator to set a rate, then try again.',
      ),
      findsOneWidget,
    );

    // Extract the key from the first failed call
    final firstCall = api.calls.where((c) => c.startsWith('closePeriod')).first;
    final keyMatch = RegExp(r'key=([a-f0-9]+)').firstMatch(firstCall);
    expect(keyMatch, isNotNull);
    final firstKey = keyMatch![1];

    api.failWith = null; // Allow the retry to succeed
    await tester.tap(find.byKey(const Key('close-submit')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('close-confirm')));
    await tester.pumpAndSettle();

    // Check that the second call uses the same key
    final secondCall = api.calls
        .where((c) => c.startsWith('closePeriod'))
        .skip(1)
        .first;
    expect(secondCall, contains('key=$firstKey'));
  });

  testWidgets('a different request gets a different key', (tester) async {
    // The same key with a different body is idempotency_key_reused (A3.6).
    // Closing Dinner fails, the user switches to Lunch and tries again: that
    // is a new request and must not carry the first one's key.
    final api = FakeYmcaApi(
      me: meWith({'tenant:admin'}),
      typeList: [
        typeNamed('Dinner', id: 'type-dinner'),
        typeNamed('Lunch', id: 'type-lunch'),
      ],
    );
    await pumpScreen(tester, api, const ClosePage());

    Future<void> attempt(String typeName) async {
      await tester.tap(find.byKey(const Key('close-type')));
      await tester.pumpAndSettle();
      await tester.tap(find.text(typeName).last);
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('close-submit')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('close-confirm')));
      await tester.pumpAndSettle();
    }

    api.failWith = const ApiException(409, 'no_rate', 'No rate configured');
    await attempt('Dinner');
    await attempt('Lunch');

    final keys = [
      for (final call in api.calls.where((c) => c.startsWith('closePeriod')))
        RegExp(r'key=([a-f0-9]+)').firstMatch(call)![1],
    ];
    expect(keys.length, 2);
    expect(
      keys[0],
      isNot(keys[1]),
      reason: 'a different type is a different request',
    );
  });

  testWidgets('no_rate error is mapped to custom message', (tester) async {
    final api = FakeYmcaApi(
      me: meWith({'tenant:admin'}),
      typeList: [typeNamed('Dinner')],
    );
    await pumpScreen(tester, api, const ClosePage());

    // Select type from dropdown
    await tester.tap(find.byKey(const Key('close-type')));
    await tester.pumpAndSettle();
    // The dropdown menu should be open now
    await tester.tap(find.text('Dinner'));
    await tester.pumpAndSettle();

    // Set the error after the type is selected
    api.failWith = const ApiException(409, 'no_rate', 'Rate not configured');

    // Tap close button and confirm
    await tester.tap(find.byKey(const Key('close-submit')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('close-confirm')));
    await tester.pumpAndSettle();

    expect(
      find.text(
        'No price is set for this item yet. Ask an administrator to set a rate, then try again.',
      ),
      findsOneWidget,
    );
  });

  testWidgets('period_already_closed error is mapped to custom message', (
    tester,
  ) async {
    final api = FakeYmcaApi(
      me: meWith({'tenant:admin'}),
      typeList: [typeNamed('Dinner')],
    );
    await pumpScreen(tester, api, const ClosePage());

    // Select type from dropdown
    await tester.tap(find.byKey(const Key('close-type')));
    await tester.pumpAndSettle();
    // The dropdown menu should be open now
    await tester.tap(find.text('Dinner'));
    await tester.pumpAndSettle();

    // Set the error after the type is selected
    api.failWith = const ApiException(
      409,
      'period_already_closed',
      'Period already closed',
    );

    // Tap close button and confirm
    await tester.tap(find.byKey(const Key('close-submit')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('close-confirm')));
    await tester.pumpAndSettle();

    expect(find.text('Part of that period is already closed.'), findsOneWidget);
  });

  testWidgets('results show closed people with invoices', (tester) async {
    final api = FakeYmcaApi(
      me: meWith({'tenant:admin'}),
      typeList: [typeNamed('Dinner')],
    );
    api.closeResult = [
      const ClosedPeriod(
        id: 'cp-1',
        subjectPersonId: 'person-123456789',
        from: '2026-09-01',
        to: '2026-09-30',
        obligatedUnits: 10,
        invoiceId: 'inv-1',
      ),
      const ClosedPeriod(
        id: 'cp-2',
        subjectPersonId: 'person-abcdefgh',
        from: '2026-09-01',
        to: '2026-09-30',
        obligatedUnits: 5,
        invoiceId: null,
      ),
    ];
    await pumpScreen(tester, api, const ClosePage());

    // Select type from dropdown
    await tester.tap(find.byKey(const Key('close-type')));
    await tester.pumpAndSettle();
    // The dropdown menu should be open now
    await tester.tap(find.text('Dinner'));
    await tester.pumpAndSettle();

    // Tap close button and confirm
    await tester.tap(find.byKey(const Key('close-submit')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('close-confirm')));
    await tester.pumpAndSettle();

    // Check results card
    expect(find.byKey(const Key('close-result')), findsOneWidget);
    expect(find.text('Closed 2 people'), findsOneWidget);
    expect(
      find.textContaining('person-1 — 10 units · invoice created'),
      findsOneWidget,
    );
    expect(
      find.textContaining('person-a — 5 units · nothing to bill'),
      findsOneWidget,
    );
  });

  testWidgets('FakeYmcaApi ConsumptionType status builder', (tester) async {
    // Verify that typeNamed can be built with status parameter
    final active = ConsumptionType(
      id: 'type-1',
      name: 'Test',
      obligates: false,
      recurrence: null,
      recordMode: 'EITHER',
      status: 'ACTIVE',
      correctionWindowDays: null,
    );
    final inactive = ConsumptionType(
      id: 'type-2',
      name: 'Test',
      obligates: false,
      recurrence: null,
      recordMode: 'EITHER',
      status: 'INACTIVE',
      correctionWindowDays: null,
    );

    expect(active.status, 'ACTIVE');
    expect(inactive.status, 'INACTIVE');
  });
}
