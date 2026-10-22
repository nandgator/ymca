import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:ymca_app/api/ymca_api.dart';
import 'package:ymca_app/ui/invoice_detail_page.dart';
import 'package:ymca_app/ui/invoices_page.dart';

import 'fake_ymca_api.dart';
import 'support.dart';

void main() {
  testWidgets('invoices page loads and displays invoices', (tester) async {
    final api = FakeYmcaApi(
      me: meWith({'tenant:finance_reader'}),
      allInvoices: [
        invoiceOf('inv-1', state: 'ISSUED', total: '1000.00'),
        invoiceOf('inv-2', state: 'DRAFT'),
      ],
    );
    await pumpScreen(tester, api, const InvoicesPage());

    expect(find.text('Draft invoice'), findsOneWidget);
    expect(find.text('INV/2026-27/000001'), findsOneWidget);
  });

  testWidgets('draft invoices show "Not yet issued"', (tester) async {
    final api = FakeYmcaApi(
      me: meWith({'tenant:finance_reader'}),
      allInvoices: [invoiceOf('inv-1', state: 'DRAFT')],
    );
    await pumpScreen(tester, api, const InvoicesPage());

    expect(find.text('Not yet issued'), findsOneWidget);
  });

  testWidgets('filters work client-side', (tester) async {
    final api = FakeYmcaApi(
      me: meWith({'tenant:finance_reader'}),
      allInvoices: [
        invoiceOf('inv-1', state: 'DRAFT'),
        invoiceOf('inv-2', state: 'ISSUED', number: 'INV/2026-27/000002'),
        invoiceOf('inv-3', state: 'PAID', balance: '0.00'),
      ],
    );
    await pumpScreen(tester, api, const InvoicesPage());

    expect(find.text('Draft invoice'), findsOneWidget);
    expect(find.text('INV/2026-27/000002'), findsOneWidget);

    await tester.tap(find.byKey(const Key('filter-draft')));
    await tester.pumpAndSettle();

    expect(find.text('Draft invoice'), findsOneWidget);
    expect(find.text('INV/2026-27/000002'), findsNothing);

    await tester.tap(find.byKey(const Key('filter-issued')));
    await tester.pumpAndSettle();

    expect(find.text('Draft invoice'), findsNothing);
    expect(find.text('INV/2026-27/000002'), findsOneWidget);

    await tester.tap(find.byKey(const Key('filter-paid')));
    await tester.pumpAndSettle();

    expect(find.text('Draft invoice'), findsNothing);
    expect(find.text('INV/2026-27/000002'), findsNothing);

    await tester.tap(find.byKey(const Key('filter-all')));
    await tester.pumpAndSettle();

    expect(find.text('Draft invoice'), findsOneWidget);
    expect(find.text('INV/2026-27/000002'), findsOneWidget);
  });

  testWidgets('tapping invoice navigates to detail page', (tester) async {
    final api = FakeYmcaApi(
      me: meWith({'tenant:finance_reader'}),
      allInvoices: [invoiceOf('inv-1', state: 'ISSUED')],
    );
    await pumpScreen(tester, api, const InvoicesPage());

    await tester.tap(find.text('INV/2026-27/000001'));
    await tester.pumpAndSettle();

    expect(find.byType(InvoiceDetailPage), findsOneWidget);
  });

  testWidgets('empty state displays friendly message', (tester) async {
    final api = FakeYmcaApi(
      me: meWith({'tenant:finance_reader'}),
      allInvoices: const [],
    );
    await pumpScreen(tester, api, const InvoicesPage());

    expect(find.text('No invoices found.'), findsOneWidget);
  });

  testWidgets('error state shows message and retry button', (tester) async {
    final api = FakeYmcaApi(
      me: meWith({'tenant:finance_reader'}),
      allInvoices: const [],
    );
    api.failCalls['invoices'] = const ApiException(
      500,
      'internal_error',
      'server error',
    );
    await pumpScreen(tester, api, const InvoicesPage());

    expect(
      find.text('Something went wrong. Please try again.'),
      findsOneWidget,
    );
    expect(find.byKey(const Key('retry')), findsOneWidget);
  });

  testWidgets('invoices show total and balance in subtitle', (tester) async {
    final api = FakeYmcaApi(
      me: meWith({'tenant:finance_reader'}),
      allInvoices: [
        invoiceOf(
          'inv-1',
          state: 'ISSUED',
          total: '5000.00',
          balance: '3000.00',
        ),
      ],
    );
    await pumpScreen(tester, api, const InvoicesPage());

    expect(find.textContaining('Total 5000.00 INR'), findsOneWidget);
    expect(find.textContaining('Balance 3000.00'), findsOneWidget);
  });

  testWidgets('state is shown in a chip', (tester) async {
    final api = FakeYmcaApi(
      me: meWith({'tenant:finance_reader'}),
      allInvoices: [
        invoiceOf('inv-1', state: 'ISSUED'),
        invoiceOf('inv-2', state: 'DRAFT'),
      ],
    );
    await pumpScreen(tester, api, const InvoicesPage());

    expect(find.byType(Chip), findsWidgets);
    expect(find.text('ISSUED'), findsOneWidget);
    expect(find.text('DRAFT'), findsOneWidget);
  });

  testWidgets('pull-to-refresh reloads the list', (tester) async {
    final api = FakeYmcaApi(
      me: meWith({'tenant:finance_reader'}),
      allInvoices: [invoiceOf('inv-1', state: 'ISSUED')],
    );
    await pumpScreen(tester, api, const InvoicesPage());

    expect(api.calls.where((c) => c.startsWith('invoices')).length, 1);

    await tester.drag(find.byType(RefreshIndicator), const Offset(0, 300));
    await tester.pumpAndSettle();

    expect(api.calls.where((c) => c.startsWith('invoices')).length, 2);
  });
}
