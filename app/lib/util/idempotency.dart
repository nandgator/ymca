import 'dart:math';

final _rng = Random.secure();

/// A fresh Idempotency-Key (A3.6).
///
/// A screen makes ONE when the user starts an action and keeps it until the
/// action succeeds, then makes another. Reusing it across a retry is what
/// makes a retried meal one meal and a retried payment one payment; making a
/// new one per tap would defeat the header entirely.
String newIdempotencyKey() {
  final bytes = List<int>.generate(16, (_) => _rng.nextInt(256));
  return bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
}
