/// A3.10: every date on the wire is a calendar day, `YYYY-MM-DD`, in UTC.
/// These are the only places the app turns a [DateTime] into one.
String isoDay(DateTime d) {
  final u = d.toUtc();
  return '${u.year.toString().padLeft(4, '0')}-'
      '${u.month.toString().padLeft(2, '0')}-'
      '${u.day.toString().padLeft(2, '0')}';
}

/// Today as the server counts it: the UTC day.
String today([DateTime Function()? now]) => isoDay((now ?? DateTime.now)());

/// The day [n] days before [from] (default today).
String daysAgo(int n, {DateTime? from}) =>
    isoDay((from ?? DateTime.now()).toUtc().subtract(Duration(days: n)));

/// Parses `YYYY-MM-DD` as a UTC midnight, or null if it is not one.
DateTime? parseDay(String s) {
  final m = RegExp(r'^(\d{4})-(\d{2})-(\d{2})$').firstMatch(s);
  if (m == null) return null;
  final d = DateTime.utc(int.parse(m[1]!), int.parse(m[2]!), int.parse(m[3]!));
  return isoDay(d) == s ? d : null;
}
