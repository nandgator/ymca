import 'package:ymca_app/api/models.dart';
import 'package:ymca_app/api/ymca_api.dart';

/// A configurable in-memory [YmcaApi] for widget tests.
///
/// Set the fields a test cares about; every call is appended to [calls] as
/// `name key=value ...` so a test can assert what a screen sent, including the
/// Idempotency-Key it chose. Setting [failWith] makes the NEXT mutating or
/// reading call throw once (it is cleared as it fires), which is how a test
/// exercises a screen's error path and then its retry.
class FakeYmcaApi implements YmcaApi {
  FakeYmcaApi({
    this._me,
    this.recordable = const [],
    this.optionList = const [],
    this.records = const [],
    this.myInvoiceList = const [],
    this.allInvoices = const [],
    this.members = const [],
    this.planList = const [],
    this.typeList = const [],
    this.invoiceById = const {},
  });

  final Me? _me;
  List<ConsumptionType> recordable;
  List<ConsumptionOption> optionList;
  List<ConsumptionRecord> records;
  List<Invoice> myInvoiceList;
  List<Invoice> allInvoices;
  List<DirectoryEntry> members;
  List<MembershipPlan> planList;
  List<ConsumptionType> typeList;
  Map<String, Invoice> invoiceById;

  final List<String> calls = [];
  ApiException? failWith;

  /// Fails the NEXT call to a named method, once: `failCalls['admit'] = ...`.
  /// `failWith` fails whichever call comes next, which is a blunt instrument
  /// when a screen makes several calls before the one under test.
  final Map<String, ApiException> failCalls = {};

  /// Closed periods the next [closePeriod] returns.
  List<ClosedPeriod> closeResult = [];

  void _record(String what) {
    calls.add(what);
    final named = failCalls.remove(what.split(' ').first);
    if (named != null) throw named;
    final f = failWith;
    if (f != null) {
      failWith = null;
      throw f;
    }
  }

  static Paged<T> _page<T>(List<T> items) => Paged(items, '');

  @override
  Future<Me> me() async {
    _record('me');
    return _me!;
  }

  @override
  Future<Paged<ConsumptionType>> myRecordableTypes({String? cursor}) async {
    _record('myRecordableTypes');
    return _page(recordable);
  }

  @override
  Future<Paged<ConsumptionOption>> options(
    String typeId, {
    String? cursor,
  }) async {
    _record('options type=$typeId');
    return _page(optionList);
  }

  @override
  Future<Paged<ConsumptionRecord>> myConsumption({String? cursor}) async {
    _record('myConsumption cursor=$cursor');
    return _page(records);
  }

  @override
  Future<Paged<Invoice>> myInvoices({String? cursor}) async {
    _record('myInvoices');
    return _page(myInvoiceList);
  }

  @override
  Future<Invoice> invoice(String id) async {
    _record('invoice id=$id');
    return invoiceById[id] ??
        [...myInvoiceList, ...allInvoices].firstWhere((i) => i.id == id);
  }

  @override
  Future<ConsumptionRecord> recordUse(
    String typeId, {
    required String occurredOn,
    String? subjectPersonId,
    String? variantCode,
    List<String> optionCodes = const [],
    required String idempotencyKey,
  }) async {
    _record(
      'recordUse type=$typeId on=$occurredOn subject=$subjectPersonId '
      'options=${optionCodes.join(",")} key=$idempotencyKey',
    );
    return ConsumptionRecord(
      id: 'rec-${calls.length}',
      typeId: typeId,
      subjectPersonId: subjectPersonId ?? _me?.personId ?? 'me',
      actorPersonId: _me?.personId ?? 'me',
      occurredOn: occurredOn,
      variantCode: variantCode,
      optionCodes: optionCodes,
      supersedesId: null,
      voided: false,
      recordedAt: '${occurredOn}T12:00:00Z',
    );
  }

  @override
  Future<ConsumptionRecord> correctRecord(
    String recordId, {
    bool voidIt = false,
    String? variantCode,
    List<String> optionCodes = const [],
    required String idempotencyKey,
  }) async {
    _record('correctRecord id=$recordId void=$voidIt key=$idempotencyKey');
    return records.firstWhere((r) => r.id == recordId);
  }

  @override
  Future<Absence> declareAbsence({
    required String from,
    required String to,
    String? subjectPersonId,
  }) async {
    _record('declareAbsence from=$from to=$to subject=$subjectPersonId');
    return Absence(id: 'abs', from: from, to: to, relievesPayment: false);
  }

  @override
  Future<Paged<DirectoryEntry>> directory({
    String? query,
    String? cursor,
  }) async {
    _record('directory q=$query');
    final q = (query ?? '').toLowerCase();
    return _page([
      for (final m in members)
        if (q.isEmpty ||
            m.displayName.toLowerCase().contains(q) ||
            m.number.toLowerCase().startsWith(q))
          m,
    ]);
  }

  @override
  Future<Paged<MembershipPlan>> plans({String? cursor}) async {
    _record('plans');
    return _page(planList);
  }

  @override
  Future<Person> registerPerson({required String displayName}) async {
    _record('registerPerson name=$displayName');
    return Person(id: 'person-${calls.length}', displayName: displayName);
  }

  @override
  Future<Membership> admit({
    required String personId,
    required String planId,
    required String number,
    required String idempotencyKey,
  }) async {
    _record(
      'admit person=$personId plan=$planId number=$number '
      'key=$idempotencyKey',
    );
    return Membership(
      id: 'm-${calls.length}',
      personId: personId,
      number: number,
      state: 'ACTIVE',
    );
  }

  @override
  Future<Membership> suspendMembership(
    String id, {
    required String reason,
    required String idempotencyKey,
  }) async {
    _record('suspendMembership id=$id reason=$reason key=$idempotencyKey');
    return Membership(id: id, personId: 'p', number: 'n', state: 'SUSPENDED');
  }

  @override
  Future<Membership> reinstateMembership(
    String id, {
    required String idempotencyKey,
  }) async {
    _record('reinstateMembership id=$id key=$idempotencyKey');
    return Membership(id: id, personId: 'p', number: 'n', state: 'ACTIVE');
  }

  @override
  Future<Membership> terminateMembership(
    String id, {
    required String idempotencyKey,
  }) async {
    _record('terminateMembership id=$id key=$idempotencyKey');
    return Membership(id: id, personId: 'p', number: 'n', state: 'TERMINATED');
  }

  @override
  Future<Paged<ConsumptionType>> consumptionTypes({String? cursor}) async {
    _record('consumptionTypes');
    return _page(typeList);
  }

  @override
  Future<Paged<ConsumptionRecord>> typeRecords(
    String typeId, {
    String? subjectPersonId,
    String? from,
    String? to,
    String? cursor,
  }) async {
    _record('typeRecords type=$typeId');
    return _page(records);
  }

  @override
  Future<List<ClosedPeriod>> closePeriod(
    String typeId, {
    required String from,
    required String to,
    required String idempotencyKey,
  }) async {
    _record('closePeriod type=$typeId from=$from to=$to key=$idempotencyKey');
    return closeResult;
  }

  @override
  Future<Paged<Invoice>> invoices({String? cursor}) async {
    _record('invoices');
    return _page(allInvoices);
  }

  @override
  Future<Invoice> issueInvoice(
    String id, {
    String? dueAt,
    required String idempotencyKey,
  }) async {
    _record('issueInvoice id=$id key=$idempotencyKey');
    final inv = await invoice(id);
    return Invoice(
      id: inv.id,
      number: 'INV/2026-27/000001',
      state: 'ISSUED',
      currency: inv.currency,
      issuedAt: '2026-10-22T00:00:00Z',
      dueAt: dueAt,
      totalAmount: inv.totalAmount,
      createdAt: inv.createdAt,
      balance: inv.totalAmount,
      charges: inv.charges,
      payments: const [],
    );
  }

  @override
  Future<Payment> payInvoice(
    String id, {
    required String method,
    required String amount,
    required String currency,
    required String idempotencyKey,
  }) async {
    _record(
      'payInvoice id=$id method=$method amount=$amount '
      'currency=$currency key=$idempotencyKey',
    );
    return Payment(
      id: 'pay-${calls.length}',
      method: method,
      amount: amount,
      currency: currency,
      receivedAt: '2026-10-22T00:00:00Z',
    );
  }
}

/// Builders so a test states only what it cares about.
Me meWith(Set<String> permissions, {String person = 'person-me'}) => Me(
  principalId: 'princ-me',
  kind: 'PERSONAL',
  personId: person,
  tenantId: 'tenant-1',
  permissions: permissions,
);

ConsumptionType typeNamed(
  String name, {
  String id = 'type-1',
  String mode = 'EITHER',
}) => ConsumptionType(
  id: id,
  name: name,
  obligates: false,
  recurrence: null,
  recordMode: mode,
  status: 'ACTIVE',
  correctionWindowDays: null,
);

Invoice invoiceOf(
  String id, {
  String state = 'ISSUED',
  String total = '2000.00',
  String? balance,
  String? number = 'INV/2026-27/000001',
}) => Invoice(
  id: id,
  number: state == 'DRAFT' ? null : number,
  state: state,
  currency: 'INR',
  issuedAt: state == 'DRAFT' ? null : '2026-10-01T00:00:00Z',
  dueAt: state == 'DRAFT' ? null : '2026-10-31T00:00:00Z',
  totalAmount: state == 'DRAFT' ? null : total,
  createdAt: '2026-10-01T00:00:00.000000',
  balance: balance ?? (state == 'DRAFT' ? null : total),
  charges: const [
    Charge(
      id: 'c1',
      sourceType: 'CONSUMPTION',
      description: 'Dinner, 2026-09-01 to 2026-09-30',
      components: [
        Component(kind: 'USAGE_FEE', amount: '2000.00', currency: 'INR'),
      ],
    ),
  ],
  payments: const [],
);
