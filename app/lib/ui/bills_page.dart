import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../api/models.dart';
import '../api/ymca_api.dart';
import 'invoice_detail_page.dart';
import 'support.dart';

/// The signed-in person's own invoices (A3.11): issued and paid ones only —
/// the server never lists a draft to its payer.
class BillsPage extends StatelessWidget {
  const BillsPage({super.key});

  @override
  Widget build(BuildContext context) {
    final api = context.read<YmcaApi>();
    return PagedList<Invoice>(
      fetch: (cursor) => api.myInvoices(cursor: cursor),
      emptyText: 'No bills yet.',
      itemBuilder: (context, invoice) => ListTile(
        title: Text(invoice.number ?? 'Invoice'),
        subtitle: Text(
          [
            if (invoice.totalAmount != null)
              'Total ${money(invoice.totalAmount!, invoice.currency)}',
            if (invoice.balance != null)
              'Balance ${money(invoice.balance!, invoice.currency)}',
          ].join(' · '),
        ),
        trailing: Chip(label: Text(stateLabel(invoice.state))),
        onTap: () => Navigator.of(context).push<void>(
          MaterialPageRoute(
            builder: (_) => InvoiceDetailPage(invoiceId: invoice.id),
          ),
        ),
      ),
    );
  }
}
