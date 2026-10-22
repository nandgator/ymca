import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../api/models.dart';
import '../api/ymca_api.dart';
import '../state/app_controller.dart';
import '../util/idempotency.dart';

/// What the member sheet reports back to the page that opened it.
enum MemberSheetResult {
  /// The staff member chose to record a meal for them.
  recordMeal,

  /// A suspension, reinstatement or termination went through.
  changed,
}

/// What can be done to a member from the directory: record a meal for them,
/// and — for an administrator — suspend, reinstate or terminate (A3.13).
/// Completes with what was chosen, or null if the sheet was dismissed.
///
/// Each action is gated on `tenant:admin` — the server says so in `/me` and
/// enforces it regardless — and each opens a confirmation that says, in plain
/// words, what it takes away: suspending and terminating end the member's
/// access to what the plan covers, immediately, on the server, before it
/// answers; terminating is final.
Future<MemberSheetResult?> showMemberSheet(
  BuildContext context,
  DirectoryEntry member,
) => showModalBottomSheet<MemberSheetResult>(
  context: context,
  builder: (sheetContext) => _ActionsSheet(member: member),
);

class _ActionsSheet extends StatelessWidget {
  const _ActionsSheet({required this.member});

  final DirectoryEntry member;

  @override
  Widget build(BuildContext context) {
    final admin = context.read<AppController>().me?.isAdmin ?? false;
    Future<void> run(Future<bool> Function() action) async {
      final navigator = Navigator.of(context);
      final changed = await action();
      navigator.pop(changed ? MemberSheetResult.changed : null);
    }

    return SafeArea(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 16, 16, 4),
            child: Text(
              member.displayName,
              style: Theme.of(context).textTheme.titleLarge,
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
            child: Text('No. ${member.number} · ${member.state}'),
          ),
          ListTile(
            key: const Key('record-for-member'),
            leading: const Icon(Icons.restaurant),
            title: const Text('Record a meal'),
            onTap: () =>
                Navigator.of(context).pop(MemberSheetResult.recordMeal),
          ),
          if (admin && member.state == 'ACTIVE')
            ListTile(
              key: const Key('suspend-member'),
              leading: const Icon(Icons.pause_circle_outline),
              title: const Text('Suspend…'),
              onTap: () => run(() => _suspend(context, member)),
            ),
          if (admin && member.state == 'SUSPENDED')
            ListTile(
              key: const Key('reinstate-member'),
              leading: const Icon(Icons.play_circle_outline),
              title: const Text('Reinstate'),
              onTap: () => run(() => _reinstate(context, member)),
            ),
          if (admin && member.state != 'TERMINATED')
            ListTile(
              key: const Key('terminate-member'),
              leading: const Icon(Icons.cancel_outlined),
              title: const Text('Terminate…'),
              onTap: () => run(() => _terminate(context, member)),
            ),
        ],
      ),
    );
  }
}

const _reasons = {
  'ARREARS': 'Arrears',
  'CONDUCT': 'Conduct',
  'SAFEGUARDING': 'Safeguarding',
  'ADMINISTRATIVE': 'Administrative',
};

Future<bool> _suspend(BuildContext context, DirectoryEntry member) async {
  final api = context.read<YmcaApi>();
  final controller = context.read<AppController>();
  return await showDialog<bool>(
        context: context,
        builder: (_) => _ConfirmDialog(
          title: 'Suspend ${member.displayName}?',
          body:
              'They lose access to what their plan covers straight away. '
              'They stay a member, and can be reinstated.',
          confirmLabel: 'Suspend',
          confirmKey: const Key('confirm-suspend'),
          controller: controller,
          withReason: true,
          act: (key, reason) => api.suspendMembership(
            member.membershipId,
            reason: reason!,
            idempotencyKey: key,
          ),
        ),
      ) ??
      false;
}

Future<bool> _reinstate(BuildContext context, DirectoryEntry member) async {
  final api = context.read<YmcaApi>();
  final controller = context.read<AppController>();
  return await showDialog<bool>(
        context: context,
        builder: (_) => _ConfirmDialog(
          title: 'Reinstate ${member.displayName}?',
          body:
              'Their coverage comes back within a few seconds, not instantly.',
          confirmLabel: 'Reinstate',
          confirmKey: const Key('confirm-reinstate'),
          controller: controller,
          act: (key, _) =>
              api.reinstateMembership(member.membershipId, idempotencyKey: key),
        ),
      ) ??
      false;
}

Future<bool> _terminate(BuildContext context, DirectoryEntry member) async {
  final api = context.read<YmcaApi>();
  final controller = context.read<AppController>();
  return await showDialog<bool>(
        context: context,
        builder: (_) => _ConfirmDialog(
          title: 'Terminate ${member.displayName}?',
          body:
              'Their membership ends for good: coverage is removed at once and '
              'their standing obligations stop after today. This cannot be '
              'undone — a returning member is admitted afresh.',
          confirmLabel: 'Terminate',
          confirmKey: const Key('confirm-terminate'),
          controller: controller,
          act: (key, _) =>
              api.terminateMembership(member.membershipId, idempotencyKey: key),
        ),
      ) ??
      false;
}

class _ConfirmDialog extends StatefulWidget {
  const _ConfirmDialog({
    required this.title,
    required this.body,
    required this.confirmLabel,
    required this.confirmKey,
    required this.controller,
    required this.act,
    this.withReason = false,
  });

  final String title;
  final String body;
  final String confirmLabel;
  final Key confirmKey;
  final AppController controller;
  final bool withReason;
  final Future<Membership> Function(String key, String? reason) act;

  @override
  State<_ConfirmDialog> createState() => _ConfirmDialogState();
}

class _ConfirmDialogState extends State<_ConfirmDialog> {
  String? _reason;
  String? _error;
  bool _busy = false;

  // One key per REQUEST: kept while the same confirmation is retried, replaced
  // if the reason changes (A3.6: the same key with a different body is refused).
  String? _key;
  String? _keyFor;

  Future<void> _confirm() async {
    final request = _reason ?? '';
    if (_keyFor != request) {
      _key = newIdempotencyKey();
      _keyFor = request;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    final navigator = Navigator.of(context);
    try {
      await widget.act(_key!, _reason);
      navigator.pop(true);
    } on ApiException catch (e) {
      if (await widget.controller.handleError(e)) {
        navigator.pop(false);
        return;
      }
      if (!mounted) return;
      setState(() {
        _error = e.userMessage;
        _busy = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _error = 'Something went wrong. Please try again.';
        _busy = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text(widget.title),
    content: Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(widget.body),
        if (widget.withReason) ...[
          const SizedBox(height: 16),
          DropdownButtonFormField<String>(
            key: const Key('suspend-reason'),
            initialValue: _reason,
            hint: const Text('Reason'),
            items: [
              for (final e in _reasons.entries)
                DropdownMenuItem(value: e.key, child: Text(e.value)),
            ],
            onChanged: (v) => setState(() => _reason = v),
          ),
        ],
        if (_error != null)
          Padding(
            padding: const EdgeInsets.only(top: 12),
            child: Text(
              _error!,
              key: const Key('confirm-error'),
              style: TextStyle(color: Theme.of(context).colorScheme.error),
            ),
          ),
      ],
    ),
    actions: [
      TextButton(
        onPressed: _busy ? null : () => Navigator.of(context).pop(false),
        child: const Text('Cancel'),
      ),
      FilledButton(
        key: widget.confirmKey,
        onPressed: _busy || (widget.withReason && _reason == null)
            ? null
            : _confirm,
        child: Text(widget.confirmLabel),
      ),
    ],
  );
}
