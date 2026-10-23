/// A3.10: every date on the wire is a calendar day, `YYYY-MM-DD`, in UTC.
/// These are the only places the app turns a [DateTime] into one.
String isoDay(DateTime d) {
  final u = d.toUtc();
  return '${u.year.toString().padLeft(4, '0')}-'
      '${u.month.toString().padLeft(2, '0')}-'
      '${u.day.toString().padLeft(2, '0')}';
}

/// The tenant's calendar (ADR-119). "Today" belongs to the association, not to
/// the device and not to UTC: a meal eaten at 1am in Mumbai is that day's meal.
/// The app has no zone database, so the server says the offset in force now
/// (`utc_offset_seconds` on /me) and every instant becomes a day through it.
class Calendar {
  const Calendar([this.utcOffsetSeconds = 0]);

  /// Seconds east of UTC. Zero is UTC, which is what a tenant is until it is
  /// given a zone.
  final int utcOffsetSeconds;

  /// The tenant's wall clock at [instant], as a UTC-flagged [DateTime] whose
  /// year, month, day and time are the tenant's. Read its fields; do not
  /// convert it.
  DateTime wallClock([DateTime? instant]) => (instant ?? DateTime.now())
      .toUtc()
      .add(Duration(seconds: utcOffsetSeconds));

  /// The tenant's calendar day at [now].
  String today([DateTime Function()? now]) => isoDay(wallClock(now?.call()));

  /// The day [n] days before [from] (default now), on the tenant's calendar.
  String daysAgo(int n, {DateTime? from}) =>
      isoDay(wallClock(from).subtract(Duration(days: n)));
}

/// Today in UTC. Anything that acts for a tenant uses its [Calendar] instead.
String today([DateTime Function()? now]) => const Calendar().today(now);

/// The day [n] days before [from] (default now) in UTC.
String daysAgo(int n, {DateTime? from}) =>
    const Calendar().daysAgo(n, from: from);

/// The calendar day a date picker returned, as the UTC midnight [isoDay]
/// expects. A picker hands back the device's local midnight, and converting
/// that to UTC lands a day early east of Greenwich.
DateTime pickedDay(DateTime picked) =>
    DateTime.utc(picked.year, picked.month, picked.day);

/// Parses `YYYY-MM-DD` as a UTC midnight, or null if it is not one.
DateTime? parseDay(String s) {
  final m = RegExp(r'^(\d{4})-(\d{2})-(\d{2})$').firstMatch(s);
  if (m == null) return null;
  final d = DateTime.utc(int.parse(m[1]!), int.parse(m[2]!), int.parse(m[3]!));
  return isoDay(d) == s ? d : null;
}
