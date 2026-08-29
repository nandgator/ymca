package com.ymca.mess.network

import com.russhwolf.settings.Settings
import com.ymca.mess.model.Role

/**
 * Persists the bearer token issued by /auth/otp/verify plus enough of the
 * Actor to route the UI (role, hostel id) without a network round trip on
 * every app launch. Nothing else about the session is cached — entries,
 * bills, roster, etc. are always fetched fresh.
 */
class SessionStore {
    private val settings: Settings = Settings()

    var token: String?
        get() = settings.getStringOrNull(KEY_TOKEN)
        set(value) = setOrRemove(KEY_TOKEN, value)

    var role: Role?
        get() = settings.getStringOrNull(KEY_ROLE)?.let { runCatching { Role.valueOf(it) }.getOrNull() }
        set(value) = setOrRemove(KEY_ROLE, value?.name)

    var actorId: String?
        get() = settings.getStringOrNull(KEY_ACTOR_ID)
        set(value) = setOrRemove(KEY_ACTOR_ID, value)

    var hostelId: String?
        get() = settings.getStringOrNull(KEY_HOSTEL_ID)
        set(value) = setOrRemove(KEY_HOSTEL_ID, value)

    val isLoggedIn: Boolean get() = token != null

    fun clear() {
        settings.remove(KEY_TOKEN)
        settings.remove(KEY_ROLE)
        settings.remove(KEY_ACTOR_ID)
        settings.remove(KEY_HOSTEL_ID)
    }

    private fun setOrRemove(key: String, value: String?) {
        if (value == null) settings.remove(key) else settings.putString(key, value)
    }

    private companion object {
        const val KEY_TOKEN = "session_token"
        const val KEY_ROLE = "session_role"
        const val KEY_ACTOR_ID = "session_actor_id"
        const val KEY_HOSTEL_ID = "session_hostel_id"
    }
}
