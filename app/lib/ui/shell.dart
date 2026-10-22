import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../state/app_controller.dart';
import 'bills_page.dart';
import 'close_page.dart';
import 'invoices_page.dart';
import 'meals_page.dart';
import 'members_page.dart';

/// One destination of the shell, shown only to a principal the server says
/// holds [permission] (null means everyone signed in).
class _Destination {
  const _Destination(this.label, this.icon, this.permission, this.builder);

  final String label;
  final IconData icon;
  final String? permission;
  final WidgetBuilder builder;
}

/// The permission-gated shell (A3.7, A3.9): members and staff use one app, and
/// what appears is decided by what `/me` reported — never by a role guessed.
///
/// The destinations a person sees are the ones their permissions justify, in a
/// fixed order, so a tab does not move because another appeared.
final List<_Destination> _destinations = [
  _Destination('Meals', Icons.restaurant, null, (_) => const MealsPage()),
  _Destination('Bills', Icons.receipt_long, null, (_) => const BillsPage()),
  _Destination(
    'Members',
    Icons.groups,
    'tenant:may_approve_membership',
    (_) => const MembersPage(),
  ),
  _Destination(
    'Invoices',
    Icons.account_balance,
    'tenant:finance_reader',
    (_) => const InvoicesPage(),
  ),
  _Destination(
    'Close',
    Icons.event_available,
    'tenant:admin',
    (_) => const ClosePage(),
  ),
];

class AppShell extends StatefulWidget {
  const AppShell({super.key});

  @override
  State<AppShell> createState() => _AppShellState();
}

class _AppShellState extends State<AppShell> {
  int _index = 0;

  @override
  Widget build(BuildContext context) {
    final c = context.watch<AppController>();
    final me = c.me!;
    final visible = [
      for (final d in _destinations)
        if (d.permission == null || me.can(d.permission!)) d,
    ];
    final index = _index.clamp(0, visible.length - 1);

    return LayoutBuilder(
      builder: (context, box) {
        final wide = box.maxWidth >= 720;
        final body = KeyedSubtree(
          key: ValueKey(visible[index].label),
          child: visible[index].builder(context),
        );
        final signOut = IconButton(
          key: const Key('sign-out'),
          tooltip: 'Sign out',
          icon: const Icon(Icons.logout),
          onPressed: c.signOut,
        );
        return Scaffold(
          appBar: AppBar(title: Text(visible[index].label), actions: [signOut]),
          body: wide
              ? Row(
                  children: [
                    NavigationRail(
                      selectedIndex: index,
                      onDestinationSelected: (i) => setState(() => _index = i),
                      labelType: NavigationRailLabelType.all,
                      destinations: [
                        for (final d in visible)
                          NavigationRailDestination(
                            icon: Icon(d.icon),
                            label: Text(d.label),
                          ),
                      ],
                    ),
                    const VerticalDivider(width: 1),
                    Expanded(child: body),
                  ],
                )
              : body,
          bottomNavigationBar: wide || visible.length < 2
              ? null
              : NavigationBar(
                  selectedIndex: index,
                  onDestinationSelected: (i) => setState(() => _index = i),
                  destinations: [
                    for (final d in visible)
                      NavigationDestination(icon: Icon(d.icon), label: d.label),
                  ],
                ),
        );
      },
    );
  }
}
