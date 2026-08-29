package com.ymca.mess.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// Every class here mirrors a DTO in backend/internal/httpapi/dto.go and
// routes_*.go — field names and JSON keys must match exactly. If the
// backend DTO changes, update here too; there is no code generation
// linking the two, so this is a manually-maintained contract.

enum class Role { MEMBER, SECRETARY, CENTRAL_ADMIN }

enum class OtpChannel { EMAIL, SMS }

enum class MealType { BREAKFAST, DINNER }

enum class LeaveType { SHORT, LONG }

@Serializable
data class RequestOtpRequest(
    val role: String,
    val login_id: String,
    val channel: String
)

@Serializable
data class VerifyOtpRequest(
    val role: String,
    val login_id: String,
    val code: String
)

@Serializable
data class VerifyOtpResponse(
    val token: String,
    val role: String,
    val actor_id: String,
    val hostel_id: String? = null
)

@Serializable
data class SubmitEntryRequest(
    val date: String, // YYYY-MM-DD, must be today
    val meal_type: String,
    val optional_item_ids: List<String> = emptyList(),
    val non_veg: Boolean = false
)

@Serializable
data class EntryDto(
    val id: String,
    val member_id: String,
    val date: String,
    val meal_type: String,
    val optional_item_ids: List<String> = emptyList(),
    val non_veg: Boolean = false
)

@Serializable
data class RegisterLeaveRequest(
    val start_date: String,
    val end_date: String
)

@Serializable
data class LeaveDto(
    val id: String,
    val member_id: String,
    val start_date: String,
    val end_date: String,
    val type: String // SHORT or LONG
)

@Serializable
data class BillLineDto(
    val label: String,
    val amount_paise: Long,
    val amount_inr: String
)

@Serializable
data class BillDto(
    val member_id: String,
    val year: Int,
    val month: Int,
    val lines: List<BillLineDto>,
    val total_paise: Long,
    val total_inr: String
)

@Serializable
data class MemberDto(
    val id: String,
    val member_id: String,
    val name: String,
    val email: String? = null,
    val mobile: String? = null
)

@Serializable
data class AddMemberRequest(
    val member_id: String,
    val name: String,
    val email: String? = null,
    val mobile: String? = null
)

@Serializable
data class OptionalItemDto(
    val id: String,
    val name: String,
    val price_paise: Long,
    val price_inr: String
)

@Serializable
data class AddOptionalItemRequest(
    val name: String,
    val price_paise: Long
)

@Serializable
data class PolicyDto(
    val flat_monthly_fee_paise: Long,
    val non_veg_surcharge_paise: Long,
    val daily_deduction_paise: Long,
    val long_leave_threshold_days: Int,
    val menu_days: List<String>, // 7 entries, index 0 = Monday
    val optional_items: List<OptionalItemDto> = emptyList()
)

@Serializable
data class UpdatePolicyRequest(
    val flat_monthly_fee_paise: Long,
    val non_veg_surcharge_paise: Long,
    val daily_deduction_paise: Long,
    val long_leave_threshold_days: Int,
    val menu_days: List<String>
)

@Serializable
data class HostelDto(
    val id: String,
    val name: String
)

@Serializable
data class CreateHostelRequest(
    val name: String,
    val flat_monthly_fee_paise: Long,
    val non_veg_surcharge_paise: Long,
    val daily_deduction_paise: Long,
    val long_leave_threshold_days: Int,
    val menu_days: List<String>
)

@Serializable
data class SecretaryDto(
    val id: String,
    val hostel_id: String,
    val staff_id: String,
    val name: String
)

@Serializable
data class CreateSecretaryRequest(
    val hostel_id: String,
    val staff_id: String,
    val name: String,
    val email: String? = null,
    val mobile: String? = null
)

@Serializable
data class ApiErrorBody(
    val error: String
)
