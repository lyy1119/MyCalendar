import type { RefObject } from 'react'
import FullCalendar from '@fullcalendar/react'
import type { DateSelectArg, DatesSetArg, EventClickArg } from '@fullcalendar/core'
import type { DateClickArg } from '@fullcalendar/interaction'
import timeGridPlugin from '@fullcalendar/timegrid'
import dayGridPlugin from '@fullcalendar/daygrid'
import interactionPlugin from '@fullcalendar/interaction'
import zhCn from '@fullcalendar/core/locales/zh-cn'

import type { ApiEvent } from '../api'
import { toCalendarEvent } from '../eventMapping'
import { BROWSER_TZ } from '../time'

interface Props {
  events: ApiEvent[]
  calendarRef: RefObject<FullCalendar | null>
  onDatesSet: (arg: DatesSetArg) => void
  onEventClick: (arg: EventClickArg) => void
  onSelect: (arg: DateSelectArg) => void
  onDateClick: (arg: DateClickArg) => void
}

/**
 * The day/week grid.
 *
 * Two views are exposed: a four-day view (the default, mirroring the layout
 * reference) and the current week. Both are timeGrid views, so they share the
 * same structure as Google Calendar: a pinned day header, a pinned all-day
 * row, then a single scrolling time region with a fixed hour axis on the left
 * and the day columns aligning to it.
 */
export function CalendarPane({
  events,
  calendarRef,
  onDatesSet,
  onEventClick,
  onSelect,
  onDateClick,
}: Props) {
  return (
    <FullCalendar
      ref={calendarRef}
      plugins={[timeGridPlugin, dayGridPlugin, interactionPlugin]}
      initialView="fourDay"
      views={{
        // A timeGrid with a four-day span; this is a plain view definition in
        // the free build, not a separate package.
        fourDay: { type: 'timeGrid', duration: { days: 4 }, buttonText: '4 天' },
        timeGridWeek: { buttonText: '周' },
      }}
      locale={zhCn}
      // The same zone the API is queried with, so the grid and the server
      // agree on where a day starts.
      timeZone={BROWSER_TZ}
      firstDay={1}
      headerToolbar={{ left: 'prev,next today', center: 'title', right: 'fourDay,timeGridWeek' }}
      buttonText={{ today: '今天' }}
      dayHeaderFormat={{ weekday: 'short', day: 'numeric' }}
      // The all-day events get their own band above the scrolling hours.
      allDaySlot
      allDayText="全天"
      slotDuration="01:00:00"
      slotLabelInterval="01:00"
      slotLabelFormat={{ hour: '2-digit', minute: '2-digit', hour12: false }}
      eventTimeFormat={{ hour: '2-digit', minute: '2-digit', hour12: false }}
      eventDisplay="block"
      dayMaxEvents={false}
      nowIndicator
      stickyHeaderDates
      expandRows
      height="100%"
      scrollTime="07:00:00"
      // Selection is for creating; dragging to reschedule is deliberately off,
      // since editing goes through the dialog.
      selectable
      selectMirror
      editable={false}
      events={events.map(toCalendarEvent)}
      datesSet={onDatesSet}
      eventClick={onEventClick}
      select={onSelect}
      dateClick={onDateClick}
    />
  )
}
