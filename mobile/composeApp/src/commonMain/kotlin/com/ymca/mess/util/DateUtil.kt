package com.ymca.mess.util

import kotlinx.datetime.Clock
import kotlinx.datetime.LocalDate
import kotlinx.datetime.TimeZone
import kotlinx.datetime.todayIn

fun todayIso(): String = Clock.System.todayIn(TimeZone.currentSystemDefault()).toString() // LocalDate.toString() is YYYY-MM-DD

fun todayLocalDate(): LocalDate = Clock.System.todayIn(TimeZone.currentSystemDefault())

/** Formats YYYY-MM-DD as "Mon DD" for compact list display. Falls back to the raw string on parse failure. */
fun formatShort(isoDate: String): String = runCatching {
    val d = LocalDate.parse(isoDate)
    val months = arrayOf("Jan","Feb","Mar","Apr","May","Jun","Jul","Aug","Sep","Oct","Nov","Dec")
    "${months[d.monthNumber - 1]} ${d.dayOfMonth}"
}.getOrDefault(isoDate)
