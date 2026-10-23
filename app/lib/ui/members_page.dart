import 'dart:async';

import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../api/models.dart' as models;
import '../api/ymca_api.dart';
import '../state/app_controller.dart';
import 'admit_member_page.dart';
import 'membership_actions.dart';
import 'record_for_member_sheet.dart';

class MembersPage extends StatefulWidget {
  const MembersPage({super.key});

  @override
  State<MembersPage> createState() => _MembersPageState();
}

class _MembersPageState extends State<MembersPage> {
  late TextEditingController _searchController;
  Timer? _debounceTimer;
  String _currentQuery = '';
  String? _currentCursor;
  List<models.DirectoryEntry> _items = [];
  bool _hasMore = false;
  bool _isLoading = false;
  String? _error;
  // Search responses can arrive out of order; only the newest request's may
  // be applied.
  int _requestSeq = 0;

  @override
  void initState() {
    super.initState();
    _searchController = TextEditingController();
    _reload();
  }

  @override
  void dispose() {
    _searchController.dispose();
    _debounceTimer?.cancel();
    super.dispose();
  }

  Future<void> _reload() async {
    if (!mounted) return;
    setState(() {
      _error = null;
      _items = [];
      _currentCursor = null;
    });
    await _loadDirectory();
  }

  Future<void> _loadDirectory({String? cursor}) async {
    if (!mounted) return;
    final seq = ++_requestSeq;
    setState(() => _isLoading = true);
    try {
      final api = context.read<YmcaApi>();
      final page = await api.directory(query: _currentQuery, cursor: cursor);
      if (!mounted || seq != _requestSeq) return;
      setState(() {
        if (cursor == null) {
          _items = page.items;
        } else {
          _items.addAll(page.items);
        }
        _currentCursor = page.nextCursor;
        _hasMore = page.hasMore;
        _error = null;
        _isLoading = false;
      });
    } on ApiException catch (e) {
      if (!mounted || seq != _requestSeq) return;
      final signedOut = await context.read<AppController>().handleError(e);
      if (!mounted) return;
      if (!signedOut) {
        setState(() {
          _error = e.userMessage;
          _isLoading = false;
        });
      }
    } catch (_) {
      if (!mounted || seq != _requestSeq) return;
      setState(() {
        _error = 'Something went wrong. Please try again.';
        _isLoading = false;
      });
    }
  }

  void _onSearchChanged(String value) {
    _debounceTimer?.cancel();
    _debounceTimer = Timer(const Duration(milliseconds: 300), () {
      _currentQuery = value;
      _reload();
    });
  }

  Future<void> _admitMember() async {
    final result = await Navigator.of(
      context,
    ).push<bool?>(MaterialPageRoute(builder: (_) => const AdmitMemberPage()));
    if (result == true && mounted) {
      _reload();
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      // No AppBar of its own: the shell supplies it. The Scaffold is here for
      // the floating action button.
      body: Column(
        children: [
          Padding(
            padding: const EdgeInsets.all(16),
            child: TextField(
              key: const Key('member-search'),
              controller: _searchController,
              decoration: InputDecoration(
                hintText: 'Search members',
                border: OutlineInputBorder(
                  borderRadius: BorderRadius.circular(8),
                ),
                contentPadding: const EdgeInsets.symmetric(
                  horizontal: 16,
                  vertical: 8,
                ),
              ),
              onChanged: _onSearchChanged,
            ),
          ),
          Expanded(
            child: RefreshIndicator(onRefresh: _reload, child: _buildContent()),
          ),
        ],
      ),
      floatingActionButton: FloatingActionButton.extended(
        key: const Key('admit-member'),
        onPressed: _admitMember,
        icon: const Icon(Icons.person_add),
        label: const Text('Admit member'),
      ),
    );
  }

  Widget _buildContent() {
    if (_error != null) {
      return SingleChildScrollView(
        physics: const AlwaysScrollableScrollPhysics(),
        child: Center(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                Text(_error!),
                const SizedBox(height: 16),
                FilledButton(onPressed: _reload, child: const Text('Retry')),
              ],
            ),
          ),
        ),
      );
    }

    if (_items.isEmpty && _isLoading) {
      return const Center(child: CircularProgressIndicator());
    }

    if (_items.isEmpty) {
      return SingleChildScrollView(
        physics: const AlwaysScrollableScrollPhysics(),
        child: Center(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: const Text('No members found'),
          ),
        ),
      );
    }

    return ListView.builder(
      physics: const AlwaysScrollableScrollPhysics(),
      itemCount: _items.length + (_hasMore ? 1 : 0),
      itemBuilder: (context, index) {
        if (index == _items.length) {
          return Padding(
            padding: const EdgeInsets.all(16),
            child: Center(
              child: FilledButton(
                onPressed: () => _loadDirectory(cursor: _currentCursor),
                child: const Text('Load more'),
              ),
            ),
          );
        }

        final member = _items[index];
        return ListTile(
          title: Text(member.displayName),
          subtitle: Text('No. ${member.number} · ${member.stateLabel}'),
          onTap: () => _showMemberSheet(member),
        );
      },
    );
  }

  // The page's context, not a sheet's: the sheet pops before the next
  // surface opens, and a popped sheet's context is not one to build on.
  Future<void> _showMemberSheet(models.DirectoryEntry member) async {
    final result = await showMemberSheet(context, member);
    if (!mounted) return;
    switch (result) {
      case MemberSheetResult.recordMeal:
        await showRecordForMemberSheet(context, member);
      case MemberSheetResult.changed:
        await _reload();
      case null:
        break;
    }
  }
}
