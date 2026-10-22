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
  // DELETE only marks an event, so the cleanup uses /remove; otherwise every
  // run would leave a deleted row behind and the trash would grow forever.
  for (const e of stale) await api(`/api/v1/events/${e.id}/remove`, { method: 'POST' })
  // User categories go too, so a second run on the same database sees the same
  // sidebar as the first: the built-ins are -1 and 0, everything else is ours.
  const existingCategories = await api('/api/v1/categories')
  const extraCategories = (existingCategories.body?.categories ?? []).filter((c) => c.id >= 1)
  for (const c of extraCategories) await api(`/api/v1/categories/${c.id}`, { method: 'DELETE' })
  check(
    'clean slate',
    true,
    `removed ${stale.length} event(s) and ${extraCategories.length} category/ies`,
  )

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

  // The category colours are user data, so the two ends of the range are the
  // interesting cases: a pale fill that can vanish into a white page and a
  // near-black one that can vanish into the dark theme.
  const paleCategory = await api('/api/v1/categories', {
    method: 'POST',
    body: JSON.stringify({ name: '浅色分类', color: '#fff59d' }),
  })
  const darkCategory = await api('/api/v1/categories', {
    method: 'POST',
    body: JSON.stringify({ name: '深色分类', color: '#101010' }),
  })
  await post({
    title: '浅色块',
    start_at: hourIn(today, 2, 10),
    end_at: hourIn(today, 2, 11),
    category_id: paleCategory.body?.id,
  })
  await post({
    title: '深色块',
    start_at: hourIn(today, 3, 10),
    end_at: hourIn(today, 3, 11),
    category_id: darkCategory.body?.id,
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
  // Editing existing events is locked by default, and the lock is a button
  // rather than a checkbox. Check the default, then unlock so the rest of this
  // suite can click through to the editor -- the earlier checks are read-only.
  const lockButton = page.locator('.mc-sidebar .mc-lock-btn')
  check('editing is locked by default', (await lockButton.getAttribute('aria-pressed')) === 'false')
  check(
    'the lock is a button labelled with its action',
    ((await lockButton.textContent()) ?? '').includes('开启修改'),
    await lockButton.textContent(),
  )

  // Locked still allows looking: clicking an event opens the details, with
  // every field read-only and no save or delete action.
  await page
    .locator('.mc-calendar .fc-timegrid-event', { hasText: '定时日程' })
    .first()
    .click({ position: { x: 24, y: 10 } })
  await page.waitForSelector('.mc-dialog', { timeout: 5000 })
  check(
    'a locked click opens the details, not the editor',
    (await page.locator('.mc-dialog').getAttribute('aria-label')) === '日程详情',
    await page.locator('.mc-dialog').getAttribute('aria-label'),
  )
  check('the details show the content', (await page.locator('.mc-dialog input').first().inputValue()) === '定时日程')
  check(
    'the read-only view has no save button',
    (await page.locator('.mc-dialog button[type="submit"]').count()) === 0,
  )
  check(
    'the read-only view has no delete button',
    (await page.locator('.mc-dialog .mc-btn--danger').count()) === 0,
  )
  check('the fields are disabled', await page.locator('.mc-dialog input').first().isDisabled())
  // ...but the way out is right there, rather than "go find the lock yourself".
  check(
    'the read-only view offers to unlock',
    (await page.locator('.mc-dialog .mc-btn', { hasText: '开启修改' }).count()) === 1,
  )
  await page.locator('.mc-dialog .mc-btn', { hasText: '关闭' }).click()
  await page.waitForSelector('.mc-dialog', { state: 'detached', timeout: 5000 })

  // Escape closes a dialog too, so the page can never be trapped behind one.
  await page
    .locator('.mc-calendar .fc-timegrid-event', { hasText: '定时日程' })
    .first()
    .click({ position: { x: 24, y: 10 } })
  await page.waitForSelector('.mc-dialog', { timeout: 5000 })
  await page.keyboard.press('Escape')
  await page.waitForTimeout(300)
  check('Escape closes the dialog', (await page.locator('.mc-dialog').count()) === 0)

  // Dragging empty grid space would normally create an event. Locked, it must
  // not: creating is only available from the sidebar button.
  const column = await page.locator('.mc-calendar .fc-timegrid-col').first().boundingBox()
  await page.mouse.move(column.x + column.width / 2, column.y + 120)
  await page.mouse.down()
  await page.mouse.move(column.x + column.width / 2, column.y + 200, { steps: 6 })
  await page.mouse.up()
  await page.waitForTimeout(400)
  check('a locked grid cannot create by dragging', (await page.locator('.mc-dialog').count()) === 0)

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

  // The jump-to-today button is called 本周 in the week view: the same command,
  // named for what it does there.
  check(
    'the week view calls it 本周',
    (await page.locator('.mc-calendar .fc-goWeek-button').isVisible()) &&
      !(await page.locator('.mc-calendar .fc-goToday-button').isVisible()),
  )
  check(
    '本周 is disabled while the current week is on screen',
    (await page.locator('.mc-calendar .fc-goWeek-button').isDisabled()) === true,
  )
  await page.locator('.mc-calendar .fc-next-button').click()
  await page.waitForTimeout(600)
  check(
    '本周 becomes usable once the week moves',
    (await page.locator('.mc-calendar .fc-goWeek-button').isDisabled()) === false,
  )
  await page.locator('.mc-calendar .fc-goWeek-button').click()
  await page.waitForTimeout(600)
  check(
    '本周 returns to the week containing today',
    (await page.locator('.mc-calendar .fc-goWeek-button').isDisabled()) === true,
  )

  // ------------------------------------------------ single day view
  await page.locator('.mc-calendar .fc-timeGridDay-button').click()
  await page.waitForTimeout(700)
  const dayView = await page.evaluate(() => ({
    headers: [...document.querySelectorAll('.mc-calendar .fc-col-header-cell')].map((e) => e.textContent.trim()),
    view: document.querySelector('.mc-calendar')?.getAttribute('data-view'),
  }))
  check('the day view shows exactly one column', dayView.headers.length === 1, `headers=${dayView.headers.join('|')}`)
  check('the day view is a real view type', dayView.view === 'timeGridDay', `view=${dayView.view}`)
  check(
    'the day view calls the jump button 今天',
    (await page.locator('.mc-calendar .fc-goToday-button').isVisible()) &&
      !(await page.locator('.mc-calendar .fc-goWeek-button').isVisible()),
  )

  await page.locator('.mc-calendar .fc-fourDay-button').click()
  await page.waitForTimeout(600)

  // 今天 in the 4-day view anchors today to the *leftmost* column rather than
  // leaving it wherever a page of prev/next happened to land.
  await page.locator('.mc-calendar .fc-next-button').click()
  await page.waitForTimeout(500)
  await page.locator('.mc-calendar .fc-goToday-button').click()
  await page.waitForTimeout(700)
  const anchored = await page.evaluate(() => ({
    first: document.querySelector('.mc-calendar .fc-col-header-cell')?.getAttribute('data-date'),
    columns: document.querySelectorAll('.mc-calendar .fc-col-header-cell').length,
  }))
  const todayIso = new Date().toISOString().slice(0, 10)
  check(
    '今天 puts today in the first column',
    anchored.first === todayIso && anchored.columns === 4,
    `first=${anchored.first} today=${todayIso} columns=${anchored.columns}`,
  )

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

  await lockButton.click()
  await page.waitForTimeout(200)
  check('unlocking editing takes effect', (await lockButton.getAttribute('aria-pressed')) === 'true')

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

  // Deleting is a mark, not a destroy: the row is still there, in category -1.
  const deleted = await api(
    `/api/v1/events?range=day&date=${dateOnly(lastDay)}&tz=${encodeURIComponent(TZ)}&category_id=-1`,
  )
  const marked = (deleted.body?.events ?? []).find((e) => e.title === '新建的日程')
  check('deleting keeps the row in the deleted category', marked?.category_id === -1, `category_id=${marked?.category_id}`)
  if (marked) await api(`/api/v1/events/${marked.id}/remove`, { method: 'POST' })

  // ----------------------------------------------------------- navigation

  const labelBefore = await page.locator('.mc-calendar .fc-toolbar-title').textContent()
  await page.locator('.mc-calendar .fc-next-button').click()
  await page.waitForTimeout(600)
  const labelAfter = await page.locator('.mc-calendar .fc-toolbar-title').textContent()
  check('next moves the window', labelBefore !== labelAfter, `${labelBefore} -> ${labelAfter}`)

  // The jump button is disabled exactly when the window already starts on
  // today -- not when today merely happens to be visible somewhere in it.
  const todayJumpState = async () =>
    page.evaluate(() => {
      const btn = document.querySelector('.mc-calendar .fc-goToday-button')
      const first = document.querySelector('.mc-calendar .fc-col-header-cell')?.getAttribute('data-date')
      return { disabled: btn?.disabled, first }
    })
  const startState = await todayJumpState()
  check(
    'today is enabled once the window has moved off today',
    startState.disabled === false,
    JSON.stringify(startState),
  )

  await page.locator('.mc-calendar .fc-goToday-button').click()
  await page.waitForTimeout(600)
  const labelToday = await page.locator('.mc-calendar .fc-toolbar-title').textContent()
  check('today returns to the current window', labelToday === labelBefore, `${labelToday} vs ${labelBefore}`)
  const home = await todayJumpState()
  check(
    'and it is disabled again once today is the first column',
    home.disabled === true,
    JSON.stringify(home),
  )

  // Now shift the window by a single day from the mini month, so today is in
  // the second column rather than absent: the button has somewhere to go, so it
  // must be usable. A real click is used because the mini month only reacts to
  // FullCalendar's own event handling.
  const yesterday = new Date(Date.now() - 86400000)
  const yesterdayCell = page.locator(
    `.mc-mini .fc-daygrid-day[data-date="${yesterday.getFullYear()}-${String(yesterday.getMonth() + 1).padStart(2, '0')}-${String(yesterday.getDate()).padStart(2, '0')}"]`,
  )
  const cellBox = await yesterdayCell.boundingBox()
  await page.mouse.click(cellBox.x + cellBox.width / 2, cellBox.y + cellBox.height / 2)
  await page.waitForTimeout(700)

  const offset = await todayJumpState()
  check(
    'today is enabled when it sits in a later column',
    offset.disabled === false && offset.first !== home.first,
    JSON.stringify(offset),
  )
  await page.locator('.mc-calendar .fc-goToday-button').click()
  await page.waitForTimeout(600)
  const back = await todayJumpState()
  check(
    'clicking it puts today back in the first column',
    back.first === home.first && back.disabled === true,
    JSON.stringify(back),
  )

  // ...including after selecting a slot in the past, which is when someone is
  // most likely to want it.
  await page.locator('.mc-calendar .fc-prev-button').click()
  await page.waitForTimeout(500)
  const col = await page.locator('.mc-calendar .fc-timegrid-col').nth(1).boundingBox()
  await page.mouse.move(col.x + col.width / 2, col.y + 130)
  await page.mouse.down()
  await page.mouse.move(col.x + col.width / 2, col.y + 210, { steps: 6 })
  await page.mouse.up()
  await page.waitForTimeout(700)

  if (await page.locator('.mc-dialog').count()) {
    await page.keyboard.press('Escape')
    await page.waitForTimeout(400)
  }

  const beforeToday = await page.locator('.mc-calendar .fc-toolbar-title').textContent()
  await page.locator('.mc-calendar .fc-goToday-button').click()
  await page.waitForTimeout(600)
  const afterToday = await page.locator('.mc-calendar .fc-toolbar-title').textContent()
  check(
    'today works after selecting in the past',
    afterToday === labelBefore && beforeToday !== afterToday,
    `${beforeToday} -> ${afterToday}`,
  )

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
        // The separator sits before the time element, not after it: attached to
        // the title it would be clipped away along with the title's text.
        comma: getComputedStyle(time, '::before').content,
        timeClipped: time.scrollWidth > time.clientWidth + 1,
        titleClipped: title.scrollWidth > title.clientWidth + 1,
        extras: [...ev.querySelectorAll('.mc-chip__line')].filter((e) => getComputedStyle(e).display !== 'none').length,
      }
    })
    check('a short block fits on one line', shortBlock !== null && shortBlock.blockH <= 26, `blockH=${shortBlock?.blockH}`)
    check('its time and title share that line', Math.abs((shortBlock?.time.y ?? 0) - (shortBlock?.title.y ?? 99)) < 8)
    // The title leads here too: it identifies the event, so it should not be
    // the thing pushed aside on a cramped block.
    check('its title is written before the time', (shortBlock?.title.x ?? 99) < (shortBlock?.time.x ?? 0), `titleX=${Math.round(shortBlock?.title.x ?? -1)} timeX=${Math.round(shortBlock?.time.x ?? -1)}`)
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

    // A month grid reads as a month grid only when the days are square; a taller
    // row turns it into a column of rectangles.
    const dayCell = await page.evaluate(() => {
      const frame = document.querySelector('.mc-mini .fc-daygrid-day-frame').getBoundingClientRect()
      return { w: frame.width, h: frame.height }
    })
    check(
      'mini month days are square',
      Math.abs(dayCell.w - dayCell.h) <= 1.5,
      `w=${dayCell.w.toFixed(1)} h=${dayCell.h.toFixed(1)}`,
    )

    const bars = await page.evaluate(() => {
      const grid = document.querySelector('.mc-calendar .fc-scroller-liquid-absolute')
      return grid
        ? { width: getComputedStyle(grid).scrollbarWidth, gap: grid.offsetWidth - grid.clientWidth }
        : null
    })
    check('the time grid scrollbar is hidden', bars?.width === 'none' && bars?.gap === 0, `width=${bars?.width} gap=${bars?.gap}`)
  }

  // -------------------------------------------------- colour and legibility
  // A solid block colour is only a design decision if the text on it is still
  // readable, so the assertion is the WCAG contrast ratio itself rather than a
  // screenshot someone has to squint at.
  const contrastInPage = async () =>
    page.evaluate(() => {
      const lin = (v) => (v / 255 <= 0.03928 ? v / 255 / 12.92 : ((v / 255 + 0.055) / 1.055) ** 2.4)
      const parse = (css) => {
        const nums = (css.match(/[\d.]+/g) ?? []).map(Number)
        return { r: nums[0], g: nums[1], b: nums[2], a: nums[3] ?? 1 }
      }
      const lum = ({ r, g, b }) => 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b)
      const ratio = (a, b) => {
        const [hi, lo] = lum(a) > lum(b) ? [lum(a), lum(b)] : [lum(b), lum(a)]
        return (hi + 0.05) / (lo + 0.05)
      }

      const pageBg = parse(getComputedStyle(document.querySelector('.mc-calendar')).backgroundColor)
      const chips = [...document.querySelectorAll('.mc-calendar .fc-timegrid-event')].map((el) => {
        const block = getComputedStyle(el)
        const main = el.querySelector('.fc-event-main')
        return {
          title: el.textContent.trim(),
          bg: parse(block.backgroundColor),
          border: parse(block.borderTopColor),
          text: parse(main ? getComputedStyle(main).color : block.color),
        }
      })
      return chips.map((chip) => ({
        title: chip.title,
        textOnFill: ratio(chip.text, chip.bg),
        // A pale fill on a white page reads through its border, not its fill,
        // so what matters is the stronger of the two.
        separation: Math.max(ratio(chip.bg, pageBg), ratio(chip.border, pageBg)),
        borderOnFill: ratio(chip.border, chip.bg),
      }))
    })

  const setTheme = async (label) => {
    await page.locator('.mc-segmented__item', { hasText: label }).click()
    await page.waitForTimeout(250)
  }

  for (const theme of ['亮色', '暗色']) {
    await setTheme(theme)
    const report = await contrastInPage()
    const wanted = ['浅色块', '深色块', '定时日程']
    const seen = report.filter((r) => wanted.some((t) => r.title.startsWith(t)))
    check(`${theme}：every filled block was found`, seen.length === wanted.length, report.map((r) => r.title).join('|'))
    const unreadable = seen.filter((r) => r.textOnFill < 4.5)
    check(
      `${theme}：text on a filled block is readable (>= 4.5:1)`,
      unreadable.length === 0,
      unreadable.map((r) => `${r.title} ${r.textOnFill.toFixed(2)}`).join(', '),
    )
    const invisible = seen.filter((r) => r.separation < 1.2)
    check(
      `${theme}：the block is distinguishable from the page behind it`,
      invisible.length === 0,
      seen.map((r) => `${r.title} ${r.separation.toFixed(2)}`).join(', '),
    )
    {
      // The active view button (4 天 / 周) is a filled control too, so the same
      // rule has to hold: black or white, whichever reads better on the accent.
      const active = await page.evaluate(() => {
        const btn = document.querySelector('.mc-calendar .fc-button-active')
        const s = getComputedStyle(btn)
        return { color: s.color, bg: s.backgroundColor }
      })
      const ratio = (() => {
        const parse = (css) => {
          const n = (css.match(/[\d.]+/g) ?? []).map(Number)
          return { r: n[0], g: n[1], b: n[2] }
        }
        const lin = (v) => (v / 255 <= 0.03928 ? v / 255 / 12.92 : ((v / 255 + 0.055) / 1.055) ** 2.4)
        const lum = ({ r, g, b }) => 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b)
        const a = lum(parse(active.color))
        const b = lum(parse(active.bg))
        return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05)
      })()
      check(
        `${theme}：the active view button is readable (>= 4.5:1)`,
        ratio >= 4.5,
        `color=${active.color} bg=${active.bg} ratio=${ratio.toFixed(2)}`,
      )
    }

    const edgeless = seen.filter((r) => r.borderOnFill < 1.05)
    check(
      `${theme}：the block has a visible edge`,
      edgeless.length === 0,
      seen.map((r) => `${r.title} ${r.borderOnFill.toFixed(2)}`).join(', '),
    )

    // The chip's own text must not be faded: an opacity on a title or a time
    // silently costs contrast that the colour choice was supposed to guarantee.
    const faded = await page.evaluate(() => {
      const found = []
      for (const chip of document.querySelectorAll('.mc-calendar .mc-chip')) {
        if (chip.closest('.mc-task')) continue
        for (const el of chip.querySelectorAll('*')) {
          const opacity = parseFloat(getComputedStyle(el).opacity)
          if (opacity < 1) found.push(`${el.className}:${opacity}`)
        }
      }
      return found
    })
    check(`${theme}：no text inside a block is faded`, faded.length === 0, faded.join(', '))

    const titleMatches = await page.evaluate(() => {
      return [...document.querySelectorAll('.mc-calendar .fc-timegrid-event')].map((el) => {
        const main = el.querySelector('.fc-event-main')
        const title = el.querySelector('.fc-event-title')
        return {
          title: el.textContent.trim().slice(0, 6),
          same: main && title ? getComputedStyle(main).color === getComputedStyle(title).color : null,
        }
      })
    })
    check(
      `${theme}：the title wears the block's computed text colour`,
      titleMatches.every((t) => t.same === true),
      titleMatches.filter((t) => t.same !== true).map((t) => t.title).join(', '),
    )
  }
  await setTheme('自动')

  // ----------------------------------------------------- sidebar category list
  // Which categories the grid draws is a stored preference, so the check is
  // that toggling one changes the grid *and* survives a reload.
  const sidebarRow = (name) => page.locator('.mc-sidebar .mc-catlist__row', { hasText: name })
  const gridTitles = () =>
    page.evaluate(() =>
      [...document.querySelectorAll('.mc-calendar .fc-timegrid-event .fc-event-title')].map((e) => e.textContent),
    )

  const rowCount = await page.locator('.mc-sidebar .mc-catlist__row').count()
  check('the sidebar lists every category', rowCount >= 2, `rows=${rowCount}`)
  check(
    'the deleted category starts hidden',
    (await sidebarRow('删除').locator('input').isChecked()) === false,
  )
  check(
    'ordinary categories start visible',
    (await sidebarRow('默认').locator('input').isChecked()) === true,
  )

  const beforeHide = await gridTitles()
  check('the pale category is on the grid to begin with', beforeHide.some((t) => t.startsWith('浅色块')))

  await sidebarRow('浅色分类').click()
  await page.waitForTimeout(700)
  const afterHide = await gridTitles()
  check('unticking a category removes its events', !afterHide.some((t) => t.startsWith('浅色块')), afterHide.join('|'))
  check(
    'and the others stay',
    afterHide.some((t) => t.startsWith('定时日程')),
    afterHide.join('|'),
  )

  // The choice is stored, not local state: reload keeps it.
  await page.reload({ waitUntil: 'networkidle' })
  await page.waitForSelector('.mc-sidebar .mc-catlist__row')
  await page.waitForTimeout(600)
  check(
    'the hidden category is still hidden after a reload',
    (await sidebarRow('浅色分类').locator('input').isChecked()) === false,
  )
  check(
    'and its events are still gone',
    !(await gridTitles()).some((t) => t.startsWith('浅色块')),
  )

  await sidebarRow('浅色分类').click()
  await page.waitForTimeout(700)
  check(
    'ticking it again brings the events back',
    (await gridTitles()).some((t) => t.startsWith('浅色块')),
  )

  // Switching the deleted category on asks the API for deleted events (they are
  // hidden server-side by default), so the toggle shows something rather than
  // an empty result. The event is made here so this does not depend on what the
  // earlier checks did with the trash.
  const trash = await post({
    title: '删除探针',
    start_at: hourIn(today, 1, 16),
    end_at: hourIn(today, 1, 17),
  })
  await api(`/api/v1/events/${trash.body?.id}`, { method: 'DELETE' })
  check(
    'the deleted event is gone while its category is off',
    !(await gridTitles()).some((t) => t.startsWith('删除探针')),
  )
  await sidebarRow('删除').click()
  await page.waitForTimeout(800)
  const withDeleted = await gridTitles()
  check(
    'enabling the deleted category shows what was deleted',
    withDeleted.some((t) => t.startsWith('删除探针')),
    withDeleted.join('|'),
  )
  await sidebarRow('删除').click()
  await page.waitForTimeout(800)
  check(
    'switching it off hides them again',
    !(await gridTitles()).some((t) => t.startsWith('删除探针')),
  )
  await api(`/api/v1/events/${trash.body?.id}/remove`, { method: 'POST' })

  // ------------------------------------------------------- category page
  // The gear opens a page of its own (a real path, so a refresh stays there),
  // not a dialog.
  await page.locator('.mc-sidebar .mc-gear-btn').click()
  await page.waitForSelector('.mc-page', { timeout: 5000 })
  check('the gear opens a page', new URL(page.url()).pathname === '/categories', page.url())
  const rows = await page.locator('.mc-page .mc-cats__row').count()
  check('the page lists the categories', rows >= 2, `rows=${rows}`)
  check(
    'the deleted category name is not editable',
    await page.locator('.mc-page .mc-cats__row').first().locator('input.mc-cats__name').isDisabled(),
  )
  check(
    'the default category name is editable',
    (await page.locator('.mc-page .mc-cats__row').nth(1).locator('input.mc-cats__name').isDisabled()) === false,
  )
  await page.locator('.mc-page .mc-cats__row').nth(1).locator('input.mc-cats__name').fill('默认（改过）')
  await page.locator('.mc-page .mc-cats__row').nth(1).locator('input.mc-cats__name').blur()
  await page.waitForTimeout(500)
  const renamed = await api('/api/v1/categories/0')
  check('renaming the default category is saved', renamed.body?.name === '默认（改过）', `name=${renamed.body?.name}`)
  await page.locator('.mc-page .mc-cats__row').nth(1).locator('input.mc-cats__name').fill('默认')
  await page.locator('.mc-page .mc-cats__row').nth(1).locator('input.mc-cats__name').blur()
  await page.waitForTimeout(500)

  await page.locator('.mc-page .mc-btn--ghost').click()
  await page.waitForSelector('.mc-calendar .fc-timegrid-col', { timeout: 5000 })
  check('back returns to the calendar', new URL(page.url()).pathname === '/', page.url())

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
