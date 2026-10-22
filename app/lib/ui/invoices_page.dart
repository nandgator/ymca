import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../api/models.dart' as api_models;
import '../api/ymca_api.dart';
import '../state/app_controller.dart';
import 'invoice_detail_page.dart';

class InvoicesPage extends StatefulWidget {
  const InvoicesPage({super.key});

  @override
  State<InvoicesPage> createState() => _InvoicesPageState();
}

class _InvoicesPageState extends State<InvoicesPage> {
  String? _cursor;
  List<api_models.Invoice> _invoices = [];
  String? _error;
  bool _loading = true;
  String _filter = 'all';

  @override
  void initState() {
    super.initState();
    _loadInvoices();
  }

  Future<void> _loadInvoices({String? cursor}) async {
    try {
      setState(() {
        _loading = true;
        _error = null;
      });

      final page = await context.read<YmcaApi>().invoices(cursor: cursor);

      if (!mounted) return;
      setState(() {
        if (cursor == null) {
          _invoices = page.items;
        } else {
          _invoices.addAll(page.items);
        }
        _cursor = page.nextCursor;
        _loading = false;
      });
    } on ApiException catch (e) {
      if (!mounted) return;
      final signedOut = await context.read<AppController>().handleError(e);
      if (!mounted) return;
      if (!signedOut) {
        setState(() {
          _error = e.userMessage;
          _loading = false;
        });
        if (_invoices.isNotEmpty) {
          // A failed "load more" leaves the list on screen; say so.
          ScaffoldMessenger.of(context)
              .showSnackBar(SnackBar(content: Text(e.userMessage)));
        }
      }
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _error = 'Something went wrong. Please try again.';
        _loading = false;
      });
    }
  }

  Future<void> _refresh() async {
    setState(() {
      _cursor = null;
      _invoices = [];
    });
    return _loadInvoices();
  }

  void _loadMore() {
    if (_cursor != null && _cursor!.isNotEmpty) {
      _loadInvoices(cursor: _cursor);
    }
  }

  List<api_models.Invoice> get _filtered {
    if (_filter == 'all') return _invoices;
    if (_filter == 'draft') return _invoices.where((i) => i.isDraft).toList();
    if (_filter == 'issued') return _invoices.where((i) => i.isIssued).toList();
    if (_filter == 'paid') return _invoices.where((i) => i.isPaid).toList();
    return _invoices;
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        SingleChildScrollView(
          scrollDirection: Axis.horizontal,
          child: Padding(
            padding: const EdgeInsets.all(8.0),
            child: Row(
              children: [
                FilterChip(
                  key: const Key('filter-all'),
                  label: const Text('All'),
                  selected: _filter == 'all',
                  onSelected: (_) {
                    setState(() => _filter = 'all');
                  },
                ),
                const SizedBox(width: 8),
                FilterChip(
                  key: const Key('filter-draft'),
                  label: const Text('Draft'),
                  selected: _filter == 'draft',
                  onSelected: (_) {
                    setState(() => _filter = 'draft');
                  },
                ),
                const SizedBox(width: 8),
                FilterChip(
                  key: const Key('filter-issued'),
                  label: const Text('Issued'),
                  selected: _filter == 'issued',
                  onSelected: (_) {
                    setState(() => _filter = 'issued');
                  },
                ),
                const SizedBox(width: 8),
                FilterChip(
                  key: const Key('filter-paid'),
                  label: const Text('Paid'),
                  selected: _filter == 'paid',
                  onSelected: (_) {
                    setState(() => _filter = 'paid');
                  },
                ),
              ],
            ),
          ),
        ),
        Expanded(child: _buildContent()),
      ],
    );
  }

  Widget _buildContent() {
    if (_loading && _invoices.isEmpty) {
      return const Center(child: CircularProgressIndicator());
    }

    if (_error != null && _invoices.isEmpty) {
      return Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Text(_error!),
            const SizedBox(height: 16),
            ElevatedButton(
              key: const Key('retry'),
              onPressed: () => _loadInvoices(),
              child: const Text('Retry'),
            ),
          ],
        ),
      );
    }

    final filtered = _filtered;

    if (filtered.isEmpty) {
      return Center(
        child: Text(
          _filter == 'all'
              ? 'No invoices found.'
              : 'No invoices in this state.',
        ),
      );
    }

    // The indicator wraps the list and not the page: it needs a scrollable
    // child, and a column of chips above a list is not one.
    return RefreshIndicator(
      onRefresh: _refresh,
      child: ListView.builder(
        itemCount:
            filtered.length +
            ((_cursor != null && _cursor!.isNotEmpty) ? 1 : 0),
        itemBuilder: (context, index) {
          if (index == filtered.length) {
            return Padding(
              padding: const EdgeInsets.all(16.0),
              child: ElevatedButton(
                onPressed: _loadMore,
                child: const Text('Load more'),
              ),
            );
          }

          final invoice = filtered[index];
          final subtitle = _buildSubtitle(invoice);

          return ListTile(
            title: Text(invoice.number ?? 'Draft invoice'),
            subtitle: Text(subtitle),
            trailing: Chip(label: Text(invoice.state)),
            onTap: () async {
              await Navigator.of(context).push<void>(
                MaterialPageRoute(
                  builder: (context) =>
                      InvoiceDetailPage(invoiceId: invoice.id, staff: true),
                ),
              );
              // Reload the list when the route pops
              if (mounted) {
                await _refresh();
              }
            },
          );
        },
      ),
    );
  }

  String _buildSubtitle(api_models.Invoice invoice) {
    final parts = <String>[];

    if (invoice.isDraft) {
      parts.add('Not yet issued');
    } else {
      if (invoice.totalAmount != null) {
        parts.add('Total ${invoice.totalAmount} ${invoice.currency}');
      }
      if (invoice.balance != null) {
        parts.add('Balance ${invoice.balance}');
      }
    }

    return parts.join(' · ');
  }
}
