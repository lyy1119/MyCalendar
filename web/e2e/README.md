# Frontend browser verification

`verify.mjs` drives the real UI in a headless browser and asserts the layout
and CRUD behaviour that unit tests cannot see:

- the 4-day view renders exactly four day columns, the week view exactly seven;
- all-day events are pinned in their own row **above** the scrolling hours;
- all day columns share a single scrolling time region, so hour lines stay
  aligned across days;
- clicking an event opens the editor prefilled, saving persists, a task can be
  completed, dragging empty grid space creates, the all-day row creates an
  all-day event, and deleting removes it;
- none of this produces a console error.

This is **optional** tooling. It is not needed to build or run MyCalendar and
its browser download is deliberately kept out of the main `web` install.

## Running it

```bash
# 1. A server to test against. Use a scratch database:
make build
MYCAL_DB=/tmp/mycalendar-e2e.db MYCAL_ADDR=127.0.0.1:8080 ./mycalendar

# 2. The browser, once:
cd web/e2e
npm install
npx playwright install --with-deps chromium

# 3. The checks:
BASE=http://127.0.0.1:8080 node verify.mjs
```

The last line is `N/N checks passed`, and a `calendar.png` screenshot of the
final state is written next to the script.

## Warning

The script begins by **deleting every event** it can see in a ±90 day window,
so that repeated runs are comparable. Point it at a scratch database, never at
one holding real calendar data.
