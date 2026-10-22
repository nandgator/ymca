import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../api/http_ymca_api.dart';
import '../api/models.dart';
import '../api/ymca_api.dart';

/// Where the app is pointed: one server, one tenant, one credential.
///
/// The token is a development credential (ADR-106's dev authenticator). A real
/// identity provider's token belongs in secure storage, not in preferences;
/// that arrives with the provider, not before.
class Session {
  const Session({
    required this.baseUrl,
    required this.tenantId,
    required this.token,
  });

  final String baseUrl;
  final String tenantId;
  final String token;
}

typedef ApiFactory = YmcaApi Function(Session session);

/// Owns the session and what the server says the signed-in principal may do.
///
/// The UI renders from [me] — the permissions it is TOLD it holds — and
/// never from a role it inferred (A3.9). A 401 from any call is turned into a
/// sign-out by [handleError], so an expired token returns to the sign-in page
/// rather than leaving every screen showing the same error.
class AppController extends ChangeNotifier {
  AppController(this._prefs, {ApiFactory? apiFactory})
    : _apiFactory = apiFactory ?? _httpFactory;

  static YmcaApi _httpFactory(Session s) =>
      HttpYmcaApi(baseUrl: s.baseUrl, tenantId: s.tenantId, token: s.token);

  final SharedPreferences _prefs;
  final ApiFactory _apiFactory;

  Session? _session;
  YmcaApi? _api;
  Me? _me;
  bool _busy = false;
  String? _error;

  Session? get session => _session;
  YmcaApi? get api => _api;
  Me? get me => _me;
  bool get busy => _busy;
  String? get error => _error;
  bool get signedIn => _api != null && _me != null;

  static const _kBase = 'baseUrl', _kTenant = 'tenantId', _kToken = 'token';

  /// The last values typed on the sign-in page, so it can prefill them.
  String get lastBaseUrl => _prefs.getString(_kBase) ?? 'http://localhost:8000';
  String get lastTenantId => _prefs.getString(_kTenant) ?? '';

  /// Re-establishes a saved session, if there is one and it still works.
  Future<void> restore() async {
    final base = _prefs.getString(_kBase);
    final tenant = _prefs.getString(_kTenant);
    final token = _prefs.getString(_kToken);
    if (base == null || tenant == null || token == null) return;
    await signIn(
      Session(baseUrl: base, tenantId: tenant, token: token),
      remember: false,
    );
  }

  Future<void> signIn(Session session, {bool remember = true}) async {
    _busy = true;
    _error = null;
    notifyListeners();
    try {
      final api = _apiFactory(session);
      final me = await api.me();
      _session = session;
      _api = api;
      _me = me;
      if (remember) {
        await _prefs.setString(_kBase, session.baseUrl);
        await _prefs.setString(_kTenant, session.tenantId);
        await _prefs.setString(_kToken, session.token);
      }
    } on ApiException catch (e) {
      _error = e.userMessage;
      await _forget();
    } catch (_) {
      _error = 'Could not reach the server.';
      await _forget();
    } finally {
      _busy = false;
      notifyListeners();
    }
  }

  Future<void> refreshMe() async {
    final api = _api;
    if (api == null) return;
    try {
      _me = await api.me();
      notifyListeners();
    } on ApiException catch (e) {
      await handleError(e);
    }
  }

  Future<void> signOut() async {
    await _forget();
    await _prefs.remove(_kToken);
    notifyListeners();
  }

  /// Screens pass every [ApiException] through here. Returns true if it was an
  /// expired or refused credential and the user has been signed out.
  Future<bool> handleError(Object error) async {
    if (error is ApiException && error.unauthenticated) {
      _error = error.userMessage;
      await signOut();
      return true;
    }
    return false;
  }

  Future<void> _forget() async {
    _session = null;
    _api = null;
    _me = null;
  }
}
