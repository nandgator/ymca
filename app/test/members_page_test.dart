import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:ymca_app/api/models.dart';
import 'package:ymca_app/api/ymca_api.dart';
import 'package:ymca_app/ui/admit_member_page.dart';
import 'package:ymca_app/ui/members_page.dart';

import 'fake_ymca_api.dart';
import 'support.dart';

DirectoryEntry entry(String name, String number, {String person = ''}) =>
    DirectoryEntry(
      membershipId: 'm-$number',
      personId: person.isEmpty ? 'p-$number' : person,
      displayName: name,
      number: number,
      state: 'ACTIVE',
      planId: 'plan',
    );

FakeYmcaApi apiOf(List<DirectoryEntry> members) => FakeYmcaApi(
  me: meWith({'tenant:may_approve_membership'}),
  members: members,
  recordable: [typeNamed('Dinner', id: 'dinner')],
);

/// A directory whose answer to 'a' arrives AFTER its answer to 'ab'.
class SlowFirstApi extends FakeYmcaApi {
  SlowFirstApi() : super(me: meWith({'tenant:may_approve_membership'}));

  @override
  Future<Paged<DirectoryEntry>> directory({
    String? query,
    String? cursor,
  }) async {
    calls.add('directory q=$query');
    if (query == 'a') {
      await Future<void>.delayed(const Duration(milliseconds: 800));
      return Paged([entry('Stale Result', '001')], '');
    }
    return Paged([entry('Fresh Result', '002')], '');
  }
}

void main() {
  testWidgets('lists members with their number and state', (tester) async {
    await pumpScreen(
      tester,
      apiOf([entry('Alice', '001'), entry('Bob', '002')]),
      const MembersPage(),
    );

    expect(find.text('Alice'), findsOneWidget);
    expect(find.text('No. 001 · ACTIVE'), findsOneWidget);
    expect(find.text('Bob'), findsOneWidget);
  });

  testWidgets('says so when there is nobody', (tester) async {
    await pumpScreen(tester, apiOf([]), const MembersPage());
    expect(find.text('No members found'), findsOneWidget);
  });

  testWidgets('search waits for a pause, then asks the server', (tester) async {
    final api = apiOf([entry('Alice', '001'), entry('Bob', '002')]);
    await pumpScreen(tester, api, const MembersPage());
    api.calls.clear();

    await tester.enterText(find.byKey(const Key('member-search')), 'bo');
    await tester.pump(const Duration(milliseconds: 100));
    expect(api.calls, isEmpty, reason: 'still inside the debounce window');

    await tester.pump(const Duration(milliseconds: 400));
    await tester.pumpAndSettle();
    expect(api.calls, ['directory q=bo']);
    expect(find.text('Bob'), findsOneWidget);
    expect(find.text('Alice'), findsNothing);
  });

  testWidgets('a slow answer to an old search cannot replace a newer one', (
    tester,
  ) async {
    final api = SlowFirstApi();
    await pumpScreen(tester, api, const MembersPage());

    await tester.enterText(find.byKey(const Key('member-search')), 'a');
    await tester.pump(const Duration(milliseconds: 350)); // 'a' is in flight
    await tester.enterText(find.byKey(const Key('member-search')), 'ab');
    await tester.pump(const Duration(milliseconds: 350)); // 'ab' answers
    await tester.pump(const Duration(milliseconds: 900)); // 'a' answers, late
    await tester.pumpAndSettle();

    expect(find.text('Fresh Result'), findsOneWidget);
    expect(find.text('Stale Result'), findsNothing);
  });

  testWidgets('a failed load shows Retry', (tester) async {
    final api = apiOf([entry('Alice', '001')]);
    api.failCalls['directory'] = const ApiException(500, 'internal', 'boom');
    await pumpScreen(tester, api, const MembersPage());

    expect(find.text('Retry'), findsOneWidget);
    await tester.tap(find.text('Retry'));
    await tester.pumpAndSettle();
    expect(find.text('Alice'), findsOneWidget);
  });

  testWidgets('tapping a member offers to record a meal for them', (
    tester,
  ) async {
    final api = apiOf([entry('Alice', '001', person: 'person-alice')]);
    await pumpScreen(tester, api, const MembersPage());

    await tester.tap(find.text('Alice'));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('record-for-member')));
    await tester.pumpAndSettle();

    expect(find.text('Record a meal for Alice'), findsOneWidget);
  });

  testWidgets('Admit member opens the admission form', (tester) async {
    await pumpScreen(tester, apiOf([]), const MembersPage());
    await tester.tap(find.byKey(const Key('admit-member')));
    await tester.pumpAndSettle();
    expect(find.byType(AdmitMemberPage), findsOneWidget);
  });

  test('a member whose term ended is labelled so, and only while ACTIVE', () {
    DirectoryEntry entry(String state, {bool inTerm = true}) => DirectoryEntry(
      membershipId: 'm',
      personId: 'p',
      displayName: 'A',
      number: '1',
      state: state,
      planId: 'plan',
      inTerm: inTerm,
    );
    expect(entry('ACTIVE').stateLabel, 'ACTIVE');
    expect(entry('ACTIVE', inTerm: false).stateLabel, 'TERM ENDED');
    expect(entry('SUSPENDED', inTerm: false).stateLabel, 'SUSPENDED');
    final parsed = DirectoryEntry.fromJson({
      'membership_id': 'm',
      'person_id': 'p',
      'display_name': 'A',
      'number': '1',
      'state': 'ACTIVE',
      'plan_id': 'plan',
      'in_term': false,
    });
    expect(parsed.inTerm, isFalse);
    expect(
      DirectoryEntry.fromJson({
        'membership_id': 'm',
        'person_id': 'p',
        'display_name': 'A',
        'number': '1',
        'state': 'ACTIVE',
        'plan_id': 'plan',
      }).inTerm,
      isTrue,
    );
  });
}
