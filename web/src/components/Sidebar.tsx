import { useEffect, useRef, useState } from 'react'
import FullCalendar from '@fullcalendar/react'
import dayGridPlugin from '@fullcalendar/daygrid'
import interactionPlugin from '@fullcalendar/interaction'
import type { DateClickArg } from '@fullcalendar/interaction'
import zhCn from '@fullcalendar/core/locales/zh-cn'

import { BROWSER_TZ, isSameDay } from '../time'
import {
  THEME_OPTIONS,
  applyTheme,
  readThemeMode,
  resolveTheme,
  saveThemeMode,
  systemPrefersDark,
  type ThemeMode,
} from '../theme'

interface Props {
  /** The date the main calendar is currently showing. */
  activeDate: Date
  onPickDate: (date: Date) => void
  onNew: () => void
}

/**
 * Tracks the theme choice and keeps the document in step with it.
 *
 * In 'auto' mode the system preference is followed live, so the returned theme
 * is state rather than a one-off read: the hint next to the switcher has to
 * update when the operating system flips.
 */
function useTheme(): [ThemeMode, (mode: ThemeMode) => void, 'light' | 'dark'] {
  const [mode, setMode] = useState<ThemeMode>(() => readThemeMode())
  const [dark, setDark] = useState<boolean>(() => systemPrefersDark())

  useEffect(() => {
    const query = window.matchMedia?.('(prefers-color-scheme: dark)')
    if (!query) return
    const onChange = (event: MediaQueryListEvent) => setDark(event.matches)
    query.addEventListener('change', onChange)
    return () => query.removeEventListener('change', onChange)
  }, [])

  useEffect(() => {
    applyTheme(mode)
    saveThemeMode(mode)
  }, [mode, dark])

  return [mode, setMode, resolveTheme(mode)]
}

/**
 * The left rail: a create button, a month overview used for navigation, and the
 * appearance control.
 *
 * The mini month follows the main calendar and jumps it when a day is picked,
 * which is the behaviour of the layout reference.
 */
export function Sidebar({ activeDate, onPickDate, onNew }: Props) {
  const miniRef = useRef<FullCalendar>(null)
  const [themeMode, setThemeMode, resolvedTheme] = useTheme()

  useEffect(() => {
    const api = miniRef.current?.getApi()
    if (!api) return
    // Only move the month when it actually differs, so navigating inside one
    // month does not keep re-rendering the mini calendar.
    const shown = api.view.currentStart
    if (shown.getFullYear() !== activeDate.getFullYear() || shown.getMonth() !== activeDate.getMonth()) {
      api.gotoDate(activeDate)
    }
  }, [activeDate])

  return (
    <aside className="mc-sidebar">
      <button type="button" className="mc-new-btn" onClick={onNew}>
        <span aria-hidden="true">＋</span>
        <span>新建日程</span>
      </button>

      <div className="mc-mini">
        <FullCalendar
          ref={miniRef}
          plugins={[dayGridPlugin, interactionPlugin]}
          initialView="dayGridMonth"
          locale={zhCn}
          // See CalendarPane: a named zone would silently fall back to UTC.
          firstDay={1}
          headerToolbar={{ left: 'prev', center: 'title', right: 'next' }}
          height="auto"
          fixedWeekCount={false}
          showNonCurrentDates={false}
          dayMaxEvents={false}
          // A bare day number: the locale default renders "12日", which is
          // noise in a month grid.
          //
          // DayCellContentArg.date is a raw UTC-midnight marker, unlike the day
          // header's converted Date, so it is read with the UTC getter. Cells
          // belonging to a neighbouring month stay blank, matching
          // showNonCurrentDates.
          dayCellContent={(arg) => (arg.isOther ? '' : String(arg.date.getUTCDate()))}
          dateClick={(arg: DateClickArg) => onPickDate(arg.date)}
          dayCellClassNames={(arg) => (isSameDay(arg.date, activeDate) ? ['mc-mini-selected'] : [])}
        />
      </div>

      <dl className="mc-facts">
        <dt>时区</dt>
        <dd>{BROWSER_TZ}</dd>
        <dt>说明</dt>
        <dd>点击日程即可编辑，拖动空白处可新建</dd>
      </dl>

      <div className="mc-theme">
        <span className="mc-theme__label">
          外观
          {themeMode === 'auto' && (
            <span className="mc-theme__hint">跟随系统（当前{resolvedTheme === 'dark' ? '暗色' : '亮色'}）</span>
          )}
        </span>
        <div className="mc-segmented" role="group" aria-label="外观">
          {THEME_OPTIONS.map((option) => (
            <button
              key={option.mode}
              type="button"
              className={option.mode === themeMode ? 'mc-segmented__item is-active' : 'mc-segmented__item'}
              aria-pressed={option.mode === themeMode}
              onClick={() => setThemeMode(option.mode)}
            >
              {option.label}
            </button>
          ))}
        </div>
      </div>
    </aside>
  )
}
