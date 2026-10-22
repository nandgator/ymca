import 'package:flutter_test/flutter_test.dart';
import 'package:ymca_app/ui/bills_page.dart';
import 'package:ymca_app/ui/invoice_detail_page.dart';

import 'fake_ymca_api.dart';
import 'support.dart';

void main() {
  testWidgets('lists the invoices with their state and amounts', (
    tester,
  ) async {
    final api = FakeYmcaApi(
      me: meWith({}),
      myInvoiceList: [
        invoiceOf('i1', balance: '1500.00'),
        invoiceOf(
          'i2',
          total: '15.00',
          balance: '0.00',
          state: 'PAID',
          number: 'INV/2026-27/000002',
        ),
      ],
    );
    await pumpScreen(tester, api, const BillsPage());

    expect(find.text('INV/2026-27/000001'), findsOneWidget);
    expect(
      find.text('Total 2000.00 INR · Balance 1500.00 INR'),
      findsOneWidget,
    );
    expect(find.text('Issued'), findsOneWidget);
    expect(find.text('Paid'), findsOneWidget);
  });

  testWidgets('an empty list says so', (tester) async {
    await pumpScreen(tester, FakeYmcaApi(me: meWith({})), const BillsPage());
    expect(find.text('No bills yet.'), findsOneWidget);
  });

  testWidgets('tapping a bill opens it', (tester) async {
    final api = FakeYmcaApi(me: meWith({}), myInvoiceList: [invoiceOf('i1')]);
    await pumpScreen(tester, api, const BillsPage());

    await tester.tap(find.text('INV/2026-27/000001'));
    await tester.pumpAndSettle();

    expect(find.byType(InvoiceDetailPage), findsOneWidget);
    expect(api.calls, contains('invoice id=i1'));
  });
}
