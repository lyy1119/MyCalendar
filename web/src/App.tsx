import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import FullCalendar from '@fullcalendar/react'
import type { DateSelectArg, DatesSetArg, EventClickArg } from '@fullcalendar/core'
import type { DateClickArg } from '@fullcalendar/interaction'

import {
  ApiError,
  getEvent,
  listCategories,
  listEvents,
  updateCategory,
  type ApiCategory,
  type ApiEvent,
} from './api'
import { draftForRange, draftFromEvent, type EventDraft } from './eventMapping'
import { BROWSER_TZ } from './time'
import { readEditUnlocked, saveEditUnlocked } from './theme'
import { CalendarPane } from './components/CalendarPane'
import { EventDialog } from './components/EventDialog'
import { Sidebar } from './components/Sidebar'
import { CategoriesPage } from './components/CategoriesPage'
import { TrashPage } from './components/TrashPage'

/** How long a success notice stays on screen. */
const TOAST_MS = 2600

/**
 * The two pages this app has.
 *
 * They are real paths rather than local state so that a refresh stays put and
 * the browser's back button works; the Go server serves the same shell for any
 * non-/api path, which is what makes that possible without a router.
 */
type Route = 'calendar' | 'categories' | 'trash'

function readRoute(): Route {
  if (window.location.pathname.startsWith('/categories')) return 'categories'
  if (window.location.pathname.startsWith('/trash')) return 'trash'
  return 'calendar'
}

export default function App() {
  const calendarRef = useRef<FullCalendar>(null)
  const [events, setEvents] = useState<ApiEvent[]>([])
  const [activeDate, setActiveDate] = useState<Date>(() => new Date())
  /** The FullCalendar view on screen ('fourDay', 'timeGridWeek', ...). */
  const [viewType, setViewType] = useState('fourDay')
  const [dialogDraft, setDialogDraft] = useState<EventDraft | null>(null)
  const [toast, setToast] = useState<string | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [categories, setCategories] = useState<ApiCategory[]>([])
  const [route, setRoute] = useState<Route>(() => readRoute())
  /**
   * Whether the event dialog is showing a saved event as read-only, because the
   * edit lock is on. Kept apart from editUnlocked so unlocking from inside the
   * dialog turns it editable in place instead of closing it.
   */
  const [viewOnly, setViewOnly] = useState(false)
  /**
   * Whether edits to existing events are unlocked.
   *
   * Locked by default. Creating is deliberately unaffected: the sidebar's
   * "新建日程" button, dragging empty grid space and clicking the all-day row
   * all keep working, because the lock is about changing what is already there.
   */
  const [editUnlocked, setEditUnlocked] = useState<boolean>(() => readEditUnlocked())
  const [lockedNotice, setLockedNotice] = useState<string | null>(null)

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

  // Categories are needed to colour the grid and to fill the dialog's picker, so
  // they are loaded once up front and again whenever the list changes.
  const loadCategories = useCallback(async () => {
    try {
      setCategories(await listCategories())
    } catch (err) {
      setLoadError(err instanceof ApiError ? err.fullMessage : String(err))
    }
  }, [])

  useEffect(() => {
    void loadCategories()
  }, [loadCategories])

  // A category change affects what is already on screen: a renamed or
  // recoloured category has to reach the grid, and a deleted one moves events.
  const handleCategoriesChanged = useCallback(() => {
    void loadCategories()
    refresh()
  }, [loadCategories, refresh])

  /**
   * Switching a category on or off. The grid re-filters as soon as the stored
   * value comes back, so the choice survives a reload and every client agrees.
   */
  const handleToggleCategory = useCallback(
    async (id: number, display: boolean) => {
      try {
        await updateCategory(id, { display })
        await loadCategories()
        refresh()
      } catch (err) {
        setLoadError(err instanceof ApiError ? err.fullMessage : String(err))
        // Put the checkbox back where the server still thinks it is.
        await loadCategories()
      }
    },
    [loadCategories, refresh],
  )

  useEffect(() => () => abortRef.current?.abort(), [])

  // Keep the page in step with the history stack.
  useEffect(() => {
    const onPopState = () => setRoute(readRoute())
    window.addEventListener('popstate', onPopState)
    return () => window.removeEventListener('popstate', onPopState)
  }, [])

  const navigate = useCallback((next: Route) => {
    const path = next === 'categories' ? '/categories' : next === 'trash' ? '/trash' : '/'
    if (window.location.pathname !== path) window.history.pushState({ route: next }, '', path)
    setRoute(next)
  }, [])

  useEffect(() => {
    if (!toast) return
    const timer = window.setTimeout(() => setToast(null), TOAST_MS)
    return () => window.clearTimeout(timer)
  }, [toast])

  useEffect(() => {
    saveEditUnlocked(editUnlocked)
    if (editUnlocked) setLockedNotice(null)
  }, [editUnlocked])

  // The notice is only a nudge; it should not sit there forever.
  useEffect(() => {
    if (!lockedNotice) return
    const timer = window.setTimeout(() => setLockedNotice(null), TOAST_MS)
    return () => window.clearTimeout(timer)
  }, [lockedNotice])

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
      setViewType(arg.view.type)
      void load(from, to)
    },
    [load],
  )

  // Opening an event always works: with the lock on it opens the details
  // read-only, which is what "click to look" means.
  const handleEventClick = useCallback(
    (arg: EventClickArg) => {
      const id = Number(arg.event.id)
      const open = (draft: EventDraft) => {
        setViewOnly(!editUnlocked)
        setDialogDraft(draft)
      }
      const local = events.find((e) => e.id === id)
      if (local) {
        open(draftFromEvent(local))
        return
      }
      // Fall back to a fresh read if the event is not in the current page.
      void getEvent(id, BROWSER_TZ)
        .then((event) => open(draftFromEvent(event)))
        .catch((err: unknown) => setLoadError(err instanceof ApiError ? err.fullMessage : String(err)))
    },
    [events, editUnlocked],
  )

  // With the lock on, creating is available only through the sidebar button:
  // dragging empty space or clicking the all-day row would otherwise open the
  // create dialog, which is an editing interaction on the grid surface.
  const handleSelect = useCallback(
    (arg: DateSelectArg) => {
      if (!editUnlocked) {
        setLockedNotice('编辑已锁定：新建请用左上角的「新建日程」')
        return
      }
      // A plain click can surface as a zero-length selection; dateClick already
      // opens the dialog for that case, so ignore it here to avoid two dialogs.
      if (arg.end.getTime() - arg.start.getTime() <= 0) return
      setViewOnly(false)
      setDialogDraft(draftForRange(arg.start, arg.end, arg.allDay))
    },
    [editUnlocked],
  )

  const handleDateClick = useCallback(
    (arg: DateClickArg) => {
      if (!editUnlocked) {
        setLockedNotice('编辑已锁定：新建请用左上角的「新建日程」')
        return
      }
      const end = arg.allDay ? null : new Date(arg.date.getTime() + 60 * 60 * 1000)
      setViewOnly(false)
      setDialogDraft(draftForRange(arg.date, end, arg.allDay))
    },
    [editUnlocked],
  )

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
    setViewOnly(false)
    setDialogDraft(draftForRange(base, new Date(base.getTime() + 60 * 60 * 1000), false))
  }, [])

  const handlePickDate = useCallback(
    (date: Date) => {
      // Picking a day is a calendar action, so it also leaves the category page.
      navigate('calendar')
      calendarRef.current?.getApi().gotoDate(date)
    },
    [navigate],
  )

  // Only the categories switched on in the sidebar are drawn. An empty list
  // means the categories have not loaded, and then hiding everything would be
  // a lie about the data, so the events are shown as they came.
  const visibleEvents = useMemo(() => {
    if (categories.length === 0) return events
    const shown = new Set(categories.filter((c) => c.display).map((c) => c.id))
    return events.filter((event) => shown.has(event.category_id))
  }, [events, categories])

  const handleSaved = useCallback(
    (message: string) => {
      setDialogDraft(null)
      setViewOnly(false)
      setToast(message)
      refresh()
    },
    [refresh],
  )

  return (
    <div className="mc-app">
      <Sidebar
        activeDate={activeDate}
        onPickDate={handlePickDate}
        onNew={handleNew}
        editUnlocked={editUnlocked}
        onEditUnlockedChange={setEditUnlocked}
        onOpenCategories={() => navigate('categories')}
        categoriesActive={route === 'categories'}
        onOpenTrash={() => navigate('trash')}
        trashActive={route === 'trash'}
        categories={categories}
        onToggleCategory={(id, display) => void handleToggleCategory(id, display)}
      />

      <main className="mc-main">
        {loadError && (
          <div className="mc-banner mc-banner--error" role="alert">
            <span>{loadError}</span>
            <button type="button" className="mc-banner__close" onClick={() => setLoadError(null)} aria-label="关闭">
              ✕
            </button>
          </div>
        )}

        {/* The calendar stays mounted while the category page covers it, so
            going there and back does not reset the view or refetch. */}
        <div
          className="mc-calendar"
          data-loading={loading ? 'true' : 'false'}
          data-readonly={editUnlocked ? 'false' : 'true'}
          data-view={viewType}
        >
          <CalendarPane
            events={visibleEvents}
            categories={categories}
            readOnly={!editUnlocked}
            calendarRef={calendarRef}
            onDatesSet={handleDatesSet}
            onEventClick={handleEventClick}
            onSelect={handleSelect}
            onDateClick={handleDateClick}
          />
        </div>

        {route === 'categories' && (
          <CategoriesPage
            categories={categories}
            onBack={() => navigate('calendar')}
            onChanged={handleCategoriesChanged}
          />
        )}

        {route === 'trash' && (
          <TrashPage categories={categories} onBack={() => navigate('calendar')} onChanged={refresh} />
        )}
      </main>

      {dialogDraft && (
        <EventDialog
          draft={dialogDraft}
          categories={categories}
          readOnly={viewOnly}
          onRequestUnlock={
            viewOnly
              ? () => {
                  setEditUnlocked(true)
                  setViewOnly(false)
                }
              : undefined
          }
          onClose={() => {
            setDialogDraft(null)
            setViewOnly(false)
          }}
          onSaved={handleSaved}
        />
      )}

      {toast && <div className="mc-toast" role="status">{toast}</div>}
      {lockedNotice && (
        <div className="mc-toast mc-toast--notice" role="status">
          {lockedNotice}
        </div>
      )}
    </div>
  )
}
