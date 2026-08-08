package com.ymca.mess.network

import com.ymca.mess.model.*
import io.ktor.client.HttpClient
import io.ktor.client.call.body
import io.ktor.client.plugins.contentnegotiation.ContentNegotiation
import io.ktor.client.plugins.logging.LogLevel
import io.ktor.client.plugins.logging.Logging
import io.ktor.client.request.HttpRequestBuilder
import io.ktor.client.request.delete
import io.ktor.client.request.get
import io.ktor.client.request.headers
import io.ktor.client.request.post
import io.ktor.client.request.put
import io.ktor.client.request.setBody
import io.ktor.client.statement.HttpResponse
import io.ktor.http.ContentType
import io.ktor.http.HttpStatusCode
import io.ktor.http.contentType
import io.ktor.http.isSuccess
import io.ktor.serialization.kotlinx.json.json
import kotlinx.serialization.json.Json

/** Thrown for any non-2xx response; [message] is the server's `error` field when present. */
class ApiException(override val message: String, val statusCode: Int) : Exception(message)

/**
 * Platform-specific "point at my local backend" default. Android's
 * emulator needs the special 10.0.2.2 alias to reach the host machine's
 * localhost; iOS's simulator can use localhost directly. See
 * androidMain/iosMain for the actuals. Override explicitly (ApiClient's
 * constructor param) once you have a real deployment URL.
 */
expect fun defaultApiBaseUrl(): String

/**
 * One client per app process, backed by [SessionStore] for the bearer
 * token. Every function here maps 1:1 to a route in
 * backend/internal/httpapi/routes_*.go — see that package if a shape here
 * looks surprising, it's the source of truth.
 *
 * [baseUrl] defaults to the local docker-compose backend for development.
 * Point it at your EC2 deployment's URL for a real build — see
 * mobile/README.md.
 */
class ApiClient(
    private val sessionStore: SessionStore,
    private val baseUrl: String = defaultApiBaseUrl()
) {
    private val json = Json { ignoreUnknownKeys = true; explicitNulls = false }

    private val http = HttpClient {
        install(ContentNegotiation) { json(json) }
        install(Logging) { level = LogLevel.INFO }
    }

    // --- auth ---------------------------------------------------------

    suspend fun requestOtp(role: Role, loginId: String, channel: OtpChannel) {
        post<RequestOtpRequest, Unit>("/auth/otp/request", RequestOtpRequest(role.name, loginId, channel.name), authed = false)
    }

    suspend fun verifyOtp(role: Role, loginId: String, code: String): VerifyOtpResponse {
        val resp = post<VerifyOtpRequest, VerifyOtpResponse>("/auth/otp/verify", VerifyOtpRequest(role.name, loginId, code), authed = false)
        sessionStore.token = resp.token
        sessionStore.role = Role.valueOf(resp.role)
        sessionStore.actorId = resp.actor_id
        sessionStore.hostelId = resp.hostel_id
        return resp
    }

    fun logout() = sessionStore.clear()

    // --- member ---------------------------------------------------------

    suspend fun submitEntry(req: SubmitEntryRequest): EntryDto = post("/member/entries", req)
    suspend fun listMyEntries(year: Int, month: Int): List<EntryDto> = get("/member/entries?year=$year&month=$month")
    suspend fun registerLeave(req: RegisterLeaveRequest): LeaveDto = post("/member/leaves", req)
    suspend fun listMyLeaves(): List<LeaveDto> = get("/member/leaves")
    suspend fun myBill(year: Int, month: Int): BillDto = get("/member/bill?year=$year&month=$month")
    suspend fun myPolicy(): PolicyDto = get("/member/policy")

    // --- secretary ---------------------------------------------------------

    suspend fun addMember(req: AddMemberRequest): MemberDto = post("/secretary/members", req)
    suspend fun listRoster(): List<MemberDto> = get("/secretary/members")
    suspend fun getPolicy(): PolicyDto = get("/secretary/policy")
    suspend fun updatePolicy(req: UpdatePolicyRequest): PolicyDto = put("/secretary/policy", req)
    suspend fun addOptionalItem(req: AddOptionalItemRequest): OptionalItemDto = post("/secretary/optional-items", req)
    suspend fun deactivateOptionalItem(itemId: String) {
        authedRequest<Unit> { http.delete("$baseUrl/secretary/optional-items/$itemId") { withAuth() } }
    }
    suspend fun hostelEntriesForDate(date: String): List<EntryDto> = get("/secretary/entries?date=$date")
    suspend fun hostelEntriesForMonth(year: Int, month: Int): List<EntryDto> = get("/secretary/entries?year=$year&month=$month")
    suspend fun runBilling(year: Int, month: Int): List<BillDto> = get("/secretary/billing?year=$year&month=$month")

    // --- admin ---------------------------------------------------------

    suspend fun createHostel(req: CreateHostelRequest): HostelDto = post("/admin/hostels", req)
    suspend fun listHostels(): List<HostelDto> = get("/admin/hostels")
    suspend fun createSecretary(req: CreateSecretaryRequest): SecretaryDto = post("/admin/secretaries", req)

    // --- plumbing ---------------------------------------------------------

    private suspend inline fun <reified T> get(path: String): T =
        authedRequest { http.get("$baseUrl$path") { withAuth() } }

    private suspend inline fun <reified B, reified T> post(path: String, body: B, authed: Boolean = true): T =
        authedRequest {
            http.post("$baseUrl$path") {
                contentType(ContentType.Application.Json)
                setBody(body)
                if (authed) withAuth()
            }
        }

    private suspend inline fun <reified B, reified T> put(path: String, body: B): T =
        authedRequest {
            http.put("$baseUrl$path") {
                contentType(ContentType.Application.Json)
                setBody(body)
                withAuth()
            }
        }

    private fun HttpRequestBuilder.withAuth() {
        sessionStore.token?.let { headers { append("Authorization", "Bearer $it") } }
    }

    private suspend inline fun <reified T> authedRequest(block: () -> HttpResponse): T {
        val response = block()
        if (!response.status.isSuccess()) {
            val body = runCatching { response.body<ApiErrorBody>() }.getOrNull()
            if (response.status == HttpStatusCode.Unauthorized) sessionStore.clear()
            throw ApiException(body?.error ?: "request failed (${response.status.value})", response.status.value)
        }
        if (T::class == Unit::class) {
            @Suppress("UNCHECKED_CAST")
            return Unit as T
        }
        return response.body()
    }
}
