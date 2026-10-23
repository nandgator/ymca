import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../api/models.dart' as api_models;
import '../api/ymca_api.dart';
import '../state/app_controller.dart';
import '../util/dates.dart';
import '../util/idempotency.dart';
import 'support.dart';

class ClosePage extends StatefulWidget {
  const ClosePage({super.key, this.now});

  /// The clock, for tests. The real app reads the device's.
  final DateTime Function()? now;

  @override
  State<ClosePage> createState() => _ClosePageState();
}

class _ClosePageState extends State<ClosePage> {
  late Future<api_models.Paged<api_models.ConsumptionType>> _typesFuture;
  List<api_models.ConsumptionType> _types = [];
  String? _error;
  String? _loadError;
  bool _typesLoading = true;

  api_models.ConsumptionType? _selectedType;
  late DateTime _fromDate;
  late DateTime _toDate;
  // One key per REQUEST: it is reused while the user retries the same close
  // and replaced the moment the type or the dates change, because the same key
  // with a different body is idempotency_key_reused (A3.6) — the server would
  // be right to refuse it.
  String? _idempotencyKey;
  String? _keyFor;
  List<api_models.ClosedPeriod>? _result;
  bool _loading = false;
  final _fromCtl = TextEditingController();
  final _toCtl = TextEditingController();

  @override
  void dispose() {
    _fromCtl.dispose();
    _toCtl.dispose();
    super.dispose();
  }

  @override
  void initState() {
    super.initState();
    _typesFuture = context.read<YmcaApi>().consumptionTypes();
    _initializeDates();
    _loadTypes();
  }

  void _initializeDates() {
    final now = calendarOf(context).wallClock(widget.now?.call());
    final previousMonthEnd = DateTime.utc(
      now.year,
      now.month,
      1,
    ).subtract(const Duration(days: 1));
    _toDate = previousMonthEnd;
    _fromDate = DateTime.utc(previousMonthEnd.year, previousMonthEnd.month, 1);
    _fromCtl.text = isoDay(_fromDate);
    _toCtl.text = isoDay(_toDate);
  }

  Future<void> _loadTypes() async {
    setState(() {
      _typesLoading = true;
      _loadError = null;
    });
    try {
      final page = await _typesFuture;
      if (!mounted) return;
      setState(() {
        _types = page.items.where((t) => t.status == 'ACTIVE').toList();
        _typesLoading = false;
      });
    } on ApiException catch (e) {
      if (!mounted) return;
      final signedOut = await context.read<AppController>().handleError(e);
      if (!mounted) return;
      if (!signedOut) {
        setState(() {
          _loadError = e.userMessage;
          _typesLoading = false;
        });
      }
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _loadError = 'Something went wrong. Please try again.';
        _typesLoading = false;
      });
    }
  }

  void _retryLoad() {
    _typesFuture = context.read<YmcaApi>().consumptionTypes();
    _loadTypes();
  }

  void _another() => setState(() {
    _result = null;
    _error = null;
  });

  Future<void> _pickFromDate() async {
    final picked = await showDatePicker(
      context: context,
      initialDate: _fromDate,
      firstDate: DateTime.utc(2020, 1, 1),
      lastDate: _toDate,
    );
    if (picked != null) {
      setState(() {
        _fromDate = pickedDay(picked);
        _fromCtl.text = isoDay(_fromDate);
      });
    }
  }

  Future<void> _pickToDate() async {
    final picked = await showDatePicker(
      context: context,
      initialDate: _toDate,
      firstDate: _fromDate,
      lastDate: calendarOf(context).wallClock(widget.now?.call()),
    );
    if (picked != null) {
      setState(() {
        _toDate = pickedDay(picked);
        _toCtl.text = isoDay(_toDate);
      });
    }
  }

  Future<void> _showConfirmDialog() async {
    if (_selectedType == null) return;

    final request =
        '${_selectedType!.id}|${isoDay(_fromDate)}|${isoDay(_toDate)}';
    if (_keyFor != request) {
      _idempotencyKey = newIdempotencyKey();
      _keyFor = request;
    }

    if (!mounted) return;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Confirm close period'),
        content: Text(
          'Close ${_selectedType!.name} from ${isoDay(_fromDate)} to ${isoDay(_toDate)}? '
          "Everyone's units for these days are billed and the period cannot be reopened.",
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            key: const Key('close-confirm'),
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('Close period'),
          ),
        ],
      ),
    );

    if (confirmed != true || !mounted) return;

    await _closePeriod();
  }

  Future<void> _closePeriod() async {
    if (_selectedType == null || _idempotencyKey == null) return;

    setState(() {
      _loading = true;
      _error = null;
    });

    try {
      final result = await context.read<YmcaApi>().closePeriod(
        _selectedType!.id,
        from: isoDay(_fromDate),
        to: isoDay(_toDate),
        idempotencyKey: _idempotencyKey!,
      );

      if (!mounted) return;
      setState(() {
        _result = result;
        _loading = false;
        // Done: the next close, even of the same range, is a new request.
        _idempotencyKey = null;
        _keyFor = null;
      });
    } on ApiException catch (e) {
      if (!mounted) return;
      final signedOut = await context.read<AppController>().handleError(e);
      if (!mounted) return;
      if (!signedOut) {
        final message = switch (e.code) {
          'no_rate' => 'No price is set for this item yet. Ask an administrator to set a rate, then try again.',
          'period_already_closed' => 'Part of that period is already closed.',
          _ => e.userMessage,
        };
        setState(() {
          _error = message;
          _loading = false;
        });
      }
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _error = 'Something went wrong. Please try again.';
        _loading = false;
      });
    }
  }

  bool get _canSubmit =>
      _selectedType != null &&
      _fromDate.isBefore(_toDate.add(const Duration(days: 1)));

  @override
  Widget build(BuildContext context) {
    if (_result != null) {
      return SingleChildScrollView(
        child: Padding(
          padding: const EdgeInsets.all(16.0),
          child: Card(
            key: const Key('close-result'),
            child: Padding(
              padding: const EdgeInsets.all(16.0),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    'Closed ${_result!.length} ${_result!.length == 1 ? 'person' : 'people'}',
                    style: Theme.of(context).textTheme.titleLarge,
                  ),
                  const SizedBox(height: 16),
                  for (final period in _result!)
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 4.0),
                      child: Text(
                        '${period.subjectPersonId.substring(0, 8)} — ${period.obligatedUnits} units'
                        '${period.invoiceId != null ? ' · invoice created' : ' · nothing to bill'}',
                      ),
                    ),
                  const SizedBox(height: 12),
                  TextButton(
                    key: const Key('close-another'),
                    onPressed: _another,
                    child: const Text('Close another period'),
                  ),
                ],
              ),
            ),
          ),
        ),
      );
    }

    if (_typesLoading) {
      return const Center(child: CircularProgressIndicator());
    }
    if (_loadError != null) {
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(_loadError!),
            const SizedBox(height: 12),
            FilledButton(
              key: const Key('retry'),
              onPressed: _retryLoad,
              child: const Text('Retry'),
            ),
          ],
        ),
      );
    }
    if (_types.isEmpty) {
      return const Center(
        child: Text('There is nothing to close yet: no active items.'),
      );
    }

    return SingleChildScrollView(
      child: Padding(
        padding: const EdgeInsets.all(16.0),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            DropdownButtonFormField<api_models.ConsumptionType>(
              key: const Key('close-type'),
              initialValue: _selectedType,
              decoration: const InputDecoration(labelText: 'Consumption type'),
              items: _types
                  .map((t) => DropdownMenuItem(value: t, child: Text(t.name)))
                  .toList(),
              onChanged: (type) {
                setState(() {
                  _selectedType = type;
                });
              },
            ),
            const SizedBox(height: 16),
            TextField(
              key: const Key('close-from'),
              readOnly: true,
              decoration: const InputDecoration(labelText: 'From'),
              controller: _fromCtl,
              onTap: _pickFromDate,
            ),
            const SizedBox(height: 16),
            TextField(
              key: const Key('close-to'),
              readOnly: true,
              decoration: const InputDecoration(labelText: 'To'),
              controller: _toCtl,
              onTap: _pickToDate,
            ),
            const SizedBox(height: 24),
            if (_error != null)
              Padding(
                padding: const EdgeInsets.only(bottom: 16.0),
                child: Text(
                  _error!,
                  key: const Key('close-error'),
                  style: TextStyle(color: Theme.of(context).colorScheme.error),
                ),
              ),
            FilledButton(
              key: const Key('close-submit'),
              onPressed: _canSubmit && !_loading ? _showConfirmDialog : null,
              child: _loading
                  ? const SizedBox(
                      height: 20,
                      width: 20,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Text('Close period'),
            ),
          ],
        ),
      ),
    );
  }
}
