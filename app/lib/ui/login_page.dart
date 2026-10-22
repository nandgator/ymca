import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../state/app_controller.dart';

/// Points the app at a server and a tenant and takes a credential.
///
/// The token field is the development authenticator's bearer token (ADR-106).
/// There is no password field because the platform has no passwords: identity
/// is the provider's, behind the Authenticator port.
class LoginPage extends StatefulWidget {
  const LoginPage({super.key});

  @override
  State<LoginPage> createState() => _LoginPageState();
}

class _LoginPageState extends State<LoginPage> {
  late final TextEditingController _base;
  late final TextEditingController _tenant;
  final _token = TextEditingController();

  @override
  void initState() {
    super.initState();
    final c = context.read<AppController>();
    _base = TextEditingController(text: c.lastBaseUrl);
    _tenant = TextEditingController(text: c.lastTenantId);
  }

  @override
  void dispose() {
    _base.dispose();
    _tenant.dispose();
    _token.dispose();
    super.dispose();
  }

  Future<void> _submit() => context.read<AppController>().signIn(
    Session(
      baseUrl: _base.text.trim(),
      tenantId: _tenant.text.trim(),
      token: _token.text.trim(),
    ),
  );

  @override
  Widget build(BuildContext context) {
    final c = context.watch<AppController>();
    return Scaffold(
      body: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 420),
          child: ListView(
            padding: const EdgeInsets.all(24),
            shrinkWrap: true,
            children: [
              Text('YMCA', style: Theme.of(context).textTheme.displaySmall),
              const SizedBox(height: 4),
              Text('Sign in', style: Theme.of(context).textTheme.titleMedium),
              const SizedBox(height: 24),
              TextField(
                key: const Key('server'),
                controller: _base,
                decoration: const InputDecoration(labelText: 'Server'),
                keyboardType: TextInputType.url,
              ),
              TextField(
                key: const Key('tenant'),
                controller: _tenant,
                decoration: const InputDecoration(labelText: 'Association ID'),
              ),
              TextField(
                key: const Key('token'),
                controller: _token,
                decoration: const InputDecoration(labelText: 'Access token'),
                obscureText: true,
                onSubmitted: (_) => _submit(),
              ),
              const SizedBox(height: 16),
              if (c.error != null)
                Padding(
                  padding: const EdgeInsets.only(bottom: 12),
                  child: Text(
                    c.error!,
                    key: const Key('login-error'),
                    style: TextStyle(
                      color: Theme.of(context).colorScheme.error,
                    ),
                  ),
                ),
              FilledButton(
                key: const Key('sign-in'),
                onPressed: c.busy ? null : _submit,
                child: c.busy
                    ? const SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Text('Sign in'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
