import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../api/models.dart';
import '../api/ymca_api.dart';
import '../state/app_controller.dart';
import '../util/dates.dart';
import '../util/idempotency.dart';

/// Records one meal for somebody else (staff recording on a member's behalf,
/// A3.10). Completes when the sheet is dismissed.
Future<void> showRecordForMemberSheet(
  BuildContext context,
  DirectoryEntry member,
) => showModalBottomSheet<void>(
  context: context,
  isScrollControlled: true,
  builder: (context) => _RecordForMemberSheet(member: member),
);

class _RecordForMemberSheet extends StatefulWidget {
  const _RecordForMemberSheet({required this.member});

  final DirectoryEntry member;

  @override
  State<_RecordForMemberSheet> createState() => _RecordForMemberSheetState();
}

class _RecordForMemberSheetState extends State<_RecordForMemberSheet> {
  List<ConsumptionType> _types = [];
  String? _selectedTypeId;
  int _dayOffset = 0;
  bool _loading = true;
  bool _saving = false;

  // Loading the types and saving a meal fail differently: a failed load leaves
  // nothing to show, a failed save must leave the form on screen so Save can
  // be pressed again.
  String? _loadError;
  String? _saveError;

  // One key per REQUEST. It is kept across a retry of the same type and day,
  // and replaced when either changes, because the same key with a different
  // body is idempotency_key_reused (A3.6).
  String? _key;
  String? _keyFor;

  @override
  void initState() {
    super.initState();
    _loadTypes();
  }

  Future<void> _loadTypes() async {
    setState(() {
      _loading = true;
      _loadError = null;
    });
    try {
      final page = await context.read<YmcaApi>().myRecordableTypes();
      if (!mounted) return;
      final staffTypes = page.items.where((t) => t.allowsStaff).toList();
      setState(() {
        _types = staffTypes;
        _selectedTypeId = staffTypes.isEmpty ? null : staffTypes.first.id;
        _loading = false;
      });
    } on ApiException catch (e) {
      if (!mounted) return;
      final signedOut = await context.read<AppController>().handleError(e);
      if (!mounted || signedOut) return;
      setState(() {
        _loadError = e.userMessage;
        _loading = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _loadError = 'Something went wrong. Please try again.';
        _loading = false;
      });
    }
  }

  Future<void> _save() async {
    final typeId = _selectedTypeId;
    if (typeId == null) return;
    final day = daysAgo(_dayOffset);
    final request = '$typeId|$day|${widget.member.personId}';
    if (_keyFor != request) {
      _key = newIdempotencyKey();
      _keyFor = request;
    }

    setState(() {
      _saveError = null;
      _saving = true;
    });
    final messenger = ScaffoldMessenger.of(context);
    final navigator = Navigator.of(context);
    try {
      await context.read<YmcaApi>().recordUse(
        typeId,
        occurredOn: day,
        subjectPersonId: widget.member.personId,
        idempotencyKey: _key!,
      );
      messenger.showSnackBar(
        SnackBar(content: Text('Recorded for ${widget.member.displayName}')),
      );
      navigator.pop();
    } on ApiException catch (e) {
      if (!mounted) return;
      final signedOut = await context.read<AppController>().handleError(e);
      if (!mounted || signedOut) return;
      setState(() {
        _saveError = switch (e.code) {
          'already_recorded' => 'That day is already recorded for them.',
          // 403 here is either the caller's authority or the member's
          // entitlement, and the server deliberately does not say which.
          'forbidden' =>
            "You can't record that for them, or they aren't entitled to it.",
          _ => e.userMessage,
        };
        _saving = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _saveError = 'Something went wrong. Please try again.';
        _saving = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: EdgeInsets.fromLTRB(
        16,
        16,
        16,
        16 + MediaQuery.of(context).viewInsets.bottom,
      ),
      child: SingleChildScrollView(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              'Record a meal for ${widget.member.displayName}',
              style: Theme.of(context).textTheme.titleLarge,
            ),
            const SizedBox(height: 24),
            if (_loading)
              const Center(child: CircularProgressIndicator())
            else if (_loadError != null) ...[
              Text(
                _loadError!,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
              const SizedBox(height: 12),
              FilledButton(
                key: const Key('rfm-retry'),
                onPressed: _loadTypes,
                child: const Text('Retry'),
              ),
            ] else if (_types.isEmpty)
              const Text('There is nothing you can record for a member yet.')
            else ...[
              DropdownButtonFormField<String>(
                key: const Key('rfm-type'),
                initialValue: _selectedTypeId,
                decoration: const InputDecoration(
                  labelText: 'Type',
                  border: OutlineInputBorder(),
                ),
                items: [
                  for (final type in _types)
                    DropdownMenuItem(value: type.id, child: Text(type.name)),
                ],
                onChanged: (value) => setState(() => _selectedTypeId = value),
              ),
              const SizedBox(height: 16),
              Wrap(
                spacing: 8,
                children: [
                  for (var i = 0; i < 3; i++)
                    ChoiceChip(
                      key: Key('rfm-day-$i'),
                      label: Text(
                        i == 0
                            ? 'Today'
                            : i == 1
                            ? 'Yesterday'
                            : '$i days ago',
                      ),
                      selected: _dayOffset == i,
                      onSelected: (selected) {
                        if (selected) setState(() => _dayOffset = i);
                      },
                    ),
                ],
              ),
              if (_saveError != null) ...[
                const SizedBox(height: 12),
                Text(
                  _saveError!,
                  key: const Key('rfm-error'),
                  style: TextStyle(color: Theme.of(context).colorScheme.error),
                ),
              ],
            ],
            const SizedBox(height: 24),
            SizedBox(
              width: double.infinity,
              child: FilledButton(
                key: const Key('rfm-save'),
                onPressed: (_types.isEmpty || _saving || _loading)
                    ? null
                    : _save,
                child: _saving
                    ? const SizedBox(
                        height: 24,
                        width: 24,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Text('Save'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
