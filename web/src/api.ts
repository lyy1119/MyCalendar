/**
 * Thin client for the MyCalendar HTTP API.
 *
 * Time is always exchanged as Unix seconds and calendar dates as YYYY-MM-DD
 * strings; the browser's IANA timezone is sent as the `tz` parameter so that
 * "a day" means the same thing to the UI and to the server.
 */

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
  start_at: number
  end_at: number | null
  is_all_day: boolean
  event_date: string | null
  is_task: boolean
  is_completed: boolean
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
  start_at?: number
  end_at?: number | null
  is_all_day?: boolean
  event_date?: string | null
  is_task?: boolean
  is_completed?: boolean
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
  signal?: AbortSignal
}

/** Events overlapping [from, to), in the given timezone. */
export async function listEvents(params: ListParams): Promise<ApiEvent[]> {
  const query = new URLSearchParams({
    from: params.from,
    to: params.to,
    tz: params.tz,
  })
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

export function deleteEvent(id: number): Promise<void> {
  return request<void>(`/api/v1/events/${id}`, { method: 'DELETE' })
}
