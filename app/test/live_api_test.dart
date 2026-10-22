@Tags(['live'])
library;

import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:ymca_app/api/http_ymca_api.dart';
import 'package:ymca_app/api/models.dart';
import 'package:ymca_app/api/ymca_api.dart';
import 'package:ymca_app/util/dates.dart';
import 'package:ymca_app/util/idempotency.dart';

/// The Dart half of the cross-language contract test.
///
/// It talks to a REAL server — the Go test `TestFlutterClientAgreesWithTheServer`
/// boots one on the production routes, seeds a hostel, and runs this file with
/// the addresses and tokens in the environment. A model that reads a key the
/// server does not write, or an endpoint the client spells differently, fails
/// here and nowhere else: the fake the widget tests use agrees with the client
/// by construction, so only this can catch the client and the server drifting.
///
/// Run on its own it skips, loudly.
String? _env(String k) {
  final v = Platform.environment[k];
  return (v == null || v.isEmpty) ? null : v;
}

HttpYmcaApi _as(String token) => HttpYmcaApi(
  baseUrl: _env('YMCA_URL')!,
  tenantId: _env('YMCA_TENANT')!,
  token: token,
);

void main() {
  final live = _env('YMCA_URL') != null;
  if (!live) {
    test(
      'live API contract',
      () {},
      skip:
          'YMCA_URL not set: run via the Go test '
          'TestFlutterClientAgreesWithTheServer',
    );
    return;
  }

  final admin = _as(_env('YMCA_ADMIN_TOKEN')!);
  final alice = _as(_env('YMCA_ALICE_TOKEN')!);
  final desk = _as(_env('YMCA_DESK_TOKEN')!);
  final breakfast = _env('YMCA_BREAKFAST_ID')!;
  final dinner = _env('YMCA_DINNER_ID')!;

  test('/me reports what each principal may do', () async {
    final a = await admin.me();
    expect(a.isAdmin, isTrue);
    expect(a.canManageFinance, isTrue);
    expect(a.canReadFinance, isTrue);
    expect(a.tenantId, _env('YMCA_TENANT'));

    final m = await alice.me();
    expect(m.permissions, isEmpty, reason: 'a plain member holds nothing');
    expect(m.personId, _env('YMCA_ALICE_PERSON'));

    final d = await desk.me();
    expect(d.canAdmit, isTrue);
    expect(d.isAdmin, isFalse);
  });

  test('a member sees what they may record, and its menu', () async {
    final types = await alice.myRecordableTypes();
    expect(
      types.items.map((t) => t.name),
      containsAll(['Breakfast', 'Dinner']),
    );
    final dinnerType = types.items.firstWhere((t) => t.name == 'Dinner');
    expect(dinnerType.obligates, isTrue);
    expect(dinnerType.recurrence, 'DAILY');
    expect(dinnerType.allowsSelf, isTrue);

    final menu = await alice.options(breakfast);
    expect(menu.items.single.code, 'EGG');
    expect(menu.items.single.amount, '15.00');
    expect(menu.items.single.currency, 'INR');
  });

  test('recording is idempotent and returns the wire shape', () async {
    final key = newIdempotencyKey();
    final first = await alice.recordUse(
      breakfast,
      occurredOn: today(),
      optionCodes: ['EGG'],
      idempotencyKey: key,
    );
    expect(first.optionCodes, ['EGG']);
    expect(first.voided, isFalse);
    expect(first.occurredOn, today());

    final again = await alice.recordUse(
      breakfast,
      occurredOn: today(),
      optionCodes: ['EGG'],
      idempotencyKey: key,
    );
    expect(again.id, first.id, reason: 'a retry is the same meal');

    await expectLater(
      alice.recordUse(
        breakfast,
        occurredOn: today(),
        idempotencyKey: newIdempotencyKey(),
      ),
      throwsA(
        isA<ApiException>()
            .having((e) => e.code, 'code', 'already_recorded')
            .having((e) => e.status, 'status', 409),
      ),
    );

    final mine = await alice.myConsumption();
    expect(mine.items.map((r) => r.id), contains(first.id));
    expect(mine.hasMore, isFalse);
  });

  test('an administrator corrects a record by voiding it', () async {
    final rec = await alice.recordUse(
      dinner,
      occurredOn: daysAgo(1),
      idempotencyKey: newIdempotencyKey(),
    );
    final voided = await admin.correctRecord(
      rec.id,
      voidIt: true,
      idempotencyKey: newIdempotencyKey(),
    );
    expect(voided.voided, isTrue);
    expect(voided.supersedesId, rec.id);
  });

  test('the front desk finds members and admits one', () async {
    final found = await desk.directory(query: 'alice');
    expect(found.items.single.displayName, 'alice');
    expect(found.items.single.personId, _env('YMCA_ALICE_PERSON'));

    final plans = await desk.plans();
    final plan = plans.items.firstWhere((p) => p.open);

    final person = await desk.registerPerson(displayName: 'Dev Newcomer');
    final key = newIdempotencyKey();
    final m = await desk.admit(
      personId: person.id,
      planId: plan.id,
      number: 'LIVE-1',
      idempotencyKey: key,
    );
    expect(m.state, 'ACTIVE');
    final again = await desk.admit(
      personId: person.id,
      planId: plan.id,
      number: 'LIVE-1',
      idempotencyKey: key,
    );
    expect(
      again.id,
      m.id,
      reason: 'a retried admission is the same membership',
    );

    await expectLater(
      desk.admit(
        personId: person.id,
        planId: plan.id,
        number: 'LIVE-1',
        idempotencyKey: newIdempotencyKey(),
      ),
      throwsA(
        isA<ApiException>().having(
          (e) => e.code,
          'code',
          anyOf('membership_number_taken', 'already_active_member'),
        ),
      ),
    );
  });

  test('a member declares an absence', () async {
    final a = await alice.declareAbsence(from: '2026-12-01', to: '2026-12-08');
    expect(
      a.relievesPayment,
      isTrue,
      reason: 'eight days is over the threshold',
    );
    expect(a.from, '2026-12-01');
  });

  test(
    'invoices: drafts, issued and paid parse, with charges and payments',
    () async {
      final all = await admin.invoices();
      final byState = {for (final i in all.items) i.id: i.state};
      expect(byState[_env('YMCA_INV_DRAFT')], 'DRAFT');
      expect(byState[_env('YMCA_INV_ISSUED')], 'ISSUED');
      expect(byState[_env('YMCA_INV_PAID')], 'PAID');

      final issued = await admin.invoice(_env('YMCA_INV_ISSUED')!);
      expect(issued.number, matches(RegExp(r'^INV/\d{4}-\d{2}/\d{6}$')));
      expect(issued.totalAmount, '2000.00');
      expect(issued.balance, '1500.00');
      expect(issued.charges.single.sourceType, 'CONSUMPTION');
      expect(issued.charges.single.components.single.kind, 'USAGE_FEE');
      expect(issued.payments.single.amount, '500.00');

      final draft = await admin.invoice(_env('YMCA_INV_DRAFT')!);
      expect(draft.isDraft, isTrue);
      expect(draft.number, isNull);
      expect(draft.totalAmount, isNull);

      // The payer sees their issued bill, and not the draft.
      final mine = await alice.myInvoices();
      expect(mine.items.map((i) => i.id), [_env('YMCA_INV_ISSUED')]);
      final detail = await alice.invoice(_env('YMCA_INV_ISSUED')!);
      expect(detail.balance, '1500.00');
    },
  );

  test('staff issue and pay, and a retry is not a second payment', () async {
    final draft = _env('YMCA_INV_FOR_ISSUE')!;
    final k = newIdempotencyKey();
    final issued = await admin.issueInvoice(draft, idempotencyKey: k);
    expect(issued.isIssued, isTrue);
    expect(issued.number, isNotNull);
    final replay = await admin.issueInvoice(draft, idempotencyKey: k);
    expect(replay.number, issued.number);

    final pk = newIdempotencyKey();
    final p = await admin.payInvoice(
      draft,
      method: 'CASH',
      amount: issued.totalAmount!,
      currency: 'INR',
      idempotencyKey: pk,
    );
    final p2 = await admin.payInvoice(
      draft,
      method: 'CASH',
      amount: issued.totalAmount!,
      currency: 'INR',
      idempotencyKey: pk,
    );
    expect(p2.id, p.id);
    expect((await admin.invoice(draft)).isPaid, isTrue);
  });

  test('closing a period parses the server\'s own answer', () {
    final raw = jsonDecode(_env('YMCA_CLOSE_JSON')!) as Map<String, dynamic>;
    final closed = [
      for (final c in raw['closed'] as List) ClosedPeriod.fromJson(c as Json),
    ];
    expect(closed, isNotEmpty);
    expect(closed.first.obligatedUnits, 20);
    expect(closed.first.invoiceId, isNotNull);
  });

  test('refusals carry A3.4 codes the client can act on', () async {
    await expectLater(
      alice.invoices(),
      throwsA(
        isA<ApiException>()
            .having((e) => e.status, 'status', 403)
            .having((e) => e.code, 'code', 'forbidden')
            .having((e) => e.forbidden, 'forbidden', isTrue),
      ),
    );
    final stranger = _as('not-a-token');
    await expectLater(
      stranger.me(),
      throwsA(
        isA<ApiException>().having(
          (e) => e.unauthenticated,
          'unauthenticated',
          isTrue,
        ),
      ),
    );
    await expectLater(
      admin.invoice('00000000-0000-0000-0000-00000000dead'),
      throwsA(
        isA<ApiException>().having((e) => e.status, 'status', anyOf(403, 404)),
      ),
    );
  });

  test('suspend, reinstate and terminate go through, in order', () async {
    final m = _env('YMCA_CAROL_MEMBERSHIP')!;
    final suspended = await admin.suspendMembership(
      m,
      reason: 'ARREARS',
      idempotencyKey: newIdempotencyKey(),
    );
    expect(suspended.state, 'SUSPENDED');

    final listed = await desk.directory(query: 'carol');
    expect(listed.items.single.state, 'SUSPENDED');

    await expectLater(
      admin.suspendMembership(
        m,
        reason: 'CONDUCT',
        idempotencyKey: newIdempotencyKey(),
      ),
      throwsA(
        isA<ApiException>()
            .having((e) => e.code, 'code', 'membership_wrong_state')
            .having((e) => e.status, 'status', 409),
      ),
    );

    final back = await admin.reinstateMembership(
      m,
      idempotencyKey: newIdempotencyKey(),
    );
    expect(back.state, 'ACTIVE');

    final key = newIdempotencyKey();
    final ended = await admin.terminateMembership(m, idempotencyKey: key);
    expect(ended.state, 'TERMINATED');
    final replay = await admin.terminateMembership(m, idempotencyKey: key);
    expect(
      replay.state,
      'TERMINATED',
      reason: 'a retried terminate is a replay',
    );
    await expectLater(
      admin.terminateMembership(m, idempotencyKey: newIdempotencyKey()),
      throwsA(
        isA<ApiException>().having(
          (e) => e.code,
          'code',
          'membership_wrong_state',
        ),
      ),
    );
  });

  test('only an administrator may change a membership', () async {
    await expectLater(
      desk.suspendMembership(
        _env('YMCA_CAROL_MEMBERSHIP')!,
        reason: 'ARREARS',
        idempotencyKey: newIdempotencyKey(),
      ),
      throwsA(
        isA<ApiException>().having((e) => e.forbidden, 'forbidden', isTrue),
      ),
    );
  });
}
