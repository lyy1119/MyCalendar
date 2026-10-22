/**
 * Theme selection.
 *
 * Three modes: follow the system, or pin light or dark. The resolved value is
 * written to `data-theme` on the root element, which is what the stylesheet
 * keys off. An inline script in index.html applies the same resolution before
 * the first paint so there is no flash of the wrong theme.
 */

import { readableTextOn } from './eventMapping'

export type ThemeMode = 'auto' | 'light' | 'dark'
export type ResolvedTheme = 'light' | 'dark'

const STORAGE_KEY = 'mycalendar.theme'

export const THEME_OPTIONS: { mode: ThemeMode; label: string }[] = [
  { mode: 'auto', label: '自动' },
  { mode: 'light', label: '亮色' },
  { mode: 'dark', label: '暗色' },
]

/** The stored preference, or 'auto' when there is none. */
export function readThemeMode(): ThemeMode {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY)
    if (raw === 'light' || raw === 'dark' || raw === 'auto') return raw
  } catch {
    // Storage can be unavailable, for example in a locked-down browser.
  }
  return 'auto'
}

/** Whether the operating system asks for a dark UI. */
export function systemPrefersDark(): boolean {
  // Default to dark when the query is unsupported: it is the theme the layout
  // was designed against.
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? true
}

/** Turn a mode into the theme that should actually be shown. */
export function resolveTheme(mode: ThemeMode): ResolvedTheme {
  if (mode === 'auto') return systemPrefersDark() ? 'dark' : 'light'
  return mode
}

/** Apply a mode to the document. */
export function applyTheme(mode: ThemeMode): void {
  document.documentElement.setAttribute('data-theme', resolveTheme(mode))
  applyAccentInk()
}

/**
 * Sets `--mc-accent-ink`: the text colour that reads best on the accent.
 *
 * The accent differs per theme (a deep blue in light, a pale blue in dark), so
 * the text on it cannot be a constant: a fixed white would be 4.5:1 on one and
 * 1.3:1 on the other. Rather than hard-code a second literal per theme, the
 * same black-or-white decision the event blocks use is applied here, reading
 * the accent straight out of the stylesheet. That keeps one rule for every
 * filled surface.
 */
function applyAccentInk(): void {
  const root = document.documentElement
  const accent = window.getComputedStyle(root).getPropertyValue('--mc-accent').trim()
  if (!accent) return
  root.style.setProperty('--mc-accent-ink', readableTextOn(accent))
}

/** Persist a mode. Failure is not worth surfacing: the theme still applies. */
export function saveThemeMode(mode: ThemeMode): void {
  writeFlag(STORAGE_KEY, mode)
}

/* ------------------------------------------------------------ edit unlock */

const EDIT_KEY = 'mycalendar.editUnlocked'

/**
 * Whether editing existing events is unlocked.
 *
 * Off by default: the calendar opens read-only, and changing anything is a
 * deliberate act. Creating is never gated -- the sidebar button always works.
 */
export function readEditUnlocked(): boolean {
  try {
    return window.localStorage.getItem(EDIT_KEY) === 'true'
  } catch {
    return false
  }
}

/** Persist the edit unlock. */
export function saveEditUnlocked(unlocked: boolean): void {
  writeFlag(EDIT_KEY, unlocked ? 'true' : 'false')
}

function writeFlag(key: string, value: string): void {
  try {
    window.localStorage.setItem(key, value)
  } catch {
    // Ignore: the choice simply will not survive a reload.
  }
}
