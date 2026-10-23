import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../api/models.dart';
import '../api/ymca_api.dart';
import '../state/app_controller.dart';
import '../util/dates.dart';

/// The outcome of [attempt]: a value, or a message to show a person.
class Attempt<T> {
  const Attempt._(this.value, this.message);

  final T? value;

  /// Already fit for a person (ApiException.userMessage, never the server's
  /// own text). Null on success, and null when the user was signed out —
  /// there is nothing left to say on a screen that is about to be replaced.
  final String? message;

  bool get ok => message == null && value != null;
}

/// Runs an API call the way every screen must: an expired or refused
/// credential signs the user out ([AppController.handleError]); anything else
/// becomes a sentence for a person. A3.4 writes the server's message for a
/// developer, so it is never returned.
///
/// Callers check `context.mounted` after awaiting this, as after any await.
Future<Attempt<T>> attempt<T>(
  BuildContext context,
  Future<T> Function() call,
) async {
  final controller = context.read<AppController>();
  try {
    return Attempt._(await call(), null);
  } on ApiException catch (e) {
    if (await controller.handleError(e)) return const Attempt._(null, null);
    return Attempt._(null, e.userMessage);
  } catch (_) {
    return const Attempt._(null, 'Something went wrong. Please try again.');
  }
}

/// The signed-in tenant's calendar: what "today" is for the association, which
/// is not the device's day and not UTC's (ADR-119).
Calendar calendarOf(BuildContext context) =>
    context.read<AppController>().me?.calendar ?? const Calendar();

/// A message and a Retry button, centred.
class ErrorRetry extends StatelessWidget {
  const ErrorRetry({super.key, required this.message, required this.onRetry});

  final String message;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) => Center(
    child: Padding(
      padding: const EdgeInsets.all(24),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(message, textAlign: TextAlign.center),
          const SizedBox(height: 12),
          FilledButton(
            key: const Key('retry'),
            onPressed: onRetry,
            child: const Text('Retry'),
          ),
        ],
      ),
    ),
  );
}

/// Shows a failure beside content that is still on screen.
void showFailure(BuildContext context, String message) =>
    ScaffoldMessenger.of(context)
        .showSnackBar(SnackBar(content: Text(message)));

/// A keyset-paged list (A3.5) with the four states every list needs —
/// loading, error with Retry, empty, data — plus pull-to-refresh and a
/// "Load more" button while the server has more.
///
/// Changing [reloadToken] reloads from the first page, which is how a parent
/// asks for a refresh after something it launched has changed the data.
class PagedList<T> extends StatefulWidget {
  const PagedList({
    super.key,
    required this.fetch,
    required this.itemBuilder,
    required this.emptyText,
    this.reloadToken = 0,
    this.header,
    this.filter,
  });

  final Future<Paged<T>> Function(String? cursor) fetch;
  final Widget Function(BuildContext, T) itemBuilder;
  final String emptyText;
  final int reloadToken;

  /// Shown above the rows in every state, e.g. a row of filter chips.
  final Widget? header;

  /// Narrows what is shown without refetching.
  final bool Function(T)? filter;

  @override
  State<PagedList<T>> createState() => _PagedListState<T>();
}

class _PagedListState<T> extends State<PagedList<T>> {
  final List<T> _items = [];
  String? _cursor;
  bool _loading = true;
  String? _error;
  int _seq = 0;

  @override
  void initState() {
    super.initState();
    _load(reset: true);
  }

  @override
  void didUpdateWidget(PagedList<T> old) {
    super.didUpdateWidget(old);
    if (old.reloadToken != widget.reloadToken) _load(reset: true);
  }

  Future<void> _load({required bool reset}) async {
    // Only the newest request may change what is on screen: a slow first page
    // must not overwrite a refresh that has already finished.
    final seq = ++_seq;
    setState(() {
      _loading = true;
      _error = null;
      if (reset) {
        _items.clear();
        _cursor = null;
      }
    });
    final result = await attempt(context, () => widget.fetch(_cursor));
    if (!mounted || seq != _seq) return;
    final page = result.value;
    setState(() {
      _loading = false;
      if (page != null) {
        _items.addAll(page.items);
        _cursor = page.hasMore ? page.nextCursor : null;
      } else {
        _error = result.message;
      }
    });
    if (page == null && _items.isNotEmpty && result.message != null) {
      showFailure(context, result.message!);
    }
  }

  @override
  Widget build(BuildContext context) {
    final shown = widget.filter == null
        ? _items
        : [
            for (final i in _items)
              if (widget.filter!(i)) i,
          ];

    final Widget body;
    if (_loading && _items.isEmpty) {
      body = const Center(child: CircularProgressIndicator());
    } else if (_error != null && _items.isEmpty) {
      body = ErrorRetry(message: _error!, onRetry: () => _load(reset: true));
    } else if (shown.isEmpty) {
      body = RefreshIndicator(
        onRefresh: () => _load(reset: true),
        child: ListView(
          children: [
            Padding(
              padding: const EdgeInsets.all(32),
              child: Text(widget.emptyText, textAlign: TextAlign.center),
            ),
          ],
        ),
      );
    } else {
      body = RefreshIndicator(
        onRefresh: () => _load(reset: true),
        child: ListView.builder(
          itemCount: shown.length + (_cursor != null ? 1 : 0),
          itemBuilder: (context, i) {
            if (i == shown.length) {
              return Padding(
                padding: const EdgeInsets.all(16),
                child: Center(
                  child: OutlinedButton(
                    key: const Key('load-more'),
                    onPressed: _loading ? null : () => _load(reset: false),
                    child: const Text('Load more'),
                  ),
                ),
              );
            }
            return widget.itemBuilder(context, shown[i]);
          },
        ),
      );
    }

    if (widget.header == null) return body;
    return Column(
      children: [
        widget.header!,
        Expanded(child: body),
      ],
    );
  }
}

/// `2026-10-01T00:00:00Z` -> `2026-10-01`.
String dayOf(String timestamp) => timestamp.split('T').first;

/// '1500.00 INR' — money is a decimal string end to end.
String money(String amount, String currency) => '$amount $currency';

String stateLabel(String state) => switch (state) {
  'DRAFT' => 'Draft',
  'ISSUED' => 'Issued',
  'PAID' => 'Paid',
  'VOIDED' => 'Voided',
  _ => state,
};
