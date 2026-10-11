import { useCallback, useEffect, useRef, useState } from 'react'
import FullCalendar from '@fullcalendar/react'
import type { DateSelectArg, DatesSetArg, EventClickArg } from '@fullcalendar/core'
import type { DateClickArg } from '@fullcalendar/interaction'

import { ApiError, getEvent, listEvents, type ApiEvent } from './api'
import { draftForRange, draftFromEvent, type EventDraft } from './eventMapping'
import { BROWSER_TZ } from './time'
import { CalendarPane } from './components/CalendarPane'
import { EventDialog } from './components/EventDialog'
import { Sidebar } from './components/Sidebar'

/** How long a success notice stays on screen. */
const TOAST_MS = 2600

export default function App() {
  const calendarRef = useRef<FullCalendar>(null)
  const [events, setEvents] = useState<ApiEvent[]>([])
  const [activeDate, setActiveDate] = useState<Date>(() => new Date())
  const [dialogDraft, setDialogDraft] = useState<EventDraft | null>(null)
  const [toast, setToast] = useState<string | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  // The range currently on screen, so mutations can refresh exactly that.
  const rangeRef = useRef<{ from: string; to: string } | null>(null)
  const abortRef = useRef<AbortController | null>(null)

  const load = useCallback(async (from: string, to: string) => {
    // A newer request supersedes an in-flight one; without this, a fast
    // sequence of week navigations can land results out of order.
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller

    setLoading(true)
    try {
      const list = await listEvents({ from, to, tz: BROWSER_TZ, signal: controller.signal })
      setEvents(list)
      setLoadError(null)
    } catch (err) {
      if (controller.signal.aborted) return
      setLoadError(err instanceof ApiError ? err.fullMessage : String(err))
    } finally {
      if (!controller.signal.aborted) setLoading(false)
    }
  }, [])

  const refresh = useCallback(() => {
    const range = rangeRef.current
    if (range) void load(range.from, range.to)
  }, [load])

  useEffect(() => () => abortRef.current?.abort(), [])

  useEffect(() => {
    if (!toast) return
    const timer = window.setTimeout(() => setToast(null), TOAST_MS)
    return () => window.clearTimeout(timer)
  }, [toast])

  const handleDatesSet = useCallback(
    (arg: DatesSetArg) => {
      // startStr/endStr are the window as ISO strings carrying the zone offset.
      // They are used rather than start/end Date objects because those are
      // UTC-midnight markers, and converting one with getTime() silently shifts
      // the window by the zone offset -- which drops events near midnight.
      const from = arg.startStr
      const to = arg.endStr
      rangeRef.current = { from, to }
      setActiveDate(arg.view.currentStart)
      void load(from, to)
    },
    [load],
  )

  const handleEventClick = useCallback(
    (arg: EventClickArg) => {
      const id = Number(arg.event.id)
      const local = events.find((e) => e.id === id)
      if (local) {
        setDialogDraft(draftFromEvent(local))
        return
      }
      // Fall back to a fresh read if the event is not in the current page.
      void getEvent(id, BROWSER_TZ)
        .then((event) => setDialogDraft(draftFromEvent(event)))
        .catch((err: unknown) => setLoadError(err instanceof ApiError ? err.fullMessage : String(err)))
    },
    [events],
  )

  const handleSelect = useCallback((arg: DateSelectArg) => {
    // A plain click can surface as a zero-length selection; dateClick already
    // opens the dialog for that case, so ignore it here to avoid two dialogs.
    if (arg.end.getTime() - arg.start.getTime() <= 0) return
    setDialogDraft(draftForRange(arg.start, arg.end, arg.allDay))
  }, [])

  const handleDateClick = useCallback((arg: DateClickArg) => {
    const end = arg.allDay ? null : new Date(arg.date.getTime() + 60 * 60 * 1000)
    setDialogDraft(draftForRange(arg.date, end, arg.allDay))
  }, [])

  const handleNew = useCallback(() => {
    const api = calendarRef.current?.getApi()
    const start = api ? api.getDate() : new Date()
    const now = new Date()
    // Prefill with today at the next full hour, which is almost always closer
    // to what someone wants than an arbitrary slot.
    const base =
      start.getFullYear() === now.getFullYear() &&
      start.getMonth() === now.getMonth() &&
      start.getDate() === now.getDate()
        ? new Date(now.getFullYear(), now.getMonth(), now.getDate(), now.getHours() + 1, 0, 0, 0)
        : new Date(start.getFullYear(), start.getMonth(), start.getDate(), 9, 0, 0, 0)
    setDialogDraft(draftForRange(base, new Date(base.getTime() + 60 * 60 * 1000), false))
  }, [])

  const handlePickDate = useCallback((date: Date) => {
    calendarRef.current?.getApi().gotoDate(date)
  }, [])

  const handleSaved = useCallback(
    (message: string) => {
      setDialogDraft(null)
      setToast(message)
      refresh()
    },
    [refresh],
  )

  return (
    <div className="mc-app">
      <Sidebar activeDate={activeDate} onPickDate={handlePickDate} onNew={handleNew} />

      <main className="mc-main">
        {loadError && (
          <div className="mc-banner mc-banner--error" role="alert">
            <span>{loadError}</span>
            <button type="button" className="mc-banner__close" onClick={() => setLoadError(null)} aria-label="关闭">
              ✕
            </button>
          </div>
        )}

        <div className="mc-calendar" data-loading={loading ? 'true' : 'false'}>
          <CalendarPane
            events={events}
            calendarRef={calendarRef}
            onDatesSet={handleDatesSet}
            onEventClick={handleEventClick}
            onSelect={handleSelect}
            onDateClick={handleDateClick}
          />
        </div>
      </main>

      {dialogDraft && (
        <EventDialog draft={dialogDraft} onClose={() => setDialogDraft(null)} onSaved={handleSaved} />
      )}

      {toast && <div className="mc-toast" role="status">{toast}</div>}
    </div>
  )
}
