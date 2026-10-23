import 'package:flutter_test/flutter_test.dart';
import 'package:ymca_app/api/models.dart';
import 'package:ymca_app/util/dates.dart';
import 'package:ymca_app/util/idempotency.dart';

void main() {
  test('a day is formatted as the server counts it, in UTC', () {
    expect(isoDay(DateTime.utc(2026, 9, 5, 23, 59)), '2026-09-05');
    expect(isoDay(DateTime.parse('2026-09-05T23:30:00-05:00')), '2026-09-06');
  });

  test('a tenant day is the instant moved by its offset', () {
    final instant = DateTime.utc(
      2026,
      10,
      23,
      20,
      0,
    ); // 01:30 on the 24th in IST
    expect(const Calendar().today(() => instant), '2026-10-23');
    expect(const Calendar(19800).today(() => instant), '2026-10-24');
    expect(const Calendar(19800).daysAgo(1, from: instant), '2026-10-23');
    expect(const Calendar(-39600).today(() => instant), '2026-10-23');
    expect(
      const Calendar(-39600).today(() => DateTime.utc(2026, 10, 23, 10, 59)),
      '2026-10-22',
      reason: 'west of UTC the tenant is still on the previous day',
    );
    expect(isoDay(const Calendar(19800).wallClock(instant)), '2026-10-24');
  });

  test('a picked date keeps its calendar day whatever the device zone', () {
    // A picker answers with the device's local midnight; east of Greenwich
    // that instant is the previous UTC day, which isoDay alone would send.
    final picked = DateTime.parse('2026-09-30T00:00:00+05:30');
    expect(isoDay(picked), '2026-09-29');
    expect(isoDay(pickedDay(DateTime(2026, 9, 30))), '2026-09-30');
  });

  test('/me carries the tenant calendar, and UTC when it says nothing', () {
    final base = {
      'principal_id': 'p',
      'kind': 'PERSONAL',
      'tenant_id': 't',
      'permissions': <String>[],
    };
    final zoned = Me.fromJson({
      ...base,
      'time_zone': 'Asia/Kolkata',
      'utc_offset_seconds': 19800,
    });
    expect(zoned.timeZone, 'Asia/Kolkata');
    expect(zoned.calendar.utcOffsetSeconds, 19800);
    final bare = Me.fromJson(base);
    expect(bare.timeZone, 'UTC');
    expect(bare.calendar.utcOffsetSeconds, 0);
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
