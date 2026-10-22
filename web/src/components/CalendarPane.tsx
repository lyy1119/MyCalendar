import { useMemo, type RefObject } from 'react'
import FullCalendar from '@fullcalendar/react'
import type { DateSelectArg, DatesSetArg, EventClickArg, EventContentArg } from '@fullcalendar/core'
import type { DateClickArg } from '@fullcalendar/interaction'
import timeGridPlugin from '@fullcalendar/timegrid'
import dayGridPlugin from '@fullcalendar/daygrid'
import interactionPlugin from '@fullcalendar/interaction'
import zhCn from '@fullcalendar/core/locales/zh-cn'

import type { ApiCategory, ApiEvent } from '../api'
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
  /** Needed to colour each event by its category. */
  categories: ApiCategory[]
  /**
   * Locked means existing events can be read but not changed: clicking one
   * opens a read-only view, and creating by dragging is off. Creating from the
   * sidebar button is never gated -- that is the one thing the lock leaves
   * available.
   */
  readOnly: boolean
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
  categories,
  readOnly,
  calendarRef,
  onDatesSet,
  onEventClick,
  onSelect,
  onDateClick,
}: Props) {
  // One lookup table for the whole render rather than one per event.
  const calendarEvents = useMemo(() => {
    const byId = new Map(categories.map((category) => [category.id, category]))
    return events.map((event) => toCalendarEvent(event, byId))
  }, [events, categories])

  return (
    <FullCalendar
      ref={calendarRef}
      plugins={[timeGridPlugin, dayGridPlugin, interactionPlugin]}
      initialView="fourDay"
      views={{
        // A timeGrid with a four-day span; this is a plain view definition in
        // the free build, not a separate package.
        fourDay: { type: 'timeGrid', duration: { days: 4 }, buttonText: '4 天' },
        timeGridDay: { buttonText: '日' },
        timeGridWeek: { buttonText: '周' },
      }}
      locale={zhCn}
      // Deliberately NOT timeZone="Asia/Shanghai": @fullcalendar/core ships no
      // named-timezone implementation, so a named zone silently degrades to UTC
      // (no warning) and every event is drawn at the wrong hour. The default,
      // 'local', uses the browser's own zone -- which is exactly the zone the
      // API is queried with, so the grid and the server still agree.
      firstDay={1}
      // "Back to today" is two buttons, and only one of them is shown: in a
      // 4-day or single-day window the useful label is 今天, in the week view it
      // is 本周. A custom button's text is fixed, so the pair is toggled by the
      // data-view attribute on the container instead.
      headerToolbar={{
        left: 'prev,next today,thisWeek',
        center: 'title',
        right: 'timeGridDay,fourDay,timeGridWeek',
      }}
      // A custom button rather than the built-in one: FullCalendar disables its
      // own "today" whenever today is already inside the visible window, so the
      // button sits there greyed out and unclickable most of the time -- which
      // reads as broken. The click-through does exactly what the built-in one
      // does, but it is always available, so "back to today" never refuses.
      customButtons={{
        // Both jump to today; in the week view that is the week containing it,
        // which is the same call. The anchor is today itself, so a 4-day window
        // puts today in the leftmost column rather than centred or centred on
        // some other day.
        today: {
          text: '今天',
          hint: '把今天放到第一列',
          click: () => calendarRef.current?.getApi().today(),
        },
        thisWeek: {
          text: '本周',
          hint: '回到本周',
          click: () => calendarRef.current?.getApi().today(),
        },
      }}
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
      selectable={!readOnly}
      selectMirror
      editable={false}
      events={calendarEvents}
      eventContent={eventChip}
      datesSet={onDatesSet}
      eventClick={onEventClick}
      select={onSelect}
      dateClick={onDateClick}
    />
  )
}
