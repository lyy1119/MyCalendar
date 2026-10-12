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
import type { ApiEvent, EventInput } from './api'
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
  isAllDay: boolean
  /** YYYY-MM-DD, used when isAllDay. */
  eventDate: string
  /** datetime-local value, used when !isAllDay. */
  start: string
  /** datetime-local value, or '' for an event with no end. */
  end: string
  isTask: boolean
  isCompleted: boolean
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

/** Build a FullCalendar event from an API event. */
export function toCalendarEvent(event: ApiEvent): FullCalendarEventInput {
  const classNames: string[] = []
  if (event.is_task) classNames.push('mc-task')
  if (event.is_completed) classNames.push('mc-done')

  if (event.is_all_day && event.event_date) {
    return {
      id: String(event.id),
      title: event.title,
      // A date string keeps all-day events free of any timezone drift.
      start: event.event_date,
      allDay: true,
      classNames,
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
  }
}

/** The dialog draft for an existing event. */
export function draftFromEvent(event: ApiEvent): EventDraft {
  const start = fromUnixSeconds(event.start_at)
  return {
    id: event.id,
    title: event.title,
    notes: event.notes ?? '',
    isAllDay: event.is_all_day,
    eventDate: event.event_date ?? toDateOnly(start),
    start: toDateTimeLocal(start),
    end: event.end_at !== null ? toDateTimeLocal(fromUnixSeconds(event.end_at)) : '',
    isTask: event.is_task,
    isCompleted: event.is_completed,
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
    isAllDay: allDay,
    eventDate: toDateOnly(start),
    start: toDateTimeLocal(start),
    end: toDateTimeLocal(effectiveEnd),
    isTask: false,
    isCompleted: false,
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
  const base: EventInput = {
    reminders,
    title,
    // An empty notes box means "no notes", stored as NULL.
    notes: notes === '' ? null : notes,
    is_task: draft.isTask,
    // The server clears this anyway when the event is not a task.
    is_completed: draft.isTask ? draft.isCompleted : false,
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
