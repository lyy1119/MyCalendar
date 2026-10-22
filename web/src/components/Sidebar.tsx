import { useEffect, useRef, useState, type CSSProperties } from 'react'
import FullCalendar from '@fullcalendar/react'
import dayGridPlugin from '@fullcalendar/daygrid'
import interactionPlugin from '@fullcalendar/interaction'
import type { DateClickArg } from '@fullcalendar/interaction'
import zhCn from '@fullcalendar/core/locales/zh-cn'

import { type ApiCategory } from '../api'
import { readableTextOn } from '../eventMapping'
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
  /** Whether editing existing events is unlocked. */
  editUnlocked: boolean
  onEditUnlockedChange: (unlocked: boolean) => void
  onOpenCategories: () => void
  /** Whether the category page is the one on screen, so the gear can show it. */
  categoriesActive: boolean
  /** Every category, with the stored answer to "show this one?". */
  categories: ApiCategory[]
  onToggleCategory: (id: number, display: boolean) => void
  onOpenTrash: () => void
  /** Whether the trash page is the one on screen. */
  trashActive: boolean
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
export function Sidebar({
  activeDate,
  onPickDate,
  onNew,
  editUnlocked,
  onEditUnlockedChange,
  onOpenCategories,
  categoriesActive,
  categories,
  onToggleCategory,
  onOpenTrash,
  trashActive,
}: Props) {
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
      <div className="mc-new-row">
        <button type="button" className="mc-new-btn" onClick={onNew}>
          <span aria-hidden="true">＋</span>
          <span>新建日程</span>
        </button>
        <button
          type="button"
          className={categoriesActive ? 'mc-gear-btn is-active' : 'mc-gear-btn'}
          onClick={onOpenCategories}
          aria-label="分类"
          aria-pressed={categoriesActive}
          title="分类管理"
        >
          <span aria-hidden="true">⚙</span>
        </button>
      </div>

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

      {/* Which categories the grid draws. Like Google's calendar list: the
          square is the checkbox and the swatch at once, so the colour and the
          visibility of a category are one control. */}
      <div className="mc-catlist">
        <div className="mc-catlist__head">分类</div>
        <ul className="mc-catlist__items">
          {categories.map((category) => (
            <li key={category.id}>
              <label
                className="mc-catlist__row"
                style={
                  {
                    '--cat': category.color,
                    '--cat-ink': readableTextOn(category.color),
                  } as CSSProperties
                }
              >
                <input
                  type="checkbox"
                  checked={category.display}
                  aria-label={`显示「${category.name}」`}
                  onChange={(e) => onToggleCategory(category.id, e.target.checked)}
                />
                <span className="mc-catlist__box" aria-hidden="true">
                  {category.display ? '✓' : ''}
                </span>
                <span className="mc-catlist__name">{category.name}</span>
              </label>
            </li>
          ))}
        </ul>
      </div>

      <dl className="mc-facts">
        <dt>时区</dt>
        <dd>{BROWSER_TZ}</dd>
        <dt>说明</dt>
        <dd>{editUnlocked ? '点击日程可查看并修改，拖动空白处可新建' : '点击日程可查看内容；新建用左上角按钮'}</dd>
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

      <div className="mc-edit">
        <button
          type="button"
          className={editUnlocked ? 'mc-lock-btn is-on' : 'mc-lock-btn'}
          aria-pressed={editUnlocked}
          onClick={() => onEditUnlockedChange(!editUnlocked)}
        >
          <span className="mc-lock-btn__icon" aria-hidden="true">
            {editUnlocked ? '🔓' : '🔒'}
          </span>
          <span>{editUnlocked ? '修改已开启' : '开启修改'}</span>
        </button>
        {/* Creating is deliberately never gated, so the hint has to say so. */}
        <p className="mc-hint">
          {editUnlocked
            ? '点击日程可修改或删除'
            : '已锁定：点击日程只能查看内容，「新建日程」仍然可用'}
        </p>

        {/* The trash sits below the lock, at the bottom of the rail: it is a
            place you go to, not a setting. */}
        <button
          type="button"
          className={trashActive ? 'mc-trash-link is-active' : 'mc-trash-link'}
          onClick={onOpenTrash}
          aria-pressed={trashActive}
        >
          <span aria-hidden="true">🗑</span>
          <span>回收站</span>
        </button>
      </div>
    </aside>
  )
}
