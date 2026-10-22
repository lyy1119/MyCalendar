import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  ApiError,
  deleteForeverEvent,
  listEvents,
  updateEvent,
  type ApiCategory,
  type ApiEvent,
  type SearchField,
} from '../api'
import { BROWSER_TZ } from '../time'

interface Props {
  categories: ApiCategory[]
  onBack: () => void
  /** Something was destroyed; the calendar and the counts are now stale. */
  onChanged: () => void
}

/**
 * The trash is the other half of "delete": an event moved here is off the
 * calendar but still a row, and this page is the only place that destroys one
 * for good.
 *
 * The window is deliberately enormous: a trashed event keeps its dates, and the
 * listing endpoint always asks for a window, so this asks for "all of them"
 * rather than inventing a separate unfiltered endpoint.
 */
const TRASH_FROM = '1970-01-01T00:00:00'
const TRASH_TO = '2100-01-01T00:00:00'

const SEARCH_FIELDS: { value: SearchField; label: string }[] = [
  { value: 'title', label: '标题' },
  { value: 'location', label: '地点' },
  { value: 'notes', label: '备注' },
  { value: 'date', label: '日期' },
]

/** One line describing when the event is, in the browser's timezone. */
function whenText(event: ApiEvent): string {
  if (event.is_all_day) return event.event_date ?? '全天'
  const start = event.start_at_local?.slice(0, 16).replace('T', ' ') ?? ''
  const end = event.end_at_local?.slice(11, 16) ?? ''
  return end ? `${start} – ${end}` : start
}

export function TrashPage({ categories, onBack, onChanged }: Props) {
  const [events, setEvents] = useState<ApiEvent[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const [field, setField] = useState<SearchField>('title')
  const [busyID, setBusyID] = useState<number | null>(null)

  const byID = useMemo(() => new Map(categories.map((c) => [c.id, c])), [categories])

  const load = useCallback(
    async (term: string, searchField: SearchField) => {
      setLoading(true)
      try {
        const list = await listEvents({
          from: TRASH_FROM,
          to: TRASH_TO,
          tz: BROWSER_TZ,
          deleted: true,
          q: term.trim() || undefined,
          searchField,
          // The list is capped anyway; a trash that big is a bug, not a user.
          limit: 500,
        })
        setEvents(list)
        setError(null)
      } catch (err) {
        setError(err instanceof ApiError ? err.fullMessage : String(err))
      } finally {
        setLoading(false)
      }
    },
    [],
  )

  useEffect(() => {
    document.title = '回收站 · MyCalendar'
    void load('', 'title')
    return () => {
      document.title = 'MyCalendar'
    }
  }, [load])

  const search = (event: React.FormEvent) => {
    event.preventDefault()
    void load(query, field)
  }

  // Restoring is an ordinary field write: it puts the event back on the
  // calendar with its category, reminders and canceled state untouched.
  const restore = async (event: ApiEvent) => {
    setBusyID(event.id)
    setError(null)
    try {
      await updateEvent(event.id, { deleted: false }, BROWSER_TZ)
      setEvents((prev) => prev.filter((e) => e.id !== event.id))
      onChanged()
    } catch (err) {
      setError(err instanceof ApiError ? err.fullMessage : String(err))
    } finally {
      setBusyID(null)
    }
  }

  const destroy = async (event: ApiEvent) => {
    const confirmed = window.confirm(
      `永久删除「${event.title || '未命名'}」？这条日程和它的提醒会从数据库里删掉，无法恢复。`,
    )
    if (!confirmed) return

    setBusyID(event.id)
    setError(null)
    try {
      await deleteForeverEvent(event.id)
      setEvents((prev) => prev.filter((e) => e.id !== event.id))
      onChanged()
    } catch (err) {
      setError(err instanceof ApiError ? err.fullMessage : String(err))
    } finally {
      setBusyID(null)
    }
  }

  return (
    <div className="mc-page mc-trash">
      <header className="mc-page__head">
        <button type="button" className="mc-btn mc-btn--ghost" onClick={onBack}>
          ← 返回日历
        </button>
        <h1 className="mc-page__title">回收站</h1>
        <p className="mc-page__lead">
          这里的日程已经不在日历上，但还没有被删除。可以按标题、地点、备注或日期找回，也可以永久删除。
        </p>

        <form className="mc-trash__search" onSubmit={search} role="search">
          <select
            className="mc-input mc-trash__field"
            aria-label="搜索字段"
            value={field}
            onChange={(e) => {
              const next = e.target.value as SearchField
              setField(next)
              // A date only makes sense as a date; clear a term that cannot be one.
              void load(next === 'date' ? '' : query, next)
            }}
          >
            {SEARCH_FIELDS.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
          <input
            className="mc-input mc-trash__term"
            type="search"
            aria-label="搜索内容"
            placeholder={field === 'date' ? 'YYYY-MM-DD' : `搜索${SEARCH_FIELDS.find((f) => f.value === field)?.label}`}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
          <button type="submit" className="mc-btn mc-btn--primary" disabled={loading}>
            {loading ? '搜索中…' : '搜索'}
          </button>
        </form>
      </header>

      <div className="mc-page__body mc-trash__body">
        {error && (
          <p className="mc-error" role="alert">
            {error}
          </p>
        )}

        {events.length === 0 && !loading && (
          <p className="mc-hint">{query.trim() ? '没有匹配的日程。' : '回收站是空的。'}</p>
        )}

        <ul className="mc-trash__list">
          {events.map((event) => {
            const category = byID.get(event.category_id)
            return (
              <li className="mc-trash__row" key={event.id}>
                <span
                  className="mc-trash__dot"
                  style={{ backgroundColor: category?.color ?? 'var(--mc-text-dim)' }}
                  aria-hidden="true"
                />
                <div className="mc-trash__main">
                  <div className="mc-trash__title">{event.title || '未命名'}</div>
                  <div className="mc-trash__meta">
                    <span>{whenText(event)}</span>
                    {category && <span className="mc-trash__cat">{category.name}</span>}
                    {event.location && <span className="mc-trash__where">{event.location}</span>}
                  </div>
                </div>
                <div className="mc-trash__actions">
                  <button
                    type="button"
                    className="mc-btn mc-btn--small mc-trash__restore"
                    disabled={busyID === event.id}
                    onClick={() => void restore(event)}
                  >
                    还原
                  </button>
                  <button
                    type="button"
                    className="mc-btn mc-btn--small mc-btn--danger mc-trash__destroy"
                    disabled={busyID === event.id}
                    onClick={() => void destroy(event)}
                  >
                    永久删除
                  </button>
                </div>
              </li>
            )
          })}
        </ul>
      </div>
    </div>
  )
}
