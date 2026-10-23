import 'models.dart';

/// A3.4's error: a stable machine-readable [code], and a [message] written for
/// a developer — never rendered to a member (use [userMessage]).
class ApiException implements Exception {
  const ApiException(this.status, this.code, this.message);

  final int status;
  final String code;
  final String message;

  bool get unauthenticated => status == 401;
  bool get forbidden => status == 403;
  bool get notFound => status == 404;
  bool get conflict => status == 409;

  /// What a person is shown. The server's message is for a developer (A3.4),
  /// so this maps the codes a screen can act on and says nothing else.
  String get userMessage => switch (code) {
    'unauthenticated' || 'token_expired' => 'Please sign in again.',
    'forbidden' || 'tenant_mismatch' => "You don't have access to that.",
    'not_found' => 'That could not be found.',
    'already_recorded' => 'That day is already recorded.',
    'period_closed' => 'That period has already been billed.',
    'period_already_closed' => 'That period has already been closed.',
    'consumption_window_closed' =>
      'That record is too old to correct without extra authority.',
    'record_mode_not_allowed' =>
      'This kind of recording is not allowed for that item.',
    'membership_number_taken' => 'That membership number is already in use.',
    'already_active_member' => 'That person is already a member.',
    'no_rate' => 'No price is set for that item yet.',
    'membership_wrong_state' =>
      'That membership cannot be changed that way right now.',
    'invoice_wrong_state' => 'That invoice cannot be changed right now.',
    'payment_exceeds_balance' => 'That is more than is owed.',
    'credit_exceeds_invoice' => 'That is more than the invoice still asks for.',
    'currency_mismatch' => 'That currency does not match the invoice.',
    'invalid_request' => 'Please check what you entered.',
    _ => 'Something went wrong. Please try again.',
  };

  @override
  String toString() => 'ApiException($status $code: $message)';
}

/// The client's whole view of the server. Screens depend on THIS, not on
/// HTTP, so each can be tested against a fake and the live test can prove the
/// real implementation agrees with the contract.
///
/// Every mutating call that A3.6 scopes to an Idempotency-Key takes one from
/// the caller: a screen generates a key when the user starts an action and
/// REUSES it on a retry, which is the entire point of the header.
abstract class YmcaApi {
  Future<Me> me();

  // Member reads.
  Future<Paged<ConsumptionType>> myRecordableTypes({String? cursor});
  Future<Paged<ConsumptionOption>> options(String typeId, {String? cursor});
  Future<Paged<ConsumptionRecord>> myConsumption({String? cursor});
  Future<Paged<Invoice>> myInvoices({String? cursor});
  Future<Invoice> invoice(String id);

  /// Records one use. [subjectPersonId] null means the caller themselves.
  /// [occurredOn] is `YYYY-MM-DD`.
  Future<ConsumptionRecord> recordUse(
    String typeId, {
    required String occurredOn,
    String? subjectPersonId,
    String? variantCode,
    List<String> optionCodes,
    required String idempotencyKey,
  });

  /// Supersedes a record (A3.10). Void says no consumption occurred.
  Future<ConsumptionRecord> correctRecord(
    String recordId, {
    bool voidIt,
    String? variantCode,
    List<String> optionCodes,
    required String idempotencyKey,
  });

  Future<Absence> declareAbsence({
    required String from,
    required String to,
    String? subjectPersonId,
  });

  // Staff reads and writes.
  Future<Paged<DirectoryEntry>> directory({String? query, String? cursor});
  Future<Paged<MembershipPlan>> plans({String? cursor});
  Future<Person> registerPerson({required String displayName});
  Future<Membership> admit({
    required String personId,
    required String planId,
    required String number,
    required String idempotencyKey,
  });
  Future<Paged<ConsumptionType>> consumptionTypes({String? cursor});
  Future<Paged<ConsumptionRecord>> typeRecords(
    String typeId, {
    String? subjectPersonId,
    String? from,
    String? to,
    String? cursor,
  });
  Future<List<ClosedPeriod>> closePeriod(
    String typeId, {
    required String from,
    required String to,
    required String idempotencyKey,
  });

  // Membership lifecycle (A3.13). Suspending and terminating revoke coverage
  // on the server before they answer; reinstating re-grants, which may lag.
  Future<Membership> suspendMembership(
    String id, {
    required String reason,
    required String idempotencyKey,
  });
  Future<Membership> reinstateMembership(
    String id, {
    required String idempotencyKey,
  });
  Future<Membership> terminateMembership(
    String id, {
    required String idempotencyKey,
  });

  // Finance.
  Future<Paged<Invoice>> invoices({String? cursor});
  Future<Invoice> issueInvoice(
    String id, {
    String? dueAt,
    required String idempotencyKey,
  });
  Future<Payment> payInvoice(
    String id, {
    required String method,
    required String amount,
    required String currency,
    required String idempotencyKey,
  });
  Future<CreditNote> creditInvoice(
    String id, {
    required String amount,
    required String reason,
    required String idempotencyKey,
  });
}
