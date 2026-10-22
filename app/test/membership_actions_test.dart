import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:ymca_app/api/models.dart';
import 'package:ymca_app/api/ymca_api.dart';
import 'package:ymca_app/ui/members_page.dart';

import 'fake_ymca_api.dart';
import 'support.dart';

DirectoryEntry entry(String name, String state) => DirectoryEntry(
  membershipId: 'm-$name',
  personId: 'p-$name',
  displayName: name,
  number: 'H-1',
  state: state,
  planId: 'plan',
);

FakeYmcaApi apiOf(Set<String> permissions, List<DirectoryEntry> members) =>
    FakeYmcaApi(
      me: meWith(permissions),
      members: members,
      recordable: [typeNamed('Dinner', id: 'dinner')],
    );

Future<void> openSheet(WidgetTester tester, String name) async {
  await tester.tap(find.text(name));
  await tester.pumpAndSettle();
}

void main() {
  const admin = {'tenant:admin', 'tenant:may_approve_membership'};

  testWidgets(
    'a front desk without admin sees no way to suspend or terminate',
    (tester) async {
      await pumpScreen(
        tester,
        apiOf({'tenant:may_approve_membership'}, [entry('Alice', 'ACTIVE')]),
        const MembersPage(),
      );
      await openSheet(tester, 'Alice');

      expect(find.byKey(const Key('record-for-member')), findsOneWidget);
      expect(find.byKey(const Key('suspend-member')), findsNothing);
      expect(find.byKey(const Key('terminate-member')), findsNothing);
    },
  );

  testWidgets('what is offered follows the membership\'s state', (
    tester,
  ) async {
    final api = apiOf(admin, [
      entry('Alice', 'ACTIVE'),
      entry('Bob', 'SUSPENDED'),
      entry('Carol', 'TERMINATED'),
    ]);
    await pumpScreen(tester, api, const MembersPage());

    await openSheet(tester, 'Alice');
    expect(find.byKey(const Key('suspend-member')), findsOneWidget);
    expect(find.byKey(const Key('reinstate-member')), findsNothing);
    expect(find.byKey(const Key('terminate-member')), findsOneWidget);
    await tester.tapAt(const Offset(10, 10)); // dismiss
    await tester.pumpAndSettle();

    await openSheet(tester, 'Bob');
    expect(find.byKey(const Key('suspend-member')), findsNothing);
    expect(find.byKey(const Key('reinstate-member')), findsOneWidget);
    expect(find.byKey(const Key('terminate-member')), findsOneWidget);
    await tester.tapAt(const Offset(10, 10));
    await tester.pumpAndSettle();

    await openSheet(tester, 'Carol');
    expect(find.byKey(const Key('suspend-member')), findsNothing);
    expect(find.byKey(const Key('reinstate-member')), findsNothing);
    expect(find.byKey(const Key('terminate-member')), findsNothing);
  });

  group('suspending', () {
    Future<void> openDialog(WidgetTester tester, FakeYmcaApi api) async {
      await pumpScreen(tester, api, const MembersPage());
      await openSheet(tester, 'Alice');
      await tester.tap(find.byKey(const Key('suspend-member')));
      await tester.pumpAndSettle();
    }

    Future<void> chooseReason(WidgetTester tester, String label) async {
      await tester.tap(find.byKey(const Key('suspend-reason')));
      await tester.pumpAndSettle();
      await tester.tap(find.text(label).last);
      await tester.pumpAndSettle();
    }

    testWidgets('says what it takes away, and needs a reason', (tester) async {
      final api = apiOf(admin, [entry('Alice', 'ACTIVE')]);
      await openDialog(tester, api);

      expect(find.textContaining('lose access'), findsOneWidget);
      FilledButton confirm() =>
          tester.widget<FilledButton>(find.byKey(const Key('confirm-suspend')));
      expect(confirm().onPressed, isNull);

      await chooseReason(tester, 'Arrears');
      expect(confirm().onPressed, isNotNull);
    });

    testWidgets('sends the reason and a key, then reloads the directory', (
      tester,
    ) async {
      final api = apiOf(admin, [entry('Alice', 'ACTIVE')]);
      await openDialog(tester, api);
      await chooseReason(tester, 'Conduct');
      api.calls.clear();
      await tester.tap(find.byKey(const Key('confirm-suspend')));
      await tester.pumpAndSettle();

      final call = api.calls.firstWhere(
        (c) => c.startsWith('suspendMembership'),
      );
      expect(call, contains('id=m-Alice'));
      expect(call, contains('reason=CONDUCT'));
      expect(api.calls, contains('directory q='));
    });

    testWidgets('a failed confirm stays open; the retry reuses the key', (
      tester,
    ) async {
      final api = apiOf(admin, [entry('Alice', 'ACTIVE')]);
      await openDialog(tester, api);
      await chooseReason(tester, 'Arrears');

      api.failCalls['suspendMembership'] = const ApiException(
        500,
        'internal',
        'the revocation did not happen',
      );
      await tester.tap(find.byKey(const Key('confirm-suspend')));
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('confirm-error')), findsOneWidget);

      await tester.tap(find.byKey(const Key('confirm-suspend')));
      await tester.pumpAndSettle();
      expect(
        keyOfCall(api, 'suspendMembership', 1),
        keyOfCall(api, 'suspendMembership', 0),
      );
    });

    testWidgets('a different reason after a failure is a different request', (
      tester,
    ) async {
      final api = apiOf(admin, [entry('Alice', 'ACTIVE')]);
      await openDialog(tester, api);
      await chooseReason(tester, 'Arrears');

      api.failCalls['suspendMembership'] = const ApiException(
        500,
        'internal',
        'x',
      );
      await tester.tap(find.byKey(const Key('confirm-suspend')));
      await tester.pumpAndSettle();
      await chooseReason(tester, 'Safeguarding');
      await tester.tap(find.byKey(const Key('confirm-suspend')));
      await tester.pumpAndSettle();

      expect(
        keyOfCall(api, 'suspendMembership', 1),
        isNot(keyOfCall(api, 'suspendMembership', 0)),
        reason:
            'the same key with a different reason is idempotency_key_reused',
      );
    });
  });

  testWidgets('terminating warns that it is final, and sends a key', (
    tester,
  ) async {
    final api = apiOf(admin, [entry('Alice', 'ACTIVE')]);
    await pumpScreen(tester, api, const MembersPage());
    await openSheet(tester, 'Alice');
    await tester.tap(find.byKey(const Key('terminate-member')));
    await tester.pumpAndSettle();

    expect(find.textContaining('cannot be undone'), findsOneWidget);
    await tester.tap(find.byKey(const Key('confirm-terminate')));
    await tester.pumpAndSettle();

    expect(callCount(api, 'terminateMembership'), 1);
  });

  testWidgets('reinstating says it is not instant', (tester) async {
    final api = apiOf(admin, [entry('Bob', 'SUSPENDED')]);
    await pumpScreen(tester, api, const MembersPage());
    await openSheet(tester, 'Bob');
    await tester.tap(find.byKey(const Key('reinstate-member')));
    await tester.pumpAndSettle();

    expect(find.textContaining('not instantly'), findsOneWidget);
    await tester.tap(find.byKey(const Key('confirm-reinstate')));
    await tester.pumpAndSettle();
    expect(callCount(api, 'reinstateMembership'), 1);
  });

  testWidgets('a wrong-state answer is explained plainly', (tester) async {
    final api = apiOf(admin, [entry('Alice', 'ACTIVE')]);
    await pumpScreen(tester, api, const MembersPage());
    await openSheet(tester, 'Alice');
    await tester.tap(find.byKey(const Key('terminate-member')));
    await tester.pumpAndSettle();

    api.failCalls['terminateMembership'] = const ApiException(
      409,
      'membership_wrong_state',
      'x',
    );
    await tester.tap(find.byKey(const Key('confirm-terminate')));
    await tester.pumpAndSettle();
    expect(
      find.text('That membership cannot be changed that way right now.'),
      findsOneWidget,
    );
  });
}
