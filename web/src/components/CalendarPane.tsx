import type { RefObject } from 'react'
import FullCalendar from '@fullcalendar/react'
import type { DateSelectArg, DatesSetArg, EventClickArg, EventContentArg } from '@fullcalendar/core'
import type { DateClickArg } from '@fullcalendar/interaction'
import timeGridPlugin from '@fullcalendar/timegrid'
import dayGridPlugin from '@fullcalendar/daygrid'
import interactionPlugin from '@fullcalendar/interaction'
import zhCn from '@fullcalendar/core/locales/zh-cn'

import type { ApiEvent } from '../api'
import { toCalendarEvent } from '../eventMapping'
import { WEEKDAY_SHORT } from '../time'

/**
 * The contents of an event block.
 *
 * Two arrangements, chosen by CSS from the height the block actually has, which
 * FullCalendar derives from the event's duration:
 *
 *   room for two lines   title, then the time underneath, then location, notes
 *   one line only        "title, 13:00 - 14:00"
 *
 * The title leads in both, because it is what identifies the event. On a single
 * line the title is the part allowed to be clipped -- a truncated time is worse
 * than useless, whereas a clipped title is still recognisable -- so the time
 * takes its full width and the title yields. One markup serves both
 * arrangements, with only the direction and a separator changed, so no text is
 * rendered twice for screen readers or text selection to trip over.
 */
function eventChip(arg: EventContentArg) {
  const { location, notes } = arg.event.extendedProps as {
    location?: string | null
    notes?: string | null
  }

  const title = arg.event.title
  const time = arg.timeText

  // An all-day bar is a single line of its own: title, then location beside it.
  if (arg.event.allDay) {
    return (
      <div className="mc-chip mc-chip--bar">
        <div className="fc-event-title">{title}</div>
        {location ? <div className="mc-chip__line">{location}</div> : null}
      </div>
    )
  }

  return (
    <div className="mc-chip">
      <div className="fc-event-title">{title}</div>
      {time ? <div className="fc-event-time">{time}</div> : null}
      {location ? <div className="mc-chip__line">{location}</div> : null}
      {notes ? <div className="mc-chip__line">{notes}</div> : null}
    </div>
  )
}

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
      // Deliberately NOT timeZone="Asia/Shanghai": @fullcalendar/core ships no
      // named-timezone implementation, so a named zone silently degrades to UTC
      // (no warning) and every event is drawn at the wrong hour. The default,
      // 'local', uses the browser's own zone -- which is exactly the zone the
      // API is queried with, so the grid and the server still agree.
      firstDay={1}
      headerToolbar={{ left: 'prev,next today', center: 'title', right: 'fourDay,timeGridWeek' }}
      buttonText={{ today: '今天' }}
      // "周一 12" rather than the locale default "12日周一".
      //
      // DayHeaderContentArg.date is a real Date already converted into the
      // calendar's zone, so it must be read with local getters. Using
      // getUTCDate() here reads a zone-shifted value and reports the wrong day
      // for any zone ahead of UTC.
      dayHeaderContent={(arg) => `${WEEKDAY_SHORT.format(arg.date)} ${arg.date.getDate()}`}
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
      eventContent={eventChip}
      datesSet={onDatesSet}
      eventClick={onEventClick}
      select={onSelect}
      dateClick={onDateClick}
    />
  )
}
