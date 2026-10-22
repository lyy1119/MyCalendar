/**
 * Timezone-aware helpers.
 *
 * Every request carries the browser's IANA timezone as `tz`, and the calendar
 * is configured with the same zone. That shared choice is what lets the UI
 * work with plain local wall-clock components (`getFullYear`, `getHours`, ...)
 * instead of doing offset arithmetic by hand.
 */

/**
 * Weekday label for a day header.
 *
 * The header's `date` is a real Date already converted into the calendar's
 * zone, and the calendar runs in the browser's own zone, so the default
 * (local) formatting is the right one.
 */
export const WEEKDAY_SHORT = new Intl.DateTimeFormat('zh-CN', { weekday: 'short' })

/** The zone the API is queried with. */
export const BROWSER_TZ: string =
  Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'

export function toUnixSeconds(date: Date): number {
  return Math.floor(date.getTime() / 1000)
}

export function fromUnixSeconds(seconds: number): Date {
  return new Date(seconds * 1000)
}

function pad(value: number): string {
  return String(value).padStart(2, '0')
}

/** YYYY-MM-DD for a Date, in local wall-clock terms. */
export function toDateOnly(date: Date): string {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

/** YYYY-MM-DDTHH:mm for `<input type="datetime-local">`. */
export function toDateTimeLocal(date: Date): string {
  return `${toDateOnly(date)}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

/** Parse YYYY-MM-DD into a Date at local midnight. */
export function parseDateOnly(value: string): Date | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value.trim())
  if (!match) return null
  const date = new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3]), 0, 0, 0, 0)
  return Number.isNaN(date.getTime()) ? null : date
}

/**
 * Parse a `datetime-local` value.
 *
 * The value has no offset, so it is read as wall-clock time in the browser
 * zone, which is exactly the zone the calendar and the API are using.
 */
export function parseDateTimeLocal(value: string): Date | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(value.trim())
  if (!match) return null
  const date = new Date(
    Number(match[1]),
    Number(match[2]) - 1,
    Number(match[3]),
    Number(match[4]),
    Number(match[5]),
    0,
    0,
  )
  return Number.isNaN(date.getTime()) ? null : date
}

/** True when both instants fall on the same local calendar day. */
export function isSameDay(a: Date, b: Date): boolean {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  )
}

/**
 * The Monday of the week containing `date`, at local midnight.
 *
 * The calendar starts weeks on Monday (firstDay = 1), so "this week" in the UI
 * means this Monday-to-Sunday block, not whatever the browser's locale
 * considers the first day.
 */
export function startOfWeek(date: Date): Date {
  const start = new Date(date.getFullYear(), date.getMonth(), date.getDate())
  // getDay(): 0 is Sunday, so Sunday has to go back six days rather than one.
  const offset = (start.getDay() + 6) % 7
  start.setDate(start.getDate() - offset)
  return start
}

/** HH:mm for display. */
export function formatTime(date: Date): string {
  return `${pad(date.getHours())}:${pad(date.getMinutes())}`
}

/** A short human label such as "3月11日 13:00". */
export function formatDateTime(date: Date): string {
  return `${date.getMonth() + 1}月${date.getDate()}日 ${formatTime(date)}`
}
