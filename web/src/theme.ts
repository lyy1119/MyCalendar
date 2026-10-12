/**
 * Theme selection.
 *
 * Three modes: follow the system, or pin light or dark. The resolved value is
 * written to `data-theme` on the root element, which is what the stylesheet
 * keys off. An inline script in index.html applies the same resolution before
 * the first paint so there is no flash of the wrong theme.
 */

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
