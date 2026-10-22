import 'package:flutter/material.dart';

/// STUB — replaced by the real screen.
class InvoiceDetailPage extends StatelessWidget {
  const InvoiceDetailPage({super.key, required this.invoiceId, this.staff = false});

  final String invoiceId;
  final bool staff;

  @override
  Widget build(BuildContext context) =>
      Scaffold(appBar: AppBar(), body: Center(child: Text(invoiceId)));
}
