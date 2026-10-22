import 'package:flutter_test/flutter_test.dart';
import 'package:ymca_app/util/dates.dart';
import 'package:ymca_app/util/idempotency.dart';

void main() {
  test('a day is formatted as the server counts it, in UTC', () {
    expect(isoDay(DateTime.utc(2026, 9, 5, 23, 59)), '2026-09-05');
    expect(isoDay(DateTime.parse('2026-09-05T23:30:00-05:00')), '2026-09-06');
  });

  test('parseDay accepts real days only', () {
    expect(parseDay('2026-02-28'), DateTime.utc(2026, 2, 28));
    expect(parseDay('2026-02-30'), isNull);
    expect(parseDay('26-02-28'), isNull);
    expect(parseDay(''), isNull);
  });

  test('idempotency keys are distinct and not tiny', () {
    final keys = {for (var i = 0; i < 50; i++) newIdempotencyKey()};
    expect(keys.length, 50);
    expect(keys.first.length, greaterThanOrEqualTo(32));
  });
}
