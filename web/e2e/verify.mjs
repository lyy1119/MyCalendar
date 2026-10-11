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
const dateOnly = (d) => `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`
const atUtc = (base, offset, hour) =>
  Date.UTC(base.getUTCFullYear(), base.getUTCMonth(), base.getUTCDate() + offset, hour, 0, 0, 0)

let browser

try {
  // ------------------------------------------------------------ clean slate

  const nowSec = Math.floor(Date.now() / 1000)
  const existing = await api(`/api/v1/events?tz=UTC&from=${nowSec - 90 * 86400}&to=${nowSec + 90 * 86400}`)
  const stale = existing.body?.events ?? []
  for (const e of stale) await api(`/api/v1/events/${e.id}`, { method: 'DELETE' })
  check('clean slate', true, `removed ${stale.length} pre-existing event(s)`)

  // -------------------------------------------------------------- seed data

  const today = new Date()
  const post = (body) => api('/api/v1/events?tz=UTC', { method: 'POST', body: JSON.stringify(body) })

  const allDay = await post({ title: '全天日程', event_date: dateOnly(today) })
  const timed = await post({
    title: '定时日程',
    start_at: atUtc(today, 0, 10) / 1000,
    end_at: atUtc(today, 0, 11) / 1000,
    notes: '备注内容',
  })
  const multiDay = await post({
    title: '跨天日程',
    start_at: atUtc(today, 0, 9) / 1000,
    end_at: atUtc(today, 2, 12) / 1000,
  })
  const task = await post({
    title: '任务日程',
    start_at: atUtc(today, 1, 14) / 1000,
    end_at: atUtc(today, 1, 15) / 1000,
    is_task: true,
  })
  check(
    'events created through the API',
    [allDay, timed, multiDay, task].every((r) => r.status === 201),
    [allDay.status, timed.status, multiDay.status, task.status].join(','),
  )

  // ----------------------------------------------------------------- render

  browser = await chromium.launch()
  const page = await browser.newPage({ viewport: { width: 1600, height: 900 } })
  const consoleErrors = []
  page.on('console', (m) => {
    if (m.type() === 'error') consoleErrors.push(m.text())
  })
  page.on('pageerror', (e) => consoleErrors.push('pageerror: ' + e.message))

  await page.goto(BASE + '/', { waitUntil: 'networkidle' })
  await page.waitForSelector('.mc-calendar .fc-timegrid-col', { timeout: 15000 })
  await page.waitForTimeout(500)

  // Every query is scoped to .mc-calendar: the sidebar mini calendar renders
  // its own column headers, day cells and all-day events.
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

  // ------------------------------------------- click through to the editor

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

  const afterEdit = await api(`/api/v1/events/${timed.body.id}?tz=UTC`)
  check('saving persists the new title', afterEdit.body?.title === '定时日程（已改名）', `title=${afterEdit.body?.title}`)
  check('saving leaves untouched fields alone', afterEdit.body?.notes === '备注内容', `notes=${afterEdit.body?.notes}`)
  check('the grid shows the new title', (await page.locator('.mc-calendar .fc-timegrid-event', { hasText: '已改名' }).count()) > 0)

  // ------------------------------------------------------------ completion

  await openEditor('任务日程')
  await page.locator('.mc-dialog #mc-is-completed').check()
  await page.locator('.mc-dialog button[type="submit"]').click()
  await page.waitForSelector('.mc-dialog', { state: 'detached', timeout: 5000 })
  await page.waitForTimeout(600)
  const afterComplete = await api(`/api/v1/events/${task.body.id}?tz=UTC`)
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
  const created = await api(`/api/v1/events?range=day&date=${dateOnly(lastDay)}&tz=UTC`)
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
  const allDayList = await api(`/api/v1/events?range=day&date=${dayAfter}&tz=UTC`)
  check(
    'the all-day event was persisted as all-day',
    (allDayList.body?.events ?? []).some((e) => e.title === '新建全天' && e.is_all_day === true),
    JSON.stringify((allDayList.body?.events ?? []).map((e) => [e.title, e.is_all_day])),
  )

  // ---------------------------------------------------------------- delete

  page.once('dialog', (d) => d.accept())
  await openEditor('新建的日程')
  await page.locator('.mc-dialog .mc-btn--danger').click()
  await page.waitForSelector('.mc-dialog', { state: 'detached', timeout: 5000 })
  await page.waitForTimeout(700)

  const after = await api(`/api/v1/events?range=day&date=${dateOnly(lastDay)}&tz=UTC`)
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
