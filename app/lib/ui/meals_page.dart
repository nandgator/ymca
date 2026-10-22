import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../api/models.dart';
import '../api/ymca_api.dart';
import '../util/dates.dart';
import '../util/idempotency.dart';
import 'support.dart';

/// The signed-in person's own meals: what they may record, and what they have.
///
/// "What they may record" is `GET /me/consumption-types` (A3.12) — the types
/// the server says this person is entitled to, not a list the client guessed.
class MealsPage extends StatefulWidget {
  const MealsPage({super.key});

  @override
  State<MealsPage> createState() => _MealsPageState();
}

class _MealsPageState extends State<MealsPage> {
  List<ConsumptionType> _types = [];
  final List<ConsumptionRecord> _history = [];
  String? _cursor;
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _loadAll();
  }

  Future<void> _loadAll() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    final api = context.read<YmcaApi>();
    final types = await attempt(context, api.myRecordableTypes);
    if (!mounted) return;
    final history = types.value == null
        ? null
        : await attempt(context, () => api.myConsumption());
    if (!mounted) return;
    setState(() {
      _loading = false;
      if (types.value == null || history?.value == null) {
        _error = types.message ?? history?.message;
        return;
      }
      _types = types.value!.items;
      _history
        ..clear()
        ..addAll(history!.value!.items);
      _cursor = history.value!.hasMore ? history.value!.nextCursor : null;
    });
  }

  Future<void> _loadMore() async {
    final api = context.read<YmcaApi>();
    final more = await attempt(
      context,
      () => api.myConsumption(cursor: _cursor),
    );
    if (!mounted) return;
    if (more.value == null) {
      if (more.message != null) showFailure(context, more.message!);
      return;
    }
    setState(() {
      _history.addAll(more.value!.items);
      _cursor = more.value!.hasMore ? more.value!.nextCursor : null;
    });
  }

  Future<void> _record(ConsumptionType type) async {
    final saved = await showModalBottomSheet<bool>(
      context: context,
      isScrollControlled: true,
      builder: (_) =>
          _RecordMealSheet(type: type, api: context.read<YmcaApi>()),
    );
    if (saved == true && mounted) {
      showFailure(context, 'Recorded');
      await _loadAll();
    }
  }

  String _nameOf(String typeId) => _types
      .firstWhere(
        (t) => t.id == typeId,
        orElse: () => ConsumptionType(
          id: typeId,
          name: 'Meal',
          obligates: false,
          recurrence: null,
          recordMode: 'EITHER',
          status: 'ACTIVE',
          correctionWindowDays: null,
        ),
      )
      .name;

  @override
  Widget build(BuildContext context) {
    if (_loading && _types.isEmpty && _history.isEmpty) {
      return const Center(child: CircularProgressIndicator());
    }
    if (_error != null) return ErrorRetry(message: _error!, onRetry: _loadAll);

    final mine = _types.where((t) => t.allowsSelf).toList();
    final live = _history.where((r) => !r.voided).toList();
    return RefreshIndicator(
      onRefresh: _loadAll,
      child: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          Text('Record a meal', style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 8),
          if (mine.isEmpty)
            const Padding(
              padding: EdgeInsets.symmetric(vertical: 8),
              child: Text('There is nothing you can record yet.'),
            )
          else
            for (final type in mine)
              Card(
                child: ListTile(
                  title: Text(type.name),
                  trailing: FilledButton(
                    key: Key('record-${type.id}'),
                    onPressed: () => _record(type),
                    child: const Text('Record'),
                  ),
                ),
              ),
          const SizedBox(height: 24),
          Text('My history', style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 8),
          if (live.isEmpty)
            const Padding(
              padding: EdgeInsets.symmetric(vertical: 8),
              child: Text('No meals recorded yet.'),
            )
          else
            for (final r in live)
              ListTile(
                title: Text(_nameOf(r.typeId)),
                subtitle: Text(
                  [
                    r.occurredOn,
                    if (r.optionCodes.isNotEmpty) r.optionCodes.join(', '),
                  ].join(' · '),
                ),
              ),
          if (_cursor != null)
            Center(
              child: OutlinedButton(
                key: const Key('load-more'),
                onPressed: _loadMore,
                child: const Text('Load more'),
              ),
            ),
        ],
      ),
    );
  }
}

class _RecordMealSheet extends StatefulWidget {
  const _RecordMealSheet({required this.type, required this.api});

  final ConsumptionType type;
  final YmcaApi api;

  @override
  State<_RecordMealSheet> createState() => _RecordMealSheetState();
}

class _RecordMealSheetState extends State<_RecordMealSheet> {
  List<ConsumptionOption> _options = [];
  bool _loadingOptions = true;
  int _day = 0;
  final Set<String> _chosen = {};
  bool _saving = false;
  String? _error;

  // One key per REQUEST: kept while the same day and options are retried and
  // replaced when either changes — the same key with a different body is
  // idempotency_key_reused (A3.6), which is what a user who first hit
  // "already recorded" and then picked another day would otherwise meet.
  String? _key;
  String? _keyFor;

  @override
  void initState() {
    super.initState();
    _loadOptions();
  }

  Future<void> _loadOptions() async {
    final result = await attempt(
      context,
      () => widget.api.options(widget.type.id),
    );
    if (!mounted) return;
    setState(() {
      _options = result.value?.items ?? [];
      _loadingOptions = false;
    });
  }

  Future<void> _save() async {
    final day = daysAgo(_day);
    final codes = _chosen.toList()..sort();
    final request = '$day|${codes.join(",")}';
    if (_keyFor != request) {
      _key = newIdempotencyKey();
      _keyFor = request;
    }
    setState(() {
      _saving = true;
      _error = null;
    });
    final navigator = Navigator.of(context);
    final result = await attempt(
      context,
      () => widget.api.recordUse(
        widget.type.id,
        occurredOn: day,
        optionCodes: codes,
        idempotencyKey: _key!,
      ),
    );
    if (result.value != null) {
      navigator.pop(true);
      return;
    }
    if (!mounted) return;
    setState(() {
      _saving = false;
      _error =
          result.message == 'That day is already recorded. Correct it instead.'
          ? 'You already recorded that day.'
          : result.message;
    });
  }

  @override
  Widget build(BuildContext context) => Padding(
    padding: EdgeInsets.fromLTRB(
      16,
      16,
      16,
      16 + MediaQuery.of(context).viewInsets.bottom,
    ),
    child: SingleChildScrollView(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'Record ${widget.type.name}',
            style: Theme.of(context).textTheme.titleLarge,
          ),
          const SizedBox(height: 16),
          Wrap(
            spacing: 8,
            children: [
              for (var i = 0; i < 3; i++)
                ChoiceChip(
                  key: Key('day-$i'),
                  label: Text(
                    i == 0
                        ? 'Today'
                        : i == 1
                        ? 'Yesterday'
                        : '$i days ago',
                  ),
                  selected: _day == i,
                  onSelected: (_) => setState(() => _day = i),
                ),
            ],
          ),
          const SizedBox(height: 16),
          if (_loadingOptions)
            const LinearProgressIndicator()
          else if (_options.isNotEmpty)
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final o in _options)
                  FilterChip(
                    key: Key('option-${o.code}'),
                    label: Text('${o.name} +${money(o.amount, o.currency)}'),
                    selected: _chosen.contains(o.code),
                    onSelected: (on) => setState(() {
                      on ? _chosen.add(o.code) : _chosen.remove(o.code);
                    }),
                  ),
              ],
            ),
          if (_error != null) ...[
            const SizedBox(height: 12),
            Text(
              _error!,
              key: const Key('meal-error'),
              style: TextStyle(color: Theme.of(context).colorScheme.error),
            ),
          ],
          const SizedBox(height: 16),
          SizedBox(
            width: double.infinity,
            child: FilledButton(
              key: const Key('save-meal'),
              onPressed: _saving ? null : _save,
              child: const Text('Save'),
            ),
          ),
        ],
      ),
    ),
  );
}
