import { useEffect, useRef } from 'react'
import FullCalendar from '@fullcalendar/react'
import dayGridPlugin from '@fullcalendar/daygrid'
import interactionPlugin from '@fullcalendar/interaction'
import type { DateClickArg } from '@fullcalendar/interaction'
import zhCn from '@fullcalendar/core/locales/zh-cn'

import { BROWSER_TZ, isSameDay } from '../time'

interface Props {
  /** The date the main calendar is currently showing. */
  activeDate: Date
  onPickDate: (date: Date) => void
  onNew: () => void
}

/**
 * The left rail: a create button and a month overview used for navigation.
 *
 * The mini month follows the main calendar and jumps it when a day is picked,
 * which is the behaviour of the layout reference.
 */
export function Sidebar({ activeDate, onPickDate, onNew }: Props) {
  const miniRef = useRef<FullCalendar>(null)

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
    </aside>
  )
}
