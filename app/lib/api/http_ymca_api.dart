import 'dart:convert';

import 'package:http/http.dart' as http;

import 'models.dart';
import 'ymca_api.dart';

/// [YmcaApi] over HTTP. One instance is one (server, tenant, credential).
class HttpYmcaApi implements YmcaApi {
  HttpYmcaApi({
    required String baseUrl,
    required this.tenantId,
    required this.token,
    http.Client? client,
  }) : _base = baseUrl.replaceAll(RegExp(r'/+$'), ''),
       _client = client ?? http.Client();

  final String _base;
  final String tenantId;
  final String token;
  final http.Client _client;

  Uri _uri(String path, [Map<String, String?> query = const {}]) {
    final q = {
      for (final e in query.entries)
        if (e.value != null && e.value!.isNotEmpty) e.key: e.value!,
    };
    return Uri.parse('$_base/api/v1/t/$tenantId$path')
        .replace(queryParameters: q.isEmpty ? null : q);
  }

  Map<String, String> _headers({String? idempotencyKey, bool body = false}) => {
    'Authorization': 'Bearer $token',
    'Accept': 'application/json',
    if (body) 'Content-Type': 'application/json',
    'Idempotency-Key': ?idempotencyKey,
  };

  Json _decode(http.Response r) {
    final text = utf8.decode(r.bodyBytes);
    final Object? parsed = text.isEmpty ? null : jsonDecode(text);
    if (r.statusCode >= 200 && r.statusCode < 300) {
      return (parsed as Json?) ?? <String, dynamic>{};
    }
    final e = (parsed is Map ? parsed['error'] : null) as Map?;
    throw ApiException(
      r.statusCode,
      (e?['code'] as String?) ?? 'unknown',
      (e?['message'] as String?) ?? r.reasonPhrase ?? '',
    );
  }

  Future<Json> _get(
    String path, [
    Map<String, String?> query = const {},
  ]) async =>
      _decode(await _client.get(_uri(path, query), headers: _headers()));

  Future<Json> _post(String path, Object? body, {String? key}) async => _decode(
    await _client.post(
      _uri(path),
      headers: _headers(idempotencyKey: key, body: true),
      body: jsonEncode(body ?? <String, dynamic>{}),
    ),
  );

  @override
  Future<Me> me() async => Me.fromJson(await _get('/me'));

  @override
  Future<Paged<ConsumptionType>> myRecordableTypes({String? cursor}) async =>
      Paged.fromJson(
        await _get('/me/consumption-types', {'cursor': cursor}),
        ConsumptionType.fromJson,
      );

  @override
  Future<Paged<ConsumptionOption>> options(
    String typeId, {
    String? cursor,
  }) async => Paged.fromJson(
    await _get('/consumption-types/$typeId/options', {'cursor': cursor}),
    ConsumptionOption.fromJson,
  );

  @override
  Future<Paged<ConsumptionRecord>> myConsumption({String? cursor}) async =>
      Paged.fromJson(
        await _get('/me/consumption', {'cursor': cursor}),
        ConsumptionRecord.fromJson,
      );

  @override
  Future<Paged<Invoice>> myInvoices({String? cursor}) async => Paged.fromJson(
    await _get('/me/invoices', {'cursor': cursor}),
    Invoice.fromJson,
  );

  @override
  Future<Invoice> invoice(String id) async =>
      Invoice.fromJson(await _get('/invoices/$id'));

  @override
  Future<ConsumptionRecord> recordUse(
    String typeId, {
    required String occurredOn,
    String? subjectPersonId,
    String? variantCode,
    List<String> optionCodes = const [],
    required String idempotencyKey,
  }) async => ConsumptionRecord.fromJson(
    await _post('/consumption-types/$typeId/records', {
      'occurred_on': occurredOn,
      'subject_person_id': ?subjectPersonId,
      'variant_code': ?variantCode,
      if (optionCodes.isNotEmpty) 'option_codes': optionCodes,
    }, key: idempotencyKey),
  );

  @override
  Future<ConsumptionRecord> correctRecord(
    String recordId, {
    bool voidIt = false,
    String? variantCode,
    List<String> optionCodes = const [],
    required String idempotencyKey,
  }) async => ConsumptionRecord.fromJson(
    await _post(
      '/consumption-records/$recordId/correction',
      voidIt
          ? {'void': true}
          : {
              'variant_code': ?variantCode,
              if (optionCodes.isNotEmpty) 'option_codes': optionCodes,
            },
      key: idempotencyKey,
    ),
  );

  @override
  Future<Absence> declareAbsence({
    required String from,
    required String to,
    String? subjectPersonId,
  }) async => Absence.fromJson(
    await _post('/absences', {
      'from': from,
      'to': to,
      'subject_person_id': ?subjectPersonId,
    }),
  );

  @override
  Future<Paged<DirectoryEntry>> directory({
    String? query,
    String? cursor,
  }) async => Paged.fromJson(
    await _get('/memberships', {'q': query, 'cursor': cursor}),
    DirectoryEntry.fromJson,
  );

  @override
  Future<Paged<MembershipPlan>> plans({String? cursor}) async => Paged.fromJson(
    await _get('/membership-plans', {'cursor': cursor}),
    MembershipPlan.fromJson,
  );

  @override
  Future<Person> registerPerson({required String displayName}) async =>
      Person.fromJson(await _post('/people', {'display_name': displayName}));

  @override
  Future<Membership> admit({
    required String personId,
    required String planId,
    required String number,
    required String idempotencyKey,
  }) async => Membership.fromJson(
    await _post('/memberships', {
      'person_id': personId,
      'plan_id': planId,
      'number': number,
    }, key: idempotencyKey),
  );

  @override
  Future<Membership> suspendMembership(
    String id, {
    required String reason,
    required String idempotencyKey,
  }) async => Membership.fromJson(
    await _post('/memberships/$id/suspend', {
      'reason': reason,
    }, key: idempotencyKey),
  );

  @override
  Future<Membership> reinstateMembership(
    String id, {
    required String idempotencyKey,
  }) async => Membership.fromJson(
    await _post('/memberships/$id/reinstate', null, key: idempotencyKey),
  );

  @override
  Future<Membership> terminateMembership(
    String id, {
    required String idempotencyKey,
  }) async => Membership.fromJson(
    await _post('/memberships/$id/terminate', null, key: idempotencyKey),
  );

  @override
  Future<Paged<ConsumptionType>> consumptionTypes({String? cursor}) async =>
      Paged.fromJson(
        await _get('/consumption-types', {'cursor': cursor}),
        ConsumptionType.fromJson,
      );

  @override
  Future<Paged<ConsumptionRecord>> typeRecords(
    String typeId, {
    String? subjectPersonId,
    String? from,
    String? to,
    String? cursor,
  }) async => Paged.fromJson(
    await _get('/consumption-types/$typeId/records', {
      'subject': subjectPersonId,
      'from': from,
      'to': to,
      'cursor': cursor,
    }),
    ConsumptionRecord.fromJson,
  );

  @override
  Future<List<ClosedPeriod>> closePeriod(
    String typeId, {
    required String from,
    required String to,
    required String idempotencyKey,
  }) async {
    final out = await _post('/consumption-types/$typeId/close', {
      'from': from,
      'to': to,
    }, key: idempotencyKey);
    return [
      for (final c in (out['closed'] as List? ?? const []))
        ClosedPeriod.fromJson(c as Json),
    ];
  }

  @override
  Future<Paged<Invoice>> invoices({String? cursor}) async => Paged.fromJson(
    await _get('/invoices', {'cursor': cursor}),
    Invoice.fromJson,
  );

  @override
  Future<Invoice> issueInvoice(
    String id, {
    String? dueAt,
    required String idempotencyKey,
  }) async => Invoice.fromJson(
    await _post(
      '/invoices/$id/issue',
      dueAt == null ? null : {'due_at': dueAt},
      key: idempotencyKey,
    ),
  );

  @override
  Future<Payment> payInvoice(
    String id, {
    required String method,
    required String amount,
    required String currency,
    required String idempotencyKey,
  }) async => Payment.fromJson(
    await _post('/invoices/$id/payments', {
      'method': method,
      'amount': amount,
      'currency': currency,
    }, key: idempotencyKey),
  );
}
