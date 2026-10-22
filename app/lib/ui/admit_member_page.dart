import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../api/models.dart';
import '../api/ymca_api.dart';
import '../state/app_controller.dart';
import '../util/idempotency.dart';

class AdmitMemberPage extends StatefulWidget {
  const AdmitMemberPage({super.key});

  @override
  State<AdmitMemberPage> createState() => _AdmitMemberPageState();
}

class _AdmitMemberPageState extends State<AdmitMemberPage> {
  late TextEditingController _nameController;
  late TextEditingController _numberController;

  String? _selectedPlanId;
  List<MembershipPlan> _plans = [];
  bool _isLoading = false;
  bool _isSubmitting = false;
  String? _error;
  String? _numberError;

  // The Person registerPerson returned, and the name it was registered under.
  // A retry of a failed admit must not register them a second time; but if the
  // name has changed since, it is a different person and is registered afresh.
  Person? _registeredPerson;
  String? _registeredAs;

  // One key per admit REQUEST: kept across a retry of the same person, plan
  // and number, replaced when any changes — the same key with a different
  // body is idempotency_key_reused (A3.6). The user fixing a taken number is
  // exactly that case.
  String? _admitKey;
  String? _admitKeyFor;

  @override
  void initState() {
    super.initState();
    _nameController = TextEditingController();
    _numberController = TextEditingController();
    _loadPlans();
  }

  @override
  void dispose() {
    _nameController.dispose();
    _numberController.dispose();
    super.dispose();
  }

  Future<void> _loadPlans() async {
    if (!mounted) return;
    setState(() => _isLoading = true);
    try {
      final api = context.read<YmcaApi>();
      final page = await api.plans();
      if (!mounted) return;
      setState(() {
        _plans = page.items;
        _isLoading = false;
      });
    } on ApiException catch (e) {
      if (!mounted) return;
      final signedOut = await context.read<AppController>().handleError(e);
      if (!mounted) return;
      if (!signedOut) {
        setState(() {
          _error = e.userMessage;
          _isLoading = false;
        });
      }
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _error = 'Something went wrong. Please try again.';
        _isLoading = false;
      });
    }
  }

  Future<void> _submit() async {
    if (!mounted) return;
    setState(() {
      _error = null;
      _numberError = null;
      _isSubmitting = true;
    });

    try {
      final api = context.read<YmcaApi>();

      final name = _nameController.text.trim();
      if (_registeredPerson == null || _registeredAs != name) {
        _registeredPerson = await api.registerPerson(displayName: name);
        _registeredAs = name;
        if (!mounted) return;
      }

      final request =
          '${_registeredPerson!.id}|$_selectedPlanId|${_numberController.text}';
      if (_admitKeyFor != request) {
        _admitKey = newIdempotencyKey();
        _admitKeyFor = request;
      }

      // Admit the member
      await api.admit(
        personId: _registeredPerson!.id,
        planId: _selectedPlanId!,
        number: _numberController.text,
        idempotencyKey: _admitKey!,
      );

      if (!mounted) return;
      // The member can record meals only once the server's authorization
      // dispatch has run (a few seconds); admission itself is done.
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(
            'Admitted ${_nameController.text} (No. ${_numberController.text})',
          ),
        ),
      );
      Navigator.pop(context, true);
    } on ApiException catch (e) {
      if (!mounted) return;
      final signedOut = await context.read<AppController>().handleError(e);
      if (!mounted) return;
      if (signedOut) return;

      // Map specific error codes
      if (e.code == 'membership_number_taken') {
        setState(() {
          _numberError = 'That membership number is already in use.';
          _isSubmitting = false;
        });
      } else if (e.code == 'already_active_member') {
        setState(() {
          _error = 'That person is already a member.';
          _isSubmitting = false;
        });
      } else {
        setState(() {
          _error = e.userMessage;
          _isSubmitting = false;
        });
      }
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _error = 'Something went wrong. Please try again.';
        _isSubmitting = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_isLoading) {
      return Scaffold(
        appBar: AppBar(title: const Text('Admit member')),
        body: const Center(child: CircularProgressIndicator()),
      );
    }

    final openPlans = _plans.where((p) => p.open).toList();
    if (_plans.isEmpty && _error != null) {
      return Scaffold(
        appBar: AppBar(title: const Text('Admit member')),
        body: Center(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Text(_error!),
              const SizedBox(height: 12),
              FilledButton(
                key: const Key('retry'),
                onPressed: () {
                  setState(() => _error = null);
                  _loadPlans();
                },
                child: const Text('Retry'),
              ),
            ],
          ),
        ),
      );
    }

    return Scaffold(
      appBar: AppBar(title: const Text('Admit member')),
      body: Padding(
        padding: const EdgeInsets.all(16),
        child: SingleChildScrollView(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              if (_error != null) ...[
                Text(
                  _error!,
                  style: TextStyle(color: Theme.of(context).colorScheme.error),
                ),
                const SizedBox(height: 16),
              ],
              TextField(
                key: const Key('person-name'),
                controller: _nameController,
                decoration: const InputDecoration(
                  labelText: 'Full name',
                  border: OutlineInputBorder(),
                ),
                onChanged: (_) => setState(() {}),
              ),
              const SizedBox(height: 16),
              TextField(
                key: const Key('member-number'),
                controller: _numberController,
                decoration: InputDecoration(
                  labelText: 'Membership number',
                  border: const OutlineInputBorder(),
                  errorText: _numberError,
                ),
                onChanged: (_) => setState(() {}),
              ),
              const SizedBox(height: 16),
              DropdownButtonFormField<String>(
                key: const Key('plan'),
                initialValue: _selectedPlanId,
                decoration: const InputDecoration(
                  labelText: 'Plan',
                  border: OutlineInputBorder(),
                ),
                items: [
                  for (final plan in openPlans)
                    DropdownMenuItem(value: plan.id, child: Text(plan.name)),
                ],
                onChanged: (value) {
                  setState(() => _selectedPlanId = value);
                },
              ),
              if (openPlans.isEmpty)
                const Padding(
                  padding: EdgeInsets.only(top: 8),
                  child: Text('No plan is open to new members.'),
                ),
              const SizedBox(height: 24),
              FilledButton(
                key: const Key('submit-admit'),
                onPressed: _canSubmit ? _submit : null,
                child: _isSubmitting
                    ? const SizedBox(
                        height: 24,
                        width: 24,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Text('Admit'),
              ),
            ],
          ),
        ),
      ),
    );
  }

  bool get _canSubmit =>
      _nameController.text.isNotEmpty &&
      _numberController.text.isNotEmpty &&
      _selectedPlanId != null &&
      !_isSubmitting;
}
