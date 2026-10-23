import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../api/models.dart';
import '../api/ymca_api.dart';
import '../state/app_controller.dart';
import '../util/idempotency.dart';
import 'support.dart';

/// One invoice, with its charges, payments and credit notes. [staff] adds the
/// actions — issue a draft, record a payment, issue a credit note — and only
/// for a principal the server says holds tenant:finance_manager (A3.9).
class InvoiceDetailPage extends StatefulWidget {
  const InvoiceDetailPage({
    super.key,
    required this.invoiceId,
    this.staff = false,
  });

  final String invoiceId;
  final bool staff;

  @override
  State<InvoiceDetailPage> createState() => _InvoiceDetailPageState();
}

class _InvoiceDetailPageState extends State<InvoiceDetailPage> {
  Invoice? _invoice;
  String? _error;
  bool _loading = true;
  bool _busy = false;

  // One key per ISSUE request. Issuing the same invoice is the same request
  // however many times the button is pressed, so the key lives until it
  // succeeds — a double tap or a retry after a lost response is one issue.
  String? _issueKey;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    final api = context.read<YmcaApi>();
    final result = await attempt(context, () => api.invoice(widget.invoiceId));
    if (!mounted) return;
    setState(() {
      _invoice = result.value ?? _invoice;
      _error = result.value == null ? result.message : null;
      _loading = false;
    });
  }

  bool get _canManage =>
      widget.staff &&
      (context.read<AppController>().me?.canManageFinance ?? false);

  Future<void> _issue() async {
    final api = context.read<YmcaApi>();
    _issueKey ??= newIdempotencyKey();
    setState(() => _busy = true);
    final result = await attempt(
      context,
      () => api.issueInvoice(widget.invoiceId, idempotencyKey: _issueKey!),
    );
    if (!mounted) return;
    setState(() => _busy = false);
    if (result.value != null) {
      _issueKey = null;
      showFailure(context, 'Invoice issued');
      await _load();
    } else if (result.message != null) {
      showFailure(context, result.message!);
    }
  }

  Future<void> _pay(Invoice invoice) async {
    final paid = await showDialog<bool>(
      context: context,
      builder: (_) => _PaymentDialog(
        invoiceId: invoice.id,
        currency: invoice.currency,
        balance: invoice.balance ?? '',
        api: context.read<YmcaApi>(),
        controller: context.read<AppController>(),
      ),
    );
    if (paid == true && mounted) {
      showFailure(context, 'Payment recorded');
      await _load();
    }
  }

  Future<void> _credit(Invoice invoice) async {
    final credited = await showDialog<bool>(
      context: context,
      builder: (_) => _CreditDialog(
        invoiceId: invoice.id,
        currency: invoice.currency,
        api: context.read<YmcaApi>(),
        controller: context.read<AppController>(),
      ),
    );
    if (credited == true && mounted) {
      showFailure(context, 'Credit note issued');
      await _load();
    }
  }

  @override
  Widget build(BuildContext context) {
    final Widget body;
    final invoice = _invoice;
    if (invoice == null) {
      body = _loading
          ? const Center(child: CircularProgressIndicator())
          : ErrorRetry(message: _error ?? '', onRetry: _load);
    } else {
      body = RefreshIndicator(
        onRefresh: _load,
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            Text(
              invoice.number ?? 'Draft',
              style: Theme.of(context).textTheme.headlineSmall,
            ),
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              children: [Chip(label: Text(stateLabel(invoice.state)))],
            ),
            const SizedBox(height: 16),
            if (invoice.totalAmount != null)
              _line('Total', money(invoice.totalAmount!, invoice.currency)),
            if (invoice.balance != null && invoice.refundDue)
              _line(
                'Refund due',
                money(invoice.balance!.substring(1), invoice.currency),
              )
            else if (invoice.balance != null)
              _line('Balance', money(invoice.balance!, invoice.currency)),
            if (invoice.dueAt != null) _line('Due', dayOf(invoice.dueAt!)),
            if (invoice.charges.isNotEmpty) ...[
              const SizedBox(height: 16),
              Text('Charges', style: Theme.of(context).textTheme.titleMedium),
              for (final c in invoice.charges)
                Card(
                  child: Padding(
                    padding: const EdgeInsets.all(12),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        if (c.description != null) Text(c.description!),
                        for (final comp in c.components)
                          Text(
                            '${comp.kind}: ${money(comp.amount, comp.currency)}',
                          ),
                      ],
                    ),
                  ),
                ),
            ],
            if (invoice.payments.isNotEmpty) ...[
              const SizedBox(height: 16),
              Text('Payments', style: Theme.of(context).textTheme.titleMedium),
              for (final p in invoice.payments)
                ListTile(
                  dense: true,
                  title: Text('${p.method} ${money(p.amount, p.currency)}'),
                  subtitle: Text(dayOf(p.receivedAt)),
                ),
            ],
            if (invoice.creditNotes.isNotEmpty) ...[
              const SizedBox(height: 16),
              Text(
                'Credit notes',
                style: Theme.of(context).textTheme.titleMedium,
              ),
              for (final n in invoice.creditNotes)
                ListTile(
                  dense: true,
                  title: Text('${n.number} ${money(n.amount, n.currency)}'),
                  subtitle: Text(n.reason),
                ),
            ],
            if (_canManage) ...[
              const SizedBox(height: 24),
              if (invoice.isDraft)
                FilledButton(
                  key: const Key('issue-invoice'),
                  onPressed: _busy ? null : _issue,
                  child: const Text('Issue'),
                ),
              if (invoice.isIssued)
                FilledButton(
                  key: const Key('pay-invoice'),
                  onPressed: () => _pay(invoice),
                  child: const Text('Record payment'),
                ),
              if (invoice.canBeCredited)
                Padding(
                  padding: const EdgeInsets.only(top: 8),
                  child: OutlinedButton(
                    key: const Key('credit-invoice'),
                    onPressed: () => _credit(invoice),
                    child: const Text('Issue credit note'),
                  ),
                ),
            ],
          ],
        ),
      );
    }
    return Scaffold(
      appBar: AppBar(title: const Text('Invoice')),
      body: body,
    );
  }

  Widget _line(String label, String value) => Padding(
    padding: const EdgeInsets.symmetric(vertical: 2),
    child: Row(
      children: [
        SizedBox(width: 80, child: Text(label)),
        Text(value, style: const TextStyle(fontWeight: FontWeight.w600)),
      ],
    ),
  );
}

class _PaymentDialog extends StatefulWidget {
  const _PaymentDialog({
    required this.invoiceId,
    required this.currency,
    required this.balance,
    required this.api,
    required this.controller,
  });

  final String invoiceId;
  final String currency;
  final String balance;
  final YmcaApi api;
  final AppController controller;

  @override
  State<_PaymentDialog> createState() => _PaymentDialogState();
}

class _PaymentDialogState extends State<_PaymentDialog> {
  static const _methods = ['CASH', 'UPI', 'BANK_TRANSFER', 'CHEQUE'];

  late final TextEditingController _amount = TextEditingController(
    text: widget.balance,
  );
  String? _method;
  String? _error;
  bool _saving = false;

  // One key per payment REQUEST: kept while the same method and amount are
  // retried, replaced when either changes (the same key with a different body
  // is idempotency_key_reused, A3.6).
  String? _key;
  String? _keyFor;

  @override
  void dispose() {
    _amount.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    final method = _method;
    final amount = _amount.text.trim();
    if (method == null || amount.isEmpty) {
      setState(() => _error = 'Choose a method and enter an amount.');
      return;
    }
    final request = '$method|$amount';
    if (_keyFor != request) {
      _key = newIdempotencyKey();
      _keyFor = request;
    }
    setState(() {
      _saving = true;
      _error = null;
    });
    final navigator = Navigator.of(context);
    try {
      await widget.api.payInvoice(
        widget.invoiceId,
        method: method,
        amount: amount,
        currency: widget.currency,
        idempotencyKey: _key!,
      );
      navigator.pop(true);
    } on ApiException catch (e) {
      if (await widget.controller.handleError(e)) {
        navigator.pop(false);
        return;
      }
      if (!mounted) return;
      setState(() {
        _error = e.userMessage;
        _saving = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _error = 'Something went wrong. Please try again.';
        _saving = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('Record payment'),
    content: SingleChildScrollView(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          DropdownButtonFormField<String>(
            key: const Key('pay-method'),
            initialValue: _method,
            hint: const Text('Method'),
            items: [
              for (final m in _methods)
                DropdownMenuItem(value: m, child: Text(m)),
            ],
            onChanged: (v) => setState(() => _method = v),
          ),
          TextField(
            key: const Key('pay-amount'),
            controller: _amount,
            keyboardType: const TextInputType.numberWithOptions(decimal: true),
            decoration: InputDecoration(
              labelText: 'Amount (${widget.currency})',
            ),
          ),
          if (_error != null)
            Padding(
              padding: const EdgeInsets.only(top: 12),
              child: Text(
                _error!,
                key: const Key('pay-error'),
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ),
        ],
      ),
    ),
    actions: [
      TextButton(
        onPressed: _saving ? null : () => Navigator.of(context).pop(false),
        child: const Text('Cancel'),
      ),
      FilledButton(
        key: const Key('save-payment'),
        onPressed: _saving ? null : _save,
        child: const Text('Save payment'),
      ),
    ],
  );
}

class _CreditDialog extends StatefulWidget {
  const _CreditDialog({
    required this.invoiceId,
    required this.currency,
    required this.api,
    required this.controller,
  });

  final String invoiceId;
  final String currency;
  final YmcaApi api;
  final AppController controller;

  @override
  State<_CreditDialog> createState() => _CreditDialogState();
}

class _CreditDialogState extends State<_CreditDialog> {
  final TextEditingController _amount = TextEditingController();
  final TextEditingController _reason = TextEditingController();
  String? _error;
  bool _saving = false;

  // One key per credit REQUEST: kept while the same amount and reason are
  // retried, replaced when either changes (the same key with a different body
  // is idempotency_key_reused, A3.6).
  String? _key;
  String? _keyFor;

  @override
  void dispose() {
    _amount.dispose();
    _reason.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    final amount = _amount.text.trim();
    final reason = _reason.text.trim();
    if (amount.isEmpty || reason.isEmpty) {
      setState(() => _error = 'Enter an amount and a reason.');
      return;
    }
    final request = '$amount|$reason';
    if (_keyFor != request) {
      _key = newIdempotencyKey();
      _keyFor = request;
    }
    setState(() {
      _saving = true;
      _error = null;
    });
    final navigator = Navigator.of(context);
    try {
      await widget.api.creditInvoice(
        widget.invoiceId,
        amount: amount,
        reason: reason,
        idempotencyKey: _key!,
      );
      navigator.pop(true);
    } on ApiException catch (e) {
      if (await widget.controller.handleError(e)) {
        navigator.pop(false);
        return;
      }
      if (!mounted) return;
      setState(() {
        _error = e.userMessage;
        _saving = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _error = 'Something went wrong. Please try again.';
        _saving = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('Issue credit note'),
    content: SingleChildScrollView(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          TextField(
            key: const Key('credit-amount'),
            controller: _amount,
            keyboardType: const TextInputType.numberWithOptions(decimal: true),
            decoration: InputDecoration(
              labelText: 'Amount (${widget.currency})',
            ),
          ),
          TextField(
            key: const Key('credit-reason'),
            controller: _reason,
            maxLength: 500,
            decoration: const InputDecoration(labelText: 'Reason'),
          ),
          if (_error != null)
            Padding(
              padding: const EdgeInsets.only(top: 12),
              child: Text(
                _error!,
                key: const Key('credit-error'),
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ),
        ],
      ),
    ),
    actions: [
      TextButton(
        onPressed: _saving ? null : () => Navigator.of(context).pop(false),
        child: const Text('Cancel'),
      ),
      FilledButton(
        key: const Key('save-credit'),
        onPressed: _saving ? null : _save,
        child: const Text('Issue'),
      ),
    ],
  );
}
