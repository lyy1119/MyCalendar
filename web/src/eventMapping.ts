/**
 * Conversion between the API's event shape, FullCalendar's event shape and the
 * editable draft used by the dialog.
 *
 * The three differ on purpose:
 *  - the API stores absolute instants plus a calendar date for all-day events,
 *  - FullCalendar wants Date objects for timed events and a bare date string
 *    for all-day ones,
 *  - the dialog edits plain strings coming from native date/time inputs.
 */

import type { EventInput as FullCalendarEventInput } from '@fullcalendar/core'
import type { ApiCategory, ApiEvent, EventInput } from './api'
import {
  fromUnixSeconds,
  toDateOnly,
  toDateTimeLocal,
  parseDateOnly,
  parseDateTimeLocal,
  toUnixSeconds,
} from './time'

/**
 * How long an event with no stored end is drawn.
 *
 * The API treats a null end_at as a single instant, which is correct but
 * invisible, so the grid gives it a small block. This is presentation only and
 * is never written back.
 */
const OPEN_ENDED_MINUTES = 15

/** Default length of a newly created timed event. */
const DEFAULT_NEW_MINUTES = 60

/** Matches the server's per-event limit. */
const MAX_REMINDERS = 50

export interface EventDraft {
  /** null while creating. */
  id: number | null
  title: string
  notes: string
  /** Free text; an emptied box is sent as null, not as "". */
  location: string
  isAllDay: boolean
  /** YYYY-MM-DD, used when isAllDay. */
  eventDate: string
  /** datetime-local value, used when !isAllDay. */
  start: string
  /** datetime-local value, or '' for an event with no end. */
  end: string
  isTask: boolean
  isCompleted: boolean
  /** The event's single category. */
  categoryID: number
  /**
   * Reminder times as `datetime-local` strings, in the order they are shown.
   *
   * The API stores absolute instants, so the strings are converted on save;
   * they stay strings here because that is what the native input works with.
   */
  reminders: string[]
}

/** Convenience offsets offered next to the reminder field. */
export const REMINDER_OFFSETS: { label: string; minutes: number }[] = [
  { label: '提前 5 分钟', minutes: 5 },
  { label: '提前 10 分钟', minutes: 10 },
  { label: '提前 30 分钟', minutes: 30 },
  { label: '提前 1 小时', minutes: 60 },
  { label: '提前 1 天', minutes: 1440 },
]

/**
 * The instant a reminder offset is measured from: the event's own start.
 *
 * For an all-day event that is local midnight of its date, which is the only
 * anchor the schema gives it.
 */
export function draftStart(draft: EventDraft): Date | null {
  return draft.isAllDay ? parseDateOnly(draft.eventDate) : parseDateTimeLocal(draft.start)
}

const INK = '#1f1f1f'

/** Splits #rgb or #rrggbb into three 0-255 channels, or null when unparseable. */
function channels(hex: string): [number, number, number] | null {
  const match = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(hex.trim())
  if (!match) return null
  let digits = match[1]
  if (digits.length === 3) {
    digits = digits
      .split('')
      .map((c) => c + c)
      .join('')
  }
  return [
    parseInt(digits.slice(0, 2), 16),
    parseInt(digits.slice(2, 4), 16),
    parseInt(digits.slice(4, 6), 16),
  ]
}

/** Rec. 709 relative luminance, the basis of the WCAG contrast ratio. */
function luminance(hex: string): number | null {
  const rgb = channels(hex)
  if (!rgb) return null
  const [r, g, b] = rgb.map((value) => {
    const v = value / 255
    return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

/** WCAG contrast ratio between two opaque colours, 1 (same) to 21 (black/white). */
export function contrastRatio(a: string, b: string): number {
  const la = luminance(a)
  const lb = luminance(b)
  if (la === null || lb === null) return 1
  const [hi, lo] = la > lb ? [la, lb] : [lb, la]
  return (hi + 0.05) / (lo + 0.05)
}

/**
 * Picks white or near-black text for a background colour, whichever actually
 * contrasts more.
 *
 * The category colours are chosen by the user, so they can be anything from
 * near-white to near-black. A luminance threshold ("light background, dark
 * text") is a guess; comparing the two real contrast ratios is not, and it
 * puts a mid blue such as #1a73e8 on white text (4.5:1) rather than a dim
 * 3.2:1.
 */
export function readableTextOn(hex: string): string {
  const white = contrastRatio(hex, '#ffffff')
  const ink = contrastRatio(hex, INK)
  return ink > white ? INK : '#ffffff'
}

/**
 * Moves a colour towards black (amount < 0) or white (amount > 0).
 *
 * Used for the event border: a fill alone leaves a very light category (white,
 * pale yellow) indistinguishable from the calendar background in the light
 * theme, and a very dark one (near-black) invisible in the dark theme. A border
 * that is slightly darker than the fill gives every colour an edge in both
 * themes without changing what colour the block reads as.
 */
export function shadeColor(hex: string, amount: number): string {
  const rgb = channels(hex)
  if (!rgb) return hex
  const target = amount < 0 ? 0 : 255
  const ratio = Math.min(Math.abs(amount), 1)
  return (
    '#' +
    rgb
      .map((value) => Math.round(value + (target - value) * ratio))
      .map((value) => value.toString(16).padStart(2, '0'))
      .join('')
  )
}

/**
 * The border for a filled event block.
 *
 * A fill alone leaves a very light category (white, pale yellow) lost against
 * the light theme's white page and a very dark one lost against the dark
 * theme's near-black page. The border therefore moves *away* from the fill --
 * lighter for dark fills, darker for light ones -- so every colour keeps an
 * edge in both themes without changing what colour the block reads as.
 */
function borderOn(hex: string): string {
  const l = luminance(hex)
  if (l === null) return hex
  return shadeColor(hex, l < 0.2 ? 0.3 : -0.22)
}

/** The background, border and text colours for an event, if any. */
function eventColors(event: ApiEvent, categories: Map<number, ApiCategory>) {
  // Tasks keep their own look -- teal, and bright green when done -- because
  // their colour carries state rather than a grouping.
  if (event.is_task) return {}
  const color = categories.get(event.category_id)?.color
  if (!color) return {}
  return {
    backgroundColor: color,
    borderColor: borderOn(color),
    textColor: readableTextOn(color),
  }
}

/** Build a FullCalendar event from an API event. */
export function toCalendarEvent(event: ApiEvent, categories: Map<number, ApiCategory> = new Map()): FullCalendarEventInput {
  const classNames: string[] = []
  if (event.is_task) classNames.push('mc-task')
  if (event.is_completed) classNames.push('mc-done')

  // The chip renderer needs these, and they are not part of FullCalendar's
  // event shape.
  const extendedProps = { location: event.location, notes: event.notes }

  if (event.is_all_day && event.event_date) {
    return {
      id: String(event.id),
      title: event.title,
      // A date string keeps all-day events free of any timezone drift.
      start: event.event_date,
      allDay: true,
      classNames,
      extendedProps,
      ...eventColors(event, categories),
    }
  }

  const start = fromUnixSeconds(event.start_at)
  const end =
    event.end_at !== null
      ? fromUnixSeconds(event.end_at)
      : new Date(start.getTime() + OPEN_ENDED_MINUTES * 60_000)

  return {
    id: String(event.id),
    title: event.title,
    start,
    end,
    allDay: false,
    classNames,
    extendedProps,
    ...eventColors(event, categories),
  }
}

/** The dialog draft for an existing event. */
export function draftFromEvent(event: ApiEvent): EventDraft {
  const start = fromUnixSeconds(event.start_at)
  return {
    id: event.id,
    title: event.title,
    notes: event.notes ?? '',
    location: event.location ?? '',
    isAllDay: event.is_all_day,
    eventDate: event.event_date ?? toDateOnly(start),
    start: toDateTimeLocal(start),
    end: event.end_at !== null ? toDateTimeLocal(fromUnixSeconds(event.end_at)) : '',
    isTask: event.is_task,
    isCompleted: event.is_completed,
    categoryID: event.category_id,
    reminders: (event.reminders ?? []).map((r) => toDateTimeLocal(fromUnixSeconds(r.remind_at))),
  }
}

/** The dialog draft for a slot the user selected in the grid. */
export function draftForRange(start: Date, end: Date | null, allDay: boolean): EventDraft {
  const effectiveEnd = end ?? new Date(start.getTime() + DEFAULT_NEW_MINUTES * 60_000)
  return {
    id: null,
    title: '',
    notes: '',
    location: '',
    isAllDay: allDay,
    eventDate: toDateOnly(start),
    start: toDateTimeLocal(start),
    end: toDateTimeLocal(effectiveEnd),
    isTask: false,
    isCompleted: false,
    categoryID: 0,
    reminders: [],
  }
}

/**
 * Keep a draft coherent when the all-day switch is flipped.
 *
 * Going all-day drops the times and keeps the date; going timed brings the
 * date back at a friendly hour rather than at midnight, which is what someone
 * converting an all-day entry almost always wants.
 */
export function toggleAllDay(draft: EventDraft, isAllDay: boolean): EventDraft {
  if (isAllDay === draft.isAllDay) return draft

  if (isAllDay) {
    const start = parseDateTimeLocal(draft.start)
    return {
      ...draft,
      isAllDay: true,
      eventDate: start ? toDateOnly(start) : draft.eventDate,
    }
  }

  const date = parseDateOnly(draft.eventDate) ?? new Date()
  const start = new Date(date.getFullYear(), date.getMonth(), date.getDate(), 9, 0, 0, 0)
  const end = new Date(start.getTime() + DEFAULT_NEW_MINUTES * 60_000)
  return {
    ...draft,
    isAllDay: false,
    start: toDateTimeLocal(start),
    end: toDateTimeLocal(end),
  }
}

export type DraftValidation =
  | { ok: true; input: EventInput }
  | { ok: false; message: string }

/** Turn a draft into an API request body, validating it first. */
export function draftToInput(draft: EventDraft): DraftValidation {
  const title = draft.title.trim()
  if (title === '') return { ok: false, message: '标题不能为空' }
  if (title.length > 500) return { ok: false, message: '标题最多 500 个字符' }

  // Reminder times are validated together so a single bad row does not silently
  // disappear from the set that gets written.
  const reminderTimes: number[] = []
  for (let i = 0; i < draft.reminders.length; i++) {
    const raw = draft.reminders[i].trim()
    if (raw === '') continue
    const parsed = parseDateTimeLocal(raw)
    if (!parsed) return { ok: false, message: `第 ${i + 1} 个提醒时间格式不正确` }
    const seconds = toUnixSeconds(parsed)
    if (!reminderTimes.includes(seconds)) reminderTimes.push(seconds)
  }
  if (reminderTimes.length > MAX_REMINDERS) {
    return { ok: false, message: `最多只能设置 ${MAX_REMINDERS} 个提醒` }
  }
  // Sending the field always, even empty, is what makes removing the last
  // reminder actually clear it: the API treats reminders as a whole set.
  const reminders = reminderTimes.map((remind_at) => ({ remind_at }))

  const notes = draft.notes.trim()
  const location = draft.location.trim()
  const base: EventInput = {
    reminders,
    title,
    // An empty box means "not set", stored as NULL rather than as an empty
    // string, which would read back as a location that exists.
    notes: notes === '' ? null : notes,
    location: location === '' ? null : location,
    is_task: draft.isTask,
    // The server clears this anyway when the event is not a task.
    is_completed: draft.isTask ? draft.isCompleted : false,
    category_id: draft.categoryID,
  }

  if (draft.isAllDay) {
    const date = parseDateOnly(draft.eventDate)
    if (!date) return { ok: false, message: '全天日程需要一个有效日期' }
    return { ok: true, input: { ...base, is_all_day: true, event_date: toDateOnly(date) } }
  }

  const start = parseDateTimeLocal(draft.start)
  if (!start) return { ok: false, message: '开始时间格式不正确' }

  const trimmedEnd = draft.end.trim()
  const end = trimmedEnd === '' ? null : parseDateTimeLocal(trimmedEnd)
  if (trimmedEnd !== '' && !end) return { ok: false, message: '结束时间格式不正确' }
  if (end && end.getTime() < start.getTime()) {
    return { ok: false, message: '结束时间不能早于开始时间' }
  }

  return {
    ok: true,
    input: {
      ...base,
      is_all_day: false,
      start_at: toUnixSeconds(start),
      end_at: end ? toUnixSeconds(end) : null,
    },
  }
}
