/**
 * Browser-level verification of the MyCalendar frontend.
 *
 * This is the only test that can prove the things that were actually asked
 * for -- four day columns, a week view, the all-day row pinned above the
 * hours, one shared scrolling time region, and click-through to the editor --
 * because none of them are visible to a unit test.
 *
 * It is optional and self-contained: it needs a Playwright browser and a
 * running server, neither of which the normal build requires. See README.md
 * in this directory.
 *
 *   BASE=http://127.0.0.1:8080 node verify.mjs
 *
 * The script starts by deleting every event it can see, so it is safe to run
 * against a scratch database but destructive against a real one.
 */
import { chromium } from 'playwright'

const BASE = process.env.BASE ?? 'http://127.0.0.1:8080'
const TZ = process.env.TZID ?? 'Asia/Shanghai'
const results = []
let failures = 0

function check(name, condition, detail = '') {
  const ok = Boolean(condition)
  if (!ok) failures++
  results.push(`${ok ? 'PASS' : 'FAIL'}  ${name}${detail ? `  [${detail}]` : ''}`)
}

async function api(path, init) {
  const res = await fetch(BASE + path, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) },
  })
  const text = await res.text()
  return { status: res.status, body: text ? JSON.parse(text) : null }
}

const pad = (n) => String(n).padStart(2, '0')
// All seeding is done in TZ, since that is the zone the grid and the API are
// working in.
const parts = (d) => new Intl.DateTimeFormat('en-CA', { timeZone: TZ, year: 'numeric', month: '2-digit', day: '2-digit' }).format(d)
const dateOnly = (d) => parts(d)
const hourIn = (base, dayOffset, hour) => {
  const [y, m, d] = parts(base).split('-').map(Number)
  // Build the wall clock in TZ by asking the browser what that local time is.
  return localEpoch(y, m, d + dayOffset, hour)
}
// Convert a wall clock in TZ to Unix seconds without assuming the harness zone.
function localEpoch(y, m, d, hour) {
  const asUtc = Date.UTC(y, m - 1, d, hour, 0, 0)
  // Two passes are enough to settle the offset, including across a DST edge.
  const guess = new Intl.DateTimeFormat('en-US', { timeZone: TZ, hour12: false, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
  let ts = asUtc
  for (let i = 0; i < 2; i++) {
    const p = Object.fromEntries(guess.formatToParts(new Date(ts)).map((x) => [x.type, x.value]))
    const shown = Date.UTC(Number(p.year), Number(p.month) - 1, Number(p.day), Number(p.hour) % 24, Number(p.minute))
    ts += asUtc - shown
  }
  return Math.floor(ts / 1000)
}

let browser

try {
  // ------------------------------------------------------------ clean slate

  const nowSec = Math.floor(Date.now() / 1000)
  const existing = await api(`/api/v1/events?tz=${encodeURIComponent(TZ)}&from=${nowSec - 90 * 86400}&to=${nowSec + 90 * 86400}`)
  const stale = existing.body?.events ?? []
  for (const e of stale) await api(`/api/v1/events/${e.id}`, { method: 'DELETE' })
  check('clean slate', true, `removed ${stale.length} pre-existing event(s)`)

  // -------------------------------------------------------------- seed data

  const today = new Date()
  const post = (body) => api(`/api/v1/events?tz=${encodeURIComponent(TZ)}`, { method: 'POST', body: JSON.stringify(body) })

  const allDay = await post({ title: '全天日程', event_date: dateOnly(today) })
  const timed = await post({
    title: '定时日程',
    start_at: hourIn(today, 0, 10),
    end_at: hourIn(today, 0, 11),
    notes: '备注内容',
  })
  const multiDay = await post({
    title: '跨天日程',
    start_at: hourIn(today, 0, 9),
    end_at: hourIn(today, 2, 12),
  })
  const task = await post({
    title: '任务日程',
    start_at: hourIn(today, 1, 14),
    end_at: hourIn(today, 1, 15),
    is_task: true,
  })
  check(
    'events created through the API',
    [allDay, timed, multiDay, task].every((r) => r.status === 201),
    [allDay.status, timed.status, multiDay.status, task.status].join(','),
  )

  // ----------------------------------------------------------------- render

  browser = await chromium.launch()
  // Deliberately a zone ahead of UTC. Running these checks in UTC once hid a
  // real bug: a named timeZone is silently ignored by @fullcalendar/core and
  // every event is drawn 8 hours early, which UTC cannot reveal.
  const ctx = await browser.newContext({ timezoneId: TZ, viewport: { width: 1600, height: 900 } })
  const page = await ctx.newPage()
  const consoleErrors = []
  page.on('console', (m) => {
    if (m.type() === 'error') consoleErrors.push(m.text())
  })
  page.on('pageerror', (e) => consoleErrors.push('pageerror: ' + e.message))

  await page.addInitScript(() => {
    const original = window.fetch
    window.fetch = function (input, init) {
      const url = typeof input === 'string' ? input : input?.url ?? ''
      if (url.includes('/api/v1/events?') && !window.__mcWindow) window.__mcWindow = decodeURIComponent(url)
      return original.apply(this, arguments)
    }
  })
  await page.goto(BASE + '/', { waitUntil: 'networkidle' })
  await page.waitForSelector('.mc-calendar .fc-timegrid-col', { timeout: 15000 })
  await page.waitForTimeout(500)

  // Every query is scoped to .mc-calendar: the sidebar mini calendar renders
  // its own column headers, day cells and all-day events.
  // Editing existing events is locked by default, and unlocking persists in
  // localStorage. Check the default, then unlock so the rest of this suite can
  // click through to the editor -- the earlier checks are read-only.
  const unlockSwitch = page.locator('.mc-sidebar .mc-edit input[type="checkbox"]')
  check('editing is locked by default', (await unlockSwitch.isChecked()) === false)

  const layout = await page.evaluate(() => {
    const root = document.querySelector('.mc-calendar')
    const scroller = root.querySelector('.fc-scroller-liquid-absolute')
    const scrollerBox = scroller?.getBoundingClientRect()
    const eventOf = (el) => ({
      title: el.textContent.trim(),
      top: el.getBoundingClientRect().top,
      insideScroller: scroller ? scroller.contains(el) : null,
    })
    const cols = [...root.querySelectorAll('.fc-timegrid-col')]
    return {
      headerCells: root.querySelectorAll('.fc-col-header-cell').length,
      headerLabels: [...root.querySelectorAll('.fc-col-header-cell-cushion')].map((e) => e.textContent.trim()),
      headerDates: [...root.querySelectorAll('.fc-col-header-cell')].map((e) => Number((e.getAttribute('data-date') ?? '').slice(8, 10))),
      headerDays: [...root.querySelectorAll('.fc-col-header-cell-cushion')].map((e) => Number((e.textContent.match(/(\d+)/) ?? [])[1])),
      requestedWindow: window.__mcWindow ?? null,
      axisCount: root.querySelectorAll('.fc-timegrid-axis').length,
      allDay: [...root.querySelectorAll('.fc-daygrid-event')].map(eventOf),
      timed: [...root.querySelectorAll('.fc-timegrid-event')].map(eventOf),
      scrollerTop: scrollerBox?.top ?? null,
      scrollerScrollable: scroller ? scroller.scrollHeight > scroller.clientHeight : false,
      scrollerOverflow: scroller ? getComputedStyle(scroller).overflowY : null,
      // All day columns share one scroller, which is what keeps the hour lines
      // aligned across days.
      sharedScroller: Boolean(scroller) && cols.length > 0 && cols.every((c) => scroller.contains(c)),
    }
  })

  const headerCells = layout.headerCells
  check('4-day view shows exactly 4 day columns', headerCells === 4, `headers=${headerCells}`)
  check('day columns are labelled', layout.headerLabels.length === 4, layout.headerLabels.join('|'))
  check('the grid has a time axis gutter', layout.axisCount >= 1, `axis=${layout.axisCount}`)
  check('all day columns share one scroller', layout.sharedScroller)
  check('the time grid actually scrolls', layout.scrollerScrollable, `overflow=${layout.scrollerOverflow}`)

  const allDayRow = layout.allDay.find((e) => e.title.includes('全天日程'))
  check('the all-day event is rendered', Boolean(allDayRow), layout.allDay.map((e) => e.title).join('|'))
  check('the all-day event sits outside the scrolling hours', allDayRow?.insideScroller === false, `inside=${allDayRow?.insideScroller}`)
  check(
    'the all-day row is pinned above the time grid',
    allDayRow && layout.scrollerTop !== null && allDayRow.top < layout.scrollerTop,
    `allDayTop=${Math.round(allDayRow?.top ?? -1)} gridTop=${Math.round(layout.scrollerTop ?? -1)}`,
  )

  // ---- the zone the grid works in
  check(
    'each day header names the day its column actually is',
    layout.headerDays.length === layout.headerDates.length &&
      layout.headerDays.every((n, i) => n === layout.headerDates[i]),
    `${layout.headerDays.join(',')} vs ${layout.headerDates.join(',')}`,
  )
  check(
    'the grid asks for a window in the browser timezone',
    layout.requestedWindow?.includes('+08:00') || layout.requestedWindow?.endsWith('Z'),
    layout.requestedWindow,
  )

  const titles = layout.timed.map((e) => e.title)
  check('the timed event is in the grid', titles.some((t) => t.includes('定时日程')))
  check('the multi-day event is in the grid', titles.some((t) => t.includes('跨天日程')))
  check('the task is in the grid', titles.some((t) => t.includes('任务日程')))

  // ------------------------------------------------------------- week view

  await page.locator('.mc-calendar .fc-timeGridWeek-button').click()
  await page.waitForTimeout(700)
  const week = await page.evaluate(() => {
    const root = document.querySelector('.mc-calendar')
    const scroller = root.querySelector('.fc-scroller-liquid-absolute')
    return {
      headers: root.querySelectorAll('.fc-col-header-cell').length,
      scrollable: scroller ? scroller.scrollHeight > scroller.clientHeight : false,
    }
  })
  check('week view shows exactly 7 day columns', week.headers === 7, `headers=${week.headers}`)
  check('week view still scrolls as one region', week.scrollable)

  await page.locator('.mc-calendar .fc-fourDay-button').click()
  await page.waitForTimeout(600)

  // ---- a wall clock typed into the dialog must be the wall clock drawn
  {
    const firstDay = await page.evaluate(() => document.querySelector('.mc-calendar .fc-col-header-cell')?.getAttribute('data-date'))
    await page.locator('.mc-new-btn').click()
    await page.waitForSelector('.mc-dialog', { timeout: 5000 })
    await page.locator('.mc-dialog input').first().fill('时刻探针')
    await page.locator('.mc-dialog input[type="datetime-local"]').first().fill(`${firstDay}T21:00`)
    await page.locator('.mc-dialog input[type="datetime-local"]').nth(1).fill(`${firstDay}T22:30`)
    await page.locator('.mc-dialog button[type="submit"]').click()
    await page.waitForSelector('.mc-dialog', { state: 'detached', timeout: 5000 })
    await page.waitForTimeout(800)

    const stored = (await api(`/api/v1/events?range=day&date=${firstDay}&tz=${encodeURIComponent(TZ)}`)).body?.events?.find((e) => e.title === '时刻探针')
    check(
      'a typed wall clock is stored as the instant it names',
      stored?.start_at_local === `${firstDay}T21:00:00+08:00` || /T21:00:00/.test(stored?.start_at_local ?? ''),
      `${stored?.start_at} / ${stored?.start_at_local}`,
    )
    const drawn = await page.evaluate(() => {
      const root = document.querySelector('.mc-calendar')
      const ev = [...root.querySelectorAll('.fc-timegrid-event')].find((e) => e.textContent.includes('时刻探针'))
      return ev ? ev.textContent.trim() : null
    })
    check('the grid draws it at the same wall clock', /21:00/.test(drawn ?? ''), `drawn=${drawn}`)
  }

  // ------------------------------------------- click through to the editor

  await unlockSwitch.check()
  await page.waitForTimeout(200)
  check('unlocking editing takes effect', (await unlockSwitch.isChecked()) === true)

  // Overlapping events are stacked, so the exposed left edge is the clickable
  // part, exactly as in Google Calendar.
  const openEditor = async (title) => {
    await page
      .locator('.mc-calendar .fc-timegrid-event', { hasText: title })
      .first()
      .click({ position: { x: 24, y: 10 } })
    await page.waitForSelector('.mc-dialog', { timeout: 5000 })
  }

  await openEditor('定时日程')
  const dialogTitle = await page.locator('.mc-dialog input').first().inputValue()
  const dialogNotes = await page.locator('.mc-dialog textarea').first().inputValue()
  check('clicking an event opens the editor', true)
  check('the editor is prefilled with the title', dialogTitle === '定时日程', `title=${dialogTitle}`)
  check('the editor is prefilled with the notes', dialogNotes === '备注内容', `notes=${dialogNotes}`)

  await page.locator('.mc-dialog input').first().fill('定时日程（已改名）')
  await page.locator('.mc-dialog button[type="submit"]').click()
  await page.waitForSelector('.mc-dialog', { state: 'detached', timeout: 5000 })
  await page.waitForTimeout(700)

  const afterEdit = await api(`/api/v1/events/${timed.body.id}?tz=${encodeURIComponent(TZ)}`)
  check('saving persists the new title', afterEdit.body?.title === '定时日程（已改名）', `title=${afterEdit.body?.title}`)
  check('saving leaves untouched fields alone', afterEdit.body?.notes === '备注内容', `notes=${afterEdit.body?.notes}`)
  check('the grid shows the new title', (await page.locator('.mc-calendar .fc-timegrid-event', { hasText: '已改名' }).count()) > 0)

  // ------------------------------------------------------------ completion

  await openEditor('任务日程')
  await page.locator('.mc-dialog #mc-is-completed').check()
  await page.locator('.mc-dialog button[type="submit"]').click()
  await page.waitForSelector('.mc-dialog', { state: 'detached', timeout: 5000 })
  await page.waitForTimeout(600)
  const afterComplete = await api(`/api/v1/events/${task.body.id}?tz=${encodeURIComponent(TZ)}`)
  check('a task can be marked complete', afterComplete.body?.is_completed === true, `is_completed=${afterComplete.body?.is_completed}`)

  // ------------------------------------------------- create by dragging

  // Drag inside the last day column, which has no seeded events, locating its
  // horizontal centre from the day header.
  const dayCentres = await page.$$eval('.mc-calendar .fc-col-header-cell', (els) =>
    els.map((el) => {
      const b = el.getBoundingClientRect()
      return b.x + b.width / 2
    }),
  )
  const scrollBox = await page.locator('.mc-calendar .fc-scroller-liquid-absolute').boundingBox()
  const dragX = dayCentres[dayCentres.length - 1]

  await page.mouse.move(dragX, scrollBox.y + 60)
  await page.mouse.down()
  await page.mouse.move(dragX, scrollBox.y + 150, { steps: 8 })
  await page.mouse.up()
  await page.waitForSelector('.mc-dialog', { timeout: 5000 })
  check('dragging empty grid space opens the create dialog', true)

  await page.locator('.mc-dialog input').first().fill('新建的日程')
  await page.locator('.mc-dialog button[type="submit"]').click()
  await page.waitForSelector('.mc-dialog', { state: 'detached', timeout: 5000 })
  await page.waitForTimeout(700)

  const lastDay = new Date(today.getTime() + 3 * 86400000)
  const created = await api(`/api/v1/events?range=day&date=${dateOnly(lastDay)}&tz=${encodeURIComponent(TZ)}`)
  check('the dragged event was persisted', (created.body?.events ?? []).some((e) => e.title === '新建的日程'), (created.body?.events ?? []).map((e) => e.title).join('|'))
  check('the new event appears in the grid', (await page.locator('.mc-calendar .fc-timegrid-event', { hasText: '新建的日程' }).count()) > 0)

  // ------------------------------------------- create from the all-day row

  await page.locator('.mc-calendar .fc-daygrid-day').nth(1).click()
  await page.waitForSelector('.mc-dialog', { timeout: 5000 })
  const allDayChecked = await page.locator('.mc-dialog input[type="checkbox"]').first().isChecked()
  check('the all-day row opens an all-day dialog', allDayChecked, `allDay=${allDayChecked}`)

  await page.locator('.mc-dialog input').first().fill('新建全天')
  await page.locator('.mc-dialog button[type="submit"]').click()
  await page.waitForSelector('.mc-dialog', { state: 'detached', timeout: 5000 })
  await page.waitForTimeout(700)

  const dayAfter = dateOnly(new Date(today.getTime() + 86400000))
  const allDayList = await api(`/api/v1/events?range=day&date=${dayAfter}&tz=${encodeURIComponent(TZ)}`)
  check(
    'the all-day event was persisted as all-day',
    (allDayList.body?.events ?? []).some((e) => e.title === '新建全天' && e.is_all_day === true),
    JSON.stringify((allDayList.body?.events ?? []).map((e) => [e.title, e.is_all_day])),
  )

  // ------------------------------------------------------------- reminders

  {
    const firstDay = await page.evaluate(() => document.querySelector('.mc-calendar .fc-col-header-cell')?.getAttribute('data-date'))

    // Add two reminders by hand and one with a quick offset, then save.
    await page.locator('.mc-new-btn').click()
    await page.waitForSelector('.mc-dialog', { timeout: 5000 })
    await page.locator('.mc-dialog input').first().fill('提醒探针')
    await page.locator('.mc-dialog input[type="datetime-local"]').first().fill(`${firstDay}T14:00`)
    await page.locator('.mc-dialog input[type="datetime-local"]').nth(1).fill(`${firstDay}T15:00`)

    await page.getByRole('button', { name: '＋ 添加提醒' }).click()
    await page.locator('.mc-dialog input[aria-label^="第 1 个提醒时间"]').fill(`${firstDay}T13:45`)
    await page.getByRole('button', { name: '＋ 添加提醒' }).click()
    await page.locator('.mc-dialog input[aria-label^="第 2 个提醒时间"]').fill(`${firstDay}T13:00`)
    await page.getByRole('button', { name: '提前 30 分钟' }).click()
    const rowsAfterAdd = await page.locator('.mc-dialog input[aria-label^="第"][aria-label$="个提醒时间"]').count()
    check('reminder rows can be added, including a quick offset', rowsAfterAdd === 3, `rows=${rowsAfterAdd}`)

    // Remove the middle one again.
    await page.locator('.mc-dialog button[aria-label="删除第 2 个提醒"]').click()
    const rowsAfterRemove = await page.locator('.mc-dialog input[aria-label^="第"][aria-label$="个提醒时间"]').count()
    check('a reminder row can be removed', rowsAfterRemove === 2, `rows=${rowsAfterRemove}`)

    await page.locator('.mc-dialog button[type="submit"]').click()
    await page.waitForSelector('.mc-dialog', { state: 'detached', timeout: 5000 })
    await page.waitForTimeout(800)

    const probe = (await api(`/api/v1/events?range=day&date=${firstDay}&tz=${encodeURIComponent(TZ)}`)).body?.events?.find((e) => e.title === '提醒探针')
    const times = (probe?.reminders ?? []).map((r) => r.remind_at_local)
    check(
      'the reminders were saved as absolute instants',
      times.length === 2 && times.includes(`${firstDay}T13:45:00+08:00`) && times.includes(`${firstDay}T13:30:00+08:00`),
      JSON.stringify(times),
    )

    // The standalone endpoints see exactly the same rows.
    const listed = await api(`/api/v1/reminders?event_id=${probe?.id}&tz=${encodeURIComponent(TZ)}`)
    check(
      'the standalone reminder API sees the same rows',
      listed.body?.count === 2,
      `count=${listed.body?.count}`,
    )

    // Reopen and confirm the times come back, then clear them all.
    await page.locator('.mc-calendar .fc-timegrid-event', { hasText: '提醒探针' }).first().click({ position: { x: 24, y: 10 } })
    await page.waitForSelector('.mc-dialog', { timeout: 5000 })
    const reopened = await page.locator('.mc-dialog input[aria-label^="第"][aria-label$="个提醒时间"]').count()
    check('reopening the event shows its reminders', reopened === 2, `rows=${reopened}`)

    await page.locator('.mc-dialog button[aria-label="删除第 1 个提醒"]').click()
    await page.locator('.mc-dialog button[aria-label="删除第 1 个提醒"]').click()
    await page.locator('.mc-dialog button[type="submit"]').click()
    await page.waitForSelector('.mc-dialog', { state: 'detached', timeout: 5000 })
    await page.waitForTimeout(800)

    const afterClear = await api(`/api/v1/reminders?event_id=${probe?.id}&tz=${encodeURIComponent(TZ)}`)
    check('removing every reminder clears the set', afterClear.body?.count === 0, `count=${afterClear.body?.count}`)
  }

  // ---------------------------------------------------------------- delete

  page.once('dialog', (d) => d.accept())
  await openEditor('新建的日程')
  await page.locator('.mc-dialog .mc-btn--danger').click()
  await page.waitForSelector('.mc-dialog', { state: 'detached', timeout: 5000 })
  await page.waitForTimeout(700)

  const after = await api(`/api/v1/events?range=day&date=${dateOnly(lastDay)}&tz=${encodeURIComponent(TZ)}`)
  check('deleting removes the event', !(after.body?.events ?? []).some((e) => e.title === '新建的日程'), (after.body?.events ?? []).map((e) => e.title).join('|'))

  // ----------------------------------------------------------- navigation

  const labelBefore = await page.locator('.mc-calendar .fc-toolbar-title').textContent()
  await page.locator('.mc-calendar .fc-next-button').click()
  await page.waitForTimeout(600)
  const labelAfter = await page.locator('.mc-calendar .fc-toolbar-title').textContent()
  check('next moves the window', labelBefore !== labelAfter, `${labelBefore} -> ${labelAfter}`)

  await page.locator('.mc-calendar .fc-today-button').click()
  await page.waitForTimeout(600)
  const labelToday = await page.locator('.mc-calendar .fc-toolbar-title').textContent()
  check('today returns to the current window', labelToday === labelBefore, `${labelToday} vs ${labelBefore}`)

  // ------------------------------------------------------------ event chip

  {
    const chipLine = (title) =>
      page.evaluate((t) => {
        const root = document.querySelector('.mc-calendar')
        const ev = [...root.querySelectorAll('.fc-timegrid-event, .fc-daygrid-event')].find((e) => e.textContent.includes(t))
        if (!ev) return null
        return {
          title: ev.querySelector('.fc-event-title')?.textContent,
          lines: [...ev.querySelectorAll('.mc-chip__line')].map((el) => {
            const style = getComputedStyle(el)
            return {
              text: el.textContent,
              nowrap: style.whiteSpace === 'nowrap',
              ellipsis: style.textOverflow === 'ellipsis',
              truncated: el.scrollWidth > el.clientWidth,
            }
          }),
        }
      }, title)

    const today2 = await page.evaluate(() => document.querySelector('.mc-calendar .fc-col-header-cell')?.getAttribute('data-date'))
    const at2 = (n) =>
      page.evaluate((d) => Math.floor(new Date(d).getTime() / 1000), `${today2}T${String(Math.floor(n)).padStart(2, '0')}:${n % 1 ? '30' : '00'}:00`)
    const longText = '某某国际中心 A 座 18 层 1808 会议室（出电梯右手边走到头再左转，靠窗那一侧的那一间）'

    await api(`/api/v1/events?tz=${encodeURIComponent(TZ)}`, {
      method: 'POST',
      body: JSON.stringify({ title: '细节探针', start_at: await at2(9), end_at: await at2(12), location: longText, notes: '记得带笔记本，以及充电器、转接头、纸质材料和上次会议的纪要，进门先找行政拿访客卡' }),
    })

    // A half-hour block at 48px per hour has room for a single line, which is
    // the case where the time has to share the row with the title. Seeded before
    // the reload: creating it afterwards leaves it in the database but not in the
    // already-rendered grid.
    await api(`/api/v1/events?tz=${encodeURIComponent(TZ)}`, {
      method: 'POST',
      body: JSON.stringify({ title: '短块探针', start_at: await at2(14), end_at: await at2(14.5), location: '三楼会议室', notes: '记得带笔记本' }),
    })

    await page.reload({ waitUntil: 'networkidle' })
    await page.waitForSelector('.mc-calendar .fc-timegrid-event', { timeout: 15000 })
    await page.waitForTimeout(700)

    const chip = await chipLine('细节探针')
    check('the chip shows the location', chip?.lines.some((l) => l.text.startsWith('某某国际中心')) === true, JSON.stringify(chip?.lines.map((l) => l.text.slice(0, 8))))
    check('the chip shows the notes on their own line', chip?.lines.some((l) => l.text.startsWith('记得带笔记本')) === true)
    check('the title still reads first', chip?.title === '细节探针', chip?.title)
    check(
      'both extra lines are single-line and ellipsis-truncated',
      chip?.lines.every((l) => l.nowrap && l.ellipsis) === true && chip?.lines.every((l) => l.truncated) === true,
      JSON.stringify(chip?.lines.map((l) => [l.nowrap, l.ellipsis, l.truncated])),
    )
    // Title above time, the other way round from FullCalendar's default.
    const order = await page.evaluate(() => {
      const root = document.querySelector('.mc-calendar')
      const ev = [...root.querySelectorAll('.fc-timegrid-event')].find((e) => e.textContent.includes('细节探针'))
      const title = ev?.querySelector('.fc-event-title')?.getBoundingClientRect()
      const time = ev?.querySelector('.fc-event-time')?.getBoundingClientRect()
      return title && time ? { titleY: title.y, timeY: time.y, titleH: title.height, timeH: time.height } : null
    })
    check('the title comes first, the time underneath', (order?.titleY ?? 0) < (order?.timeY ?? 0), `titleY=${Math.round(order?.titleY ?? -1)} timeY=${Math.round(order?.timeY ?? -1)}`)
    check(
      'lines keep their natural height rather than being squashed',
      (order?.titleH ?? 0) >= 12 && (order?.timeH ?? 0) >= 12,
      `titleH=${Math.round(order?.titleH ?? -1)} timeH=${Math.round(order?.timeH ?? -1)}`,
    )

    // A block with room for only one line puts the time and title together.
    const shortBlock = await page.evaluate(() => {
      const root = document.querySelector('.mc-calendar')
      const ev = [...root.querySelectorAll('.fc-timegrid-event')].find((e) => e.textContent.includes('短块探针'))
      if (!ev) return null
      const rect = (el) => { const r = el.getBoundingClientRect(); return { x: r.x, y: r.y } }
      const title = ev.querySelector('.fc-event-title')
      const time = ev.querySelector('.fc-event-time')
      return {
        blockH: Math.round(ev.getBoundingClientRect().height),
        time: rect(time),
        title: rect(title),
        comma: getComputedStyle(time, '::after').content,
        timeClipped: time.scrollWidth > time.clientWidth + 1,
        titleClipped: title.scrollWidth > title.clientWidth + 1,
        extras: [...ev.querySelectorAll('.mc-chip__line')].filter((e) => getComputedStyle(e).display !== 'none').length,
      }
    })
    check('a short block fits on one line', shortBlock !== null && shortBlock.blockH <= 26, `blockH=${shortBlock?.blockH}`)
    check('its time and title share that line', Math.abs((shortBlock?.time.y ?? 0) - (shortBlock?.title.y ?? 99)) < 8)
    check('its time is written before the title', (shortBlock?.time.x ?? 99) < (shortBlock?.title.x ?? 0), `timeX=${Math.round(shortBlock?.time.x ?? -1)} titleX=${Math.round(shortBlock?.title.x ?? -1)}`)
    check('they are separated by a comma', shortBlock?.comma === '","', String(shortBlock?.comma))
    check('the time is still shown in full', shortBlock?.timeClipped === false)
    check('location and notes are dropped when there is no room', shortBlock?.extras === 0, `visible=${shortBlock?.extras}`)

    check(
      'the chip does not spill out of its block',
      await page.evaluate(() => {
        const root = document.querySelector('.mc-calendar')
        return [...root.querySelectorAll('.fc-timegrid-event')].every((ev) => {
          const inner = ev.querySelector('.mc-chip')
          if (!inner) return true
          return inner.getBoundingClientRect().height <= ev.getBoundingClientRect().height + 1
        })
      }),
    )
  }

  // ------------------------------------------------------------ appearance

  {
    const bg = () => page.evaluate(() => getComputedStyle(document.body).backgroundColor)

    // Headless reports prefers-color-scheme: light, so pin dark first and then
    // light; comparing 'auto' against a pinned mode would compare light to light.
    await page.getByRole('button', { name: '暗色' }).click()
    await page.waitForTimeout(250)
    const darkBg = await bg()
    await page.getByRole('button', { name: '亮色' }).click()
    await page.waitForTimeout(250)
    const lightBg = await bg()
    check('the appearance switcher pins light and dark', darkBg === 'rgb(19, 19, 20)' && lightBg === 'rgb(255, 255, 255)', `${darkBg} / ${lightBg}`)

    await page.reload({ waitUntil: 'networkidle' })
    await page.waitForSelector('.mc-sidebar', { timeout: 15000 })
    check('the theme choice survives a reload', (await bg()) === 'rgb(255, 255, 255)', await bg())

    await page.getByRole('button', { name: '自动' }).click()
    await page.waitForTimeout(250)
    const autoTheme = await page.evaluate(() => document.documentElement.dataset.theme)
    const systemDark = await page.evaluate(() => window.matchMedia('(prefers-color-scheme: dark)').matches)
    check('auto follows the system', autoTheme === (systemDark ? 'dark' : 'light'), `theme=${autoTheme} systemDark=${systemDark}`)

    // The appearance control lives under the sidebar notes, as requested.
    check(
      'the appearance control sits under the notes',
      await page.evaluate(() => {
        const facts = document.querySelector('.mc-sidebar .mc-facts')
        const theme = document.querySelector('.mc-sidebar .mc-theme')
        return Boolean(facts && theme && facts.compareDocumentPosition(theme) & Node.DOCUMENT_POSITION_FOLLOWING)
      }),
    )

    const todayCell = await page.evaluate(() => {
      const cell = document.querySelector('.mc-mini .fc-day-today')
      return cell ? getComputedStyle(cell).borderTopLeftRadius : null
    })
    check('today is a rounded rectangle, not a circle', todayCell === '6px', `border-radius=${todayCell}`)

    const bars = await page.evaluate(() => {
      const grid = document.querySelector('.mc-calendar .fc-scroller-liquid-absolute')
      return grid
        ? { width: getComputedStyle(grid).scrollbarWidth, gap: grid.offsetWidth - grid.clientWidth }
        : null
    })
    check('the time grid scrollbar is hidden', bars?.width === 'none' && bars?.gap === 0, `width=${bars?.width} gap=${bars?.gap}`)
  }

  check('no console errors during the whole run', consoleErrors.length === 0, consoleErrors.join(' | '))

  await page.screenshot({ path: 'calendar.png' })
} catch (err) {
  failures++
  results.push(`FAIL  run aborted: ${String(err.message ?? err).split('\n')[0]}`)
} finally {
  if (browser) await browser.close()
}

console.log(results.join('\n'))
console.log(`\n${results.length - failures}/${results.length} checks passed`)
process.exit(failures > 0 ? 1 : 0)
