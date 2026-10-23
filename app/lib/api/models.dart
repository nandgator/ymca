/// Wire models for the YMCA API (docs/system-design/A3_api_contract.md).
///
/// Every `fromJson` reads exactly the keys the Go handlers write, and a test
/// against the live API (test/live_api_test.dart) fails if either side drifts.
/// Money is a decimal STRING end to end: the server never sends a float, and
/// neither does this client.
library;

import '../util/dates.dart';

typedef Json = Map<String, dynamic>;

String? _s(Json j, String k) => j[k] as String?;
String _req(Json j, String k) => j[k] as String;

/// A3.5's page. The server sends `next_cursor: ""` on the last page; both that
/// and null mean there is no more.
class Paged<T> {
  const Paged(this.items, this.nextCursor);

  final List<T> items;
  final String? nextCursor;

  bool get hasMore => nextCursor != null && nextCursor!.isNotEmpty;

  factory Paged.fromJson(Json j, T Function(Json) item) => Paged([
    for (final i in (j['items'] as List? ?? const [])) item(i as Json),
  ], _s(j, 'next_cursor'));
}

/// A3.9. `permissions` are "tenant:admin"-style strings; the client renders
/// from what it is TOLD it may do, never from a role it guessed.
class Me {
  const Me({
    required this.principalId,
    required this.kind,
    required this.personId,
    required this.tenantId,
    required this.permissions,
    this.timeZone = 'UTC',
    this.utcOffsetSeconds = 0,
  });

  final String principalId;
  final String kind;
  final String personId;
  final String tenantId;
  final Set<String> permissions;

  /// The tenant's IANA zone and its offset from UTC right now (ADR-119).
  final String timeZone;
  final int utcOffsetSeconds;

  /// What "today" means for this tenant.
  Calendar get calendar => Calendar(utcOffsetSeconds);

  bool can(String permission) => permissions.contains(permission);

  bool get isAdmin => can('tenant:admin');
  bool get canAdmit => can('tenant:may_approve_membership');
  bool get canRegisterPerson => can('tenant:may_register_person');
  bool get canReadFinance => can('tenant:finance_reader');
  bool get canManageFinance => can('tenant:finance_manager');

  factory Me.fromJson(Json j) => Me(
    principalId: _req(j, 'principal_id'),
    kind: _req(j, 'kind'),
    personId: _s(j, 'person_id') ?? '',
    tenantId: _req(j, 'tenant_id'),
    permissions: {
      for (final p in (j['permissions'] as List? ?? const [])) p as String,
    },
    timeZone: _s(j, 'time_zone') ?? 'UTC',
    utcOffsetSeconds: (j['utc_offset_seconds'] as num?)?.toInt() ?? 0,
  );
}

class ConsumptionType {
  const ConsumptionType({
    required this.id,
    required this.name,
    required this.obligates,
    required this.recurrence,
    required this.recordMode,
    required this.status,
    required this.correctionWindowDays,
  });

  final String id;
  final String name;
  final bool obligates;
  final String? recurrence;

  /// SELF | STAFF | EITHER (05.10.3).
  final String recordMode;
  final String status;
  final int? correctionWindowDays;

  /// Whether the signed-in person may record for themselves on this type.
  bool get allowsSelf => recordMode != 'STAFF';

  /// Whether staff may record for somebody else on this type.
  bool get allowsStaff => recordMode != 'SELF';

  factory ConsumptionType.fromJson(Json j) => ConsumptionType(
    id: _req(j, 'id'),
    name: _req(j, 'name'),
    obligates: j['obligates'] as bool? ?? false,
    recurrence: _s(j, 'recurrence'),
    recordMode: _req(j, 'record_mode'),
    status: _req(j, 'status'),
    correctionWindowDays: j['correction_window_days'] as int?,
  );
}

class ConsumptionOption {
  const ConsumptionOption({
    required this.id,
    required this.code,
    required this.name,
    required this.chargeKind,
    required this.amount,
    required this.currency,
    required this.status,
  });

  final String id;
  final String code;
  final String name;
  final String chargeKind;
  final String amount;
  final String currency;
  final String status;

  factory ConsumptionOption.fromJson(Json j) => ConsumptionOption(
    id: _req(j, 'id'),
    code: _req(j, 'code'),
    name: _req(j, 'name'),
    chargeKind: _req(j, 'charge_kind'),
    amount: _req(j, 'amount'),
    currency: _req(j, 'currency'),
    status: _req(j, 'status'),
  );
}

class ConsumptionRecord {
  const ConsumptionRecord({
    required this.id,
    required this.typeId,
    required this.subjectPersonId,
    required this.actorPersonId,
    required this.occurredOn,
    required this.variantCode,
    required this.optionCodes,
    required this.supersedesId,
    required this.voided,
    required this.recordedAt,
  });

  final String id;
  final String typeId;
  final String subjectPersonId;
  final String actorPersonId;

  /// A calendar day, `YYYY-MM-DD`. A date, never a timestamp (A3.10).
  final String occurredOn;
  final String? variantCode;
  final List<String> optionCodes;
  final String? supersedesId;
  final bool voided;
  final String recordedAt;

  factory ConsumptionRecord.fromJson(Json j) => ConsumptionRecord(
    id: _req(j, 'id'),
    typeId: _req(j, 'consumption_type_id'),
    subjectPersonId: _req(j, 'subject_person_id'),
    actorPersonId: _req(j, 'actor_person_id'),
    occurredOn: _req(j, 'occurred_on'),
    variantCode: _s(j, 'variant_code'),
    optionCodes: [
      for (final c in (j['option_codes'] as List? ?? const [])) c as String,
    ],
    supersedesId: _s(j, 'supersedes_id'),
    voided: j['voided'] as bool? ?? false,
    recordedAt: _req(j, 'recorded_at'),
  );
}

/// One row of the front desk's member list (A3.12).
class DirectoryEntry {
  const DirectoryEntry({
    required this.membershipId,
    required this.personId,
    required this.displayName,
    required this.number,
    required this.state,
    required this.planId,
  });

  final String membershipId;
  final String personId;
  final String displayName;
  final String number;
  final String state;
  final String planId;

  factory DirectoryEntry.fromJson(Json j) => DirectoryEntry(
    membershipId: _req(j, 'membership_id'),
    personId: _req(j, 'person_id'),
    displayName: _req(j, 'display_name'),
    number: _req(j, 'number'),
    state: _req(j, 'state'),
    planId: _req(j, 'plan_id'),
  );
}

class MembershipPlan {
  const MembershipPlan({
    required this.id,
    required this.code,
    required this.name,
    required this.status,
  });

  final String id;
  final String code;
  final String name;
  final String status;

  bool get open => status == 'OPEN';

  factory MembershipPlan.fromJson(Json j) => MembershipPlan(
    id: _req(j, 'id'),
    code: _req(j, 'code'),
    name: _req(j, 'name'),
    status: _req(j, 'status'),
  );
}

class Person {
  const Person({required this.id, required this.displayName});

  final String id;
  final String displayName;

  factory Person.fromJson(Json j) =>
      Person(id: _req(j, 'id'), displayName: _req(j, 'display_name'));
}

class Membership {
  const Membership({
    required this.id,
    required this.personId,
    required this.number,
    required this.state,
  });

  final String id;
  final String personId;
  final String number;
  final String state;

  factory Membership.fromJson(Json j) => Membership(
    id: _req(j, 'id'),
    personId: _req(j, 'person_id'),
    number: _req(j, 'number'),
    state: _req(j, 'state'),
  );
}

class Component {
  const Component({
    required this.kind,
    required this.amount,
    required this.currency,
  });

  final String kind;
  final String amount;
  final String currency;

  factory Component.fromJson(Json j) => Component(
    kind: _req(j, 'kind'),
    amount: _req(j, 'amount'),
    currency: _req(j, 'currency'),
  );
}

class Charge {
  const Charge({
    required this.id,
    required this.sourceType,
    required this.description,
    required this.components,
  });

  final String id;
  final String sourceType;
  final String? description;
  final List<Component> components;

  factory Charge.fromJson(Json j) => Charge(
    id: _req(j, 'id'),
    sourceType: _req(j, 'source_type'),
    description: _s(j, 'description'),
    components: [
      for (final c in (j['components'] as List? ?? const []))
        Component.fromJson(c as Json),
    ],
  );
}

class Payment {
  const Payment({
    required this.id,
    required this.method,
    required this.amount,
    required this.currency,
    required this.receivedAt,
  });

  final String id;
  final String method;
  final String amount;
  final String currency;
  final String receivedAt;

  factory Payment.fromJson(Json j) => Payment(
    id: _req(j, 'id'),
    method: _req(j, 'method'),
    amount: _req(j, 'amount'),
    currency: _req(j, 'currency'),
    receivedAt: _req(j, 'received_at'),
  );
}

/// A correction to an issued invoice (ADR-120). It lowers what the invoice
/// asks for; the invoice itself is never edited.
class CreditNote {
  const CreditNote({
    required this.id,
    required this.number,
    required this.amount,
    required this.currency,
    required this.reason,
    required this.issuedAt,
  });

  final String id;
  final String number;
  final String amount;
  final String currency;
  final String reason;
  final String issuedAt;

  factory CreditNote.fromJson(Json j) => CreditNote(
    id: _req(j, 'id'),
    number: _req(j, 'number'),
    amount: _req(j, 'amount'),
    currency: _req(j, 'currency'),
    reason: _req(j, 'reason'),
    issuedAt: _req(j, 'issued_at'),
  );
}

/// DRAFT -> ISSUED -> PAID, or VOIDED once credited in full with nothing
/// received (A3.11, ADR-120). `charges`, `payments` and `balance` are
/// present on the detail read only.
class Invoice {
  const Invoice({
    required this.id,
    required this.number,
    required this.state,
    required this.currency,
    required this.issuedAt,
    required this.dueAt,
    required this.totalAmount,
    required this.createdAt,
    required this.balance,
    required this.charges,
    required this.payments,
    this.creditNotes = const [],
  });

  final String id;
  final String? number;
  final String state;
  final String currency;
  final String? issuedAt;
  final String? dueAt;
  final String? totalAmount;
  final String createdAt;
  final String? balance;
  final List<Charge> charges;
  final List<Payment> payments;
  final List<CreditNote> creditNotes;

  bool get isDraft => state == 'DRAFT';
  bool get isIssued => state == 'ISSUED';
  bool get isPaid => state == 'PAID';
  bool get isVoided => state == 'VOIDED';

  /// Credit notes correct an invoice that has been issued, whether or not it
  /// has since been paid.
  bool get canBeCredited => isIssued || isPaid;

  /// More was received than the invoice now asks for: money owed back.
  bool get refundDue => (double.tryParse(balance ?? '') ?? 0) < 0;

  factory Invoice.fromJson(Json j) => Invoice(
    id: _req(j, 'id'),
    number: _s(j, 'number'),
    state: _req(j, 'state'),
    currency: _req(j, 'currency'),
    issuedAt: _s(j, 'issued_at'),
    dueAt: _s(j, 'due_at'),
    totalAmount: _s(j, 'total_amount'),
    createdAt: _req(j, 'created_at'),
    balance: _s(j, 'balance'),
    charges: [
      for (final c in (j['charges'] as List? ?? const []))
        Charge.fromJson(c as Json),
    ],
    payments: [
      for (final p in (j['payments'] as List? ?? const []))
        Payment.fromJson(p as Json),
    ],
    creditNotes: [
      for (final n in (j['credit_notes'] as List? ?? const []))
        CreditNote.fromJson(n as Json),
    ],
  );
}

/// One person's closed period (A3.10).
class ClosedPeriod {
  const ClosedPeriod({
    required this.id,
    required this.subjectPersonId,
    required this.from,
    required this.to,
    required this.obligatedUnits,
    required this.invoiceId,
  });

  final String id;
  final String subjectPersonId;
  final String from;
  final String to;
  final int obligatedUnits;
  final String? invoiceId;

  factory ClosedPeriod.fromJson(Json j) => ClosedPeriod(
    id: _req(j, 'id'),
    subjectPersonId: _req(j, 'subject_person_id'),
    from: _req(j, 'from'),
    to: _req(j, 'to'),
    obligatedUnits: j['obligated_units'] as int? ?? 0,
    invoiceId: _s(j, 'invoice_id'),
  );
}

class Absence {
  const Absence({
    required this.id,
    required this.from,
    required this.to,
    required this.relievesPayment,
  });

  final String id;
  final String from;
  final String to;
  final bool relievesPayment;

  factory Absence.fromJson(Json j) => Absence(
    id: _req(j, 'id'),
    from: _req(j, 'from'),
    to: _req(j, 'to'),
    relievesPayment: j['relieves_payment'] as bool? ?? false,
  );
}
