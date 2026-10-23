import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:ymca_app/api/models.dart';
import 'package:ymca_app/api/ymca_api.dart';
import 'package:ymca_app/ui/invoice_detail_page.dart';

import 'fake_ymca_api.dart';
import 'support.dart';

FakeYmcaApi apiFor(String state, {Set<String>? permissions}) => FakeYmcaApi(
  me: meWith(permissions ?? {'tenant:finance_manager'}),
  invoiceById: {'i1': invoiceOf('i1', state: state, balance: '2000.00')},
);

Future<void> open(WidgetTester tester, FakeYmcaApi api, {bool staff = true}) =>
    pumpScreen(
      tester,
      api,
      InvoiceDetailPage(invoiceId: 'i1', staff: staff),
      page: true,
    );

void main() {
  testWidgets('shows the number, totals, charges and the due date', (
    tester,
  ) async {
    await open(tester, apiFor('ISSUED'), staff: false);

    expect(find.text('INV/2026-27/000001'), findsOneWidget);
    expect(find.text('Total'), findsOneWidget);
    expect(find.text('2000.00 INR'), findsWidgets);
    expect(find.text('Dinner, 2026-09-01 to 2026-09-30'), findsOneWidget);
    expect(find.text('USAGE_FEE: 2000.00 INR'), findsOneWidget);
    expect(find.text('2026-10-31'), findsOneWidget);
  });

  testWidgets('a payer sees no actions', (tester) async {
    await open(tester, apiFor('DRAFT'), staff: false);
    expect(find.byKey(const Key('issue-invoice')), findsNothing);
    await open(tester, apiFor('ISSUED'), staff: false);
    expect(find.byKey(const Key('pay-invoice')), findsNothing);
  });

  testWidgets('staff without finance_manager see no actions either', (
    tester,
  ) async {
    await open(tester, apiFor('DRAFT', permissions: {'tenant:finance_reader'}));
    expect(find.byKey(const Key('issue-invoice')), findsNothing);
  });

  testWidgets('staff issue a draft with a key, once', (tester) async {
    final api = apiFor('DRAFT');
    await open(tester, api);

    await tester.tap(find.byKey(const Key('issue-invoice')));
    await tester.pumpAndSettle();

    expect(callCount(api, 'issueInvoice'), 1);
    expect(find.text('Invoice issued'), findsOneWidget);
  });

  testWidgets('a retried issue carries the same key', (tester) async {
    final api = apiFor('DRAFT');
    await open(tester, api);

    api.failCalls['issueInvoice'] = const ApiException(
      500,
      'internal',
      'lost the response',
    );
    await tester.tap(find.byKey(const Key('issue-invoice')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('issue-invoice')));
    await tester.pumpAndSettle();

    expect(callCount(api, 'issueInvoice'), 2);
    expect(
      keyOfCall(api, 'issueInvoice', 1),
      keyOfCall(api, 'issueInvoice', 0),
    );
  });

  group('recording a payment', () {
    Future<void> openDialog(WidgetTester tester, FakeYmcaApi api) async {
      await open(tester, api);
      await tester.tap(find.byKey(const Key('pay-invoice')));
      await tester.pumpAndSettle();
    }

    Future<void> chooseMethod(WidgetTester tester, String method) async {
      await tester.tap(find.byKey(const Key('pay-method')));
      await tester.pumpAndSettle();
      await tester.tap(find.text(method).last);
      await tester.pumpAndSettle();
    }

    testWidgets('the amount starts at the balance', (tester) async {
      await openDialog(tester, apiFor('ISSUED'));
      expect(find.widgetWithText(TextField, '2000.00'), findsOneWidget);
    });

    testWidgets('a method is required', (tester) async {
      final api = apiFor('ISSUED');
      await openDialog(tester, api);
      await tester.tap(find.byKey(const Key('save-payment')));
      await tester.pumpAndSettle();

      expect(find.text('Choose a method and enter an amount.'), findsOneWidget);
      expect(callCount(api, 'payInvoice'), 0);
    });

    testWidgets('saving sends the method, amount, currency and a key', (
      tester,
    ) async {
      final api = apiFor('ISSUED');
      await openDialog(tester, api);
      await chooseMethod(tester, 'UPI');
      await tester.tap(find.byKey(const Key('save-payment')));
      await tester.pumpAndSettle();

      final call = api.calls.firstWhere((c) => c.startsWith('payInvoice'));
      expect(call, contains('method=UPI'));
      expect(call, contains('amount=2000.00'));
      expect(call, contains('currency=INR'));
      expect(find.text('Payment recorded'), findsOneWidget);
    });

    testWidgets('a refusal stays in the dialog, and the retry reuses the key', (
      tester,
    ) async {
      final api = apiFor('ISSUED');
      await openDialog(tester, api);
      await chooseMethod(tester, 'CASH');

      api.failCalls['payInvoice'] = const ApiException(
        409,
        'payment_exceeds_balance',
        'too much',
      );
      await tester.tap(find.byKey(const Key('save-payment')));
      await tester.pumpAndSettle();
      expect(find.text('That is more than is owed.'), findsOneWidget);

      await tester.tap(find.byKey(const Key('save-payment')));
      await tester.pumpAndSettle();
      expect(keyOfCall(api, 'payInvoice', 1), keyOfCall(api, 'payInvoice', 0));
    });

    testWidgets('a different amount after a refusal is a different request', (
      tester,
    ) async {
      final api = apiFor('ISSUED');
      await openDialog(tester, api);
      await chooseMethod(tester, 'CASH');

      api.failCalls['payInvoice'] = const ApiException(
        409,
        'payment_exceeds_balance',
        'too much',
      );
      await tester.tap(find.byKey(const Key('save-payment')));
      await tester.pumpAndSettle();

      await tester.enterText(find.byKey(const Key('pay-amount')), '500.00');
      await tester.tap(find.byKey(const Key('save-payment')));
      await tester.pumpAndSettle();

      expect(
        keyOfCall(api, 'payInvoice', 1),
        isNot(keyOfCall(api, 'payInvoice', 0)),
      );
    });
  });

  group('issuing a credit note', () {
    Future<void> openCredit(WidgetTester tester, FakeYmcaApi api) async {
      await open(tester, api);
      await tester.tap(find.byKey(const Key('credit-invoice')));
      await tester.pumpAndSettle();
    }

    Future<void> fill(WidgetTester tester, String amount, String reason) async {
      await tester.enterText(find.byKey(const Key('credit-amount')), amount);
      await tester.enterText(find.byKey(const Key('credit-reason')), reason);
    }

    for (final (state, offered) in [
      ('ISSUED', true),
      ('PAID', true),
      ('DRAFT', false),
      ('VOIDED', false),
    ]) {
      testWidgets(
        'a $state invoice ${offered ? "can" : "cannot"} be credited',
        (tester) async {
          await open(tester, apiFor(state));
          expect(
            find.byKey(const Key('credit-invoice')),
            offered ? findsOneWidget : findsNothing,
          );
        },
      );
    }

    testWidgets('is not offered to a payer', (tester) async {
      await open(tester, apiFor('ISSUED'), staff: false);
      expect(find.byKey(const Key('credit-invoice')), findsNothing);
    });

    testWidgets('is not offered to a finance reader', (tester) async {
      await open(
        tester,
        apiFor('ISSUED', permissions: {'tenant:finance_reader'}),
      );
      expect(find.byKey(const Key('credit-invoice')), findsNothing);
    });

    testWidgets('needs an amount and a reason', (tester) async {
      final api = apiFor('ISSUED');
      await openCredit(tester, api);
      await fill(tester, '15.00', '');
      await tester.tap(find.byKey(const Key('save-credit')));
      await tester.pumpAndSettle();

      expect(find.text('Enter an amount and a reason.'), findsOneWidget);
      expect(callCount(api, 'creditInvoice'), 0);
    });

    testWidgets('sends the amount, the reason and a key', (tester) async {
      final api = apiFor('ISSUED');
      await openCredit(tester, api);
      await fill(tester, '15.00', 'one egg was never served');
      await tester.tap(find.byKey(const Key('save-credit')));
      await tester.pumpAndSettle();

      final call = api.calls.firstWhere((c) => c.startsWith('creditInvoice'));
      expect(call, contains('amount=15.00'));
      expect(call, contains('reason=one egg was never served'));
      expect(find.text('Credit note issued'), findsOneWidget);
    });

    testWidgets('a refusal stays in the dialog, and the retry reuses the key', (
      tester,
    ) async {
      final api = apiFor('ISSUED');
      await openCredit(tester, api);
      await fill(tester, '9999.00', 'too much');
      api.failCalls['creditInvoice'] = const ApiException(
        409,
        'credit_exceeds_invoice',
        'more than asked',
      );
      await tester.tap(find.byKey(const Key('save-credit')));
      await tester.pumpAndSettle();
      expect(
        find.text('That is more than the invoice still asks for.'),
        findsOneWidget,
      );

      await tester.tap(find.byKey(const Key('save-credit')));
      await tester.pumpAndSettle();
      expect(
        keyOfCall(api, 'creditInvoice', 1),
        keyOfCall(api, 'creditInvoice', 0),
      );
    });

    testWidgets('a different amount or reason is a different request', (
      tester,
    ) async {
      final api = apiFor('ISSUED');
      await openCredit(tester, api);
      await fill(tester, '9999.00', 'too much');
      api.failCalls['creditInvoice'] = const ApiException(
        409,
        'credit_exceeds_invoice',
        'more than asked',
      );
      await tester.tap(find.byKey(const Key('save-credit')));
      await tester.pumpAndSettle();

      await tester.enterText(find.byKey(const Key('credit-reason')), 'fixed');
      api.failCalls['creditInvoice'] = const ApiException(
        409,
        'credit_exceeds_invoice',
        'more than asked',
      );
      await tester.tap(find.byKey(const Key('save-credit')));
      await tester.pumpAndSettle();

      expect(
        keyOfCall(api, 'creditInvoice', 1),
        isNot(keyOfCall(api, 'creditInvoice', 0)),
      );
    });
  });

  testWidgets('shows credit notes, and a refund due instead of a balance', (
    tester,
  ) async {
    final api = FakeYmcaApi(
      me: meWith({'tenant:finance_manager'}),
      invoiceById: {
        'i1': invoiceOf(
          'i1',
          state: 'PAID',
          balance: '-10.00',
          creditNotes: const [
            CreditNote(
              id: 'cn1',
              number: 'CN/2026-27/000001',
              amount: '10.00',
              currency: 'INR',
              reason: 'overcharge',
              issuedAt: '2026-10-02T00:00:00Z',
            ),
          ],
        ),
      },
    );
    await open(tester, api, staff: false);

    expect(find.text('Credit notes'), findsOneWidget);
    expect(find.text('CN/2026-27/000001 10.00 INR'), findsOneWidget);
    expect(find.text('overcharge'), findsOneWidget);
    expect(find.text('Refund due'), findsOneWidget);
    expect(find.text('Balance'), findsNothing);
    expect(find.text('-10.00 INR'), findsNothing);
  });
}
