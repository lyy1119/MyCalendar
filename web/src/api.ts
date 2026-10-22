/**
 * Thin client for the MyCalendar HTTP API.
 *
 * Time is always exchanged as Unix seconds and calendar dates as YYYY-MM-DD
 * strings; the browser's IANA timezone is sent as the `tz` parameter so that
 * "a day" means the same thing to the UI and to the server.
 */

export interface ApiCategory {
  id: number
  name: string
  color: string
  /**
   * Whether the sidebar shows this category. A stored preference: the events
   * are still there and still returned, they are just filtered out of the grid.
   */
  display: boolean
}

/** The two ids the API gives a fixed meaning to. */
export const CATEGORY_DELETED = -1
export const CATEGORY_UNCATEGORIZED = 0

export interface CategoryInput {
  name?: string
  color?: string
  display?: boolean
}

export interface ApiReminder {
  id: number
  event_id: number
  remind_at: number
  created_at: number
}

export interface ApiEvent {
  id: number
  title: string
  notes: string | null
  location: string | null
  start_at: number
  end_at: number | null
  is_all_day: boolean
  event_date: string | null
  is_task: boolean
  is_completed: boolean
  category_id: number
  created_at: number
  updated_at: number
  reminders: ApiReminder[]
  /** Derived by the server for the requested timezone; read-only. */
  start_at_local?: string
  end_at_local?: string | null
}

/**
 * Request body for POST and PATCH.
 *
 * On PATCH an omitted field is left untouched and an explicit null clears a
 * nullable column, so this type keeps `undefined` (omit) and `null` (clear)
 * distinct.
 */
export interface EventInput {
  title?: string
  notes?: string | null
  location?: string | null
  start_at?: number
  end_at?: number | null
  is_all_day?: boolean
  event_date?: string | null
  is_task?: boolean
  is_completed?: boolean
  category_id?: number
  /**
   * Whole-set replacement of the event's reminders. Sending [] clears them;
   * omitting the field leaves them untouched.
   */
  reminders?: { remind_at: number }[]
}

export interface ApiFieldError {
  field: string
  message: string
}

/** An error returned by the API, or a transport failure. */
export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly details: ApiFieldError[]

  constructor(status: number, code: string, message: string, details: ApiFieldError[] = []) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.details = details
  }

  /** The message plus every per-field problem, for display in one place. */
  get fullMessage(): string {
    if (this.details.length === 0) return this.message
    return this.details.map((d) => `${d.field}: ${d.message}`).join('；')
  }
}

/** Same-origin by default, which is how the Go server serves this bundle. */
const BASE = (import.meta.env.VITE_API_BASE ?? '').replace(/\/+$/, '')

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  let response: Response
  try {
    response = await fetch(`${BASE}${path}`, {
      ...init,
      headers: {
        Accept: 'application/json',
        ...(init.body === undefined ? {} : { 'Content-Type': 'application/json' }),
        ...init.headers,
      },
    })
  } catch {
    throw new ApiError(0, 'network_error', '无法连接后端服务，请确认服务正在运行', [])
  }

  if (response.status === 204) return undefined as T

  const text = await response.text()
  let payload: unknown = null
  if (text) {
    try {
      payload = JSON.parse(text)
    } catch {
      payload = null
    }
  }

  if (!response.ok) {
    const envelope = payload as { error?: { code?: string; message?: string; details?: ApiFieldError[] } } | null
    const err = envelope?.error
    throw new ApiError(
      response.status,
      err?.code ?? 'unknown_error',
      err?.message ?? `请求失败（HTTP ${response.status}）`,
      err?.details ?? [],
    )
  }
  return payload as T
}

export interface ListParams {
  /**
   * Inclusive lower bound: RFC3339 with an offset, or Unix seconds. A value
   * without an offset is read in `tz`.
   */
  from: string
  /** Exclusive upper bound, same encodings as `from`. */
  to: string
  tz: string
  /**
   * Stop hiding events in the deleted category. The grid asks for them when the
   * deleted category is switched on in the sidebar, so that toggling it shows
   * what was thrown away rather than an empty result.
   */
  includeDeleted?: boolean
  signal?: AbortSignal
}

/** Events overlapping [from, to), in the given timezone. */
export async function listEvents(params: ListParams): Promise<ApiEvent[]> {
  const query = new URLSearchParams({
    from: params.from,
    to: params.to,
    tz: params.tz,
  })
  if (params.includeDeleted) query.set('include_deleted', 'true')
  const data = await request<{ events?: ApiEvent[] }>(`/api/v1/events?${query.toString()}`, {
    signal: params.signal,
  })
  return data.events ?? []
}

/** A single event, including its reminders. */
export function getEvent(id: number, tz: string): Promise<ApiEvent> {
  return request<ApiEvent>(`/api/v1/events/${id}?tz=${encodeURIComponent(tz)}`)
}

export function createEvent(input: EventInput, tz: string): Promise<ApiEvent> {  return request<ApiEvent>(`/api/v1/events?tz=${encodeURIComponent(tz)}`, {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export function updateEvent(id: number, input: EventInput, tz: string): Promise<ApiEvent> {
  return request<ApiEvent>(`/api/v1/events/${id}?tz=${encodeURIComponent(tz)}`, {
    method: 'PATCH',
    body: JSON.stringify(input),
  })
}

/**
 * Delete an event, which marks it: it moves to the deleted category (id -1),
 * leaves the calendar, and can be brought back like any other category change.
 */
export function deleteEvent(id: number): Promise<void> {
  return request<void>(`/api/v1/events/${id}`, { method: 'DELETE' })
}

/** Destroy an event for good, reminders included. There is no way back. */
export function removeEvent(id: number): Promise<void> {
  return request<void>(`/api/v1/events/${id}/remove`, { method: 'POST' })
}

/** Every category, including the seeded deleted and uncategorized rows. */
export async function listCategories(signal?: AbortSignal): Promise<ApiCategory[]> {
  const data = await request<{ categories?: ApiCategory[] }>('/api/v1/categories', { signal })
  return data.categories ?? []
}

export function createCategory(input: CategoryInput): Promise<ApiCategory> {
  return request<ApiCategory>('/api/v1/categories', { method: 'POST', body: JSON.stringify(input) })
}

export function updateCategory(id: number, input: CategoryInput): Promise<ApiCategory> {
  return request<ApiCategory>(`/api/v1/categories/${id}`, { method: 'PATCH', body: JSON.stringify(input) })
}

export function deleteCategory(id: number): Promise<void> {
  return request<void>(`/api/v1/categories/${id}`, { method: 'DELETE' })
}
