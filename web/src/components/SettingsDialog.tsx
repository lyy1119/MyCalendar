import { useEffect, useRef, useState } from 'react'
import {
  ApiError,
  CATEGORY_DELETED,
  CATEGORY_UNCATEGORIZED,
  createCategory,
  deleteCategory,
  updateCategory,
  type ApiCategory,
} from '../api'
interface Props {
  categories: ApiCategory[]
  onClose: () => void
  /** The list changed; the calendar has to refetch and recolour. */
  onChanged: () => void
}

/** The seeded rows the API refuses to delete. */
const PROTECTED = new Set<number>([CATEGORY_DELETED, CATEGORY_UNCATEGORIZED])

/**
 * Category management: add, rename, recolour, remove.
 *
 * Saves happen per row rather than on a submit button, because each row is an
 * independent API call and a half-applied batch is worse than a single failed
 * one.
 */
export function SettingsDialog({ categories, onClose, onChanged }: Props) {
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [newName, setNewName] = useState('')
  const [newColor, setNewColor] = useState('#4a90e2')
  // Colours are dragged, not clicked: the native picker fires a change per
  // pixel, so the swatch follows the pointer locally and the request only
  // goes out once the dragging stops.
  const [drafts, setDrafts] = useState<Record<number, string>>({})
  const colorTimers = useRef(new Map<number, number>())
  const firstInput = useRef<HTMLInputElement>(null)

  useEffect(() => {
    firstInput.current?.focus()
  }, [])

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !busy) onClose()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [onClose, busy])

  // Closing the dialog mid-drag must not leave timer callbacks writing to an
  // unmounted component.
  useEffect(() => {
    const timers = colorTimers.current
    return () => {
      for (const timer of timers.values()) window.clearTimeout(timer)
      timers.clear()
    }
  }, [])

  const run = async (action: () => Promise<void>) => {
    setBusy(true)
    setError(null)
    try {
      await action()
      onChanged()
    } catch (err) {
      setError(err instanceof ApiError ? err.fullMessage : String(err))
    } finally {
      setBusy(false)
    }
  }

  const dropDraft = (id: number) =>
    setDrafts((prev) => {
      const next = { ...prev }
      delete next[id]
      return next
    })

  const stageColor = (category: ApiCategory, color: string) => {
    setDrafts((prev) => ({ ...prev, [category.id]: color }))
    const pending = colorTimers.current.get(category.id)
    if (pending !== undefined) window.clearTimeout(pending)
    colorTimers.current.set(
      category.id,
      window.setTimeout(() => {
        colorTimers.current.delete(category.id)
        // Dragged back to where it started: nothing to save.
        if (color === category.color) {
          dropDraft(category.id)
          return
        }
        void run(async () => {
          try {
            await updateCategory(category.id, { color })
          } finally {
            // On success the refetched list carries the canonical colour; on
            // failure the row falls back to the colour the server still has.
            dropDraft(category.id)
          }
        })
      }, 350),
    )
  }

  const add = () =>
    run(async () => {
      const name = newName.trim()
      if (name === '') {
        throw new ApiError(0, 'validation_failed', '分类名称不能为空')
      }
      await createCategory({ name, color: newColor })
      setNewName('')
    })

  const remove = (category: ApiCategory) => {
    // Deleting a category moves its events to uncategorized, so say so.
    if (!window.confirm(`删除分类「${category.name}」？该分类下的日程会移到「未分类」。`)) return
    void run(async () => {
      await deleteCategory(category.id)
    })
  }

  return (
    <div
      className="mc-overlay"
      role="presentation"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget && !busy) onClose()
      }}
    >
      <div className="mc-dialog" role="dialog" aria-modal="true" aria-label="设置">
        <header className="mc-dialog__head">
          <h2>设置 · 分类</h2>
          <button type="button" className="mc-icon-btn" onClick={onClose} disabled={busy} aria-label="关闭">
            ✕
          </button>
        </header>

        <div className="mc-dialog__body">
          <ul className="mc-cats">
            {categories.map((category) => (
              <li className="mc-cats__row" key={category.id}>
                <label
                  className="mc-cats__swatch"
                  style={{ backgroundColor: drafts[category.id] ?? category.color }}
                  title="修改颜色"
                >
                  <input
                    type="color"
                    aria-label={`「${category.name}」的颜色`}
                    value={drafts[category.id] ?? category.color}
                    disabled={busy}
                    onChange={(e) => stageColor(category, e.target.value)}
                  />
                </label>

                <input
                  className="mc-input mc-cats__name"
                  aria-label={`「${category.name}」的名称`}
                  defaultValue={category.name}
                  maxLength={100}
                  disabled={busy}
                  onBlur={(e) => {
                    const name = e.target.value.trim()
                    if (name === '' || name === category.name) {
                      e.target.value = category.name
                      return
                    }
                    void run(async () => {
                      await updateCategory(category.id, { name })
                    })
                  }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') e.currentTarget.blur()
                  }}
                />

                {PROTECTED.has(category.id) ? (
                  <span className="mc-cats__tag">内置</span>
                ) : (
                  <button
                    type="button"
                    className="mc-icon-btn"
                    aria-label={`删除分类「${category.name}」`}
                    disabled={busy}
                    onClick={() => remove(category)}
                  >
                    ✕
                  </button>
                )}
              </li>
            ))}
          </ul>

          <div className="mc-cats__add">
            <label className="mc-cats__swatch" style={{ backgroundColor: newColor }} title="新分类的颜色">
              <input
                type="color"
                aria-label="新分类的颜色"
                value={newColor}
                disabled={busy}
                onChange={(e) => setNewColor(e.target.value)}
              />
            </label>
            <input
              ref={firstInput}
              className="mc-input mc-cats__name"
              aria-label="新分类名称"
              placeholder="新分类名称"
              value={newName}
              maxLength={100}
              disabled={busy}
              onChange={(e) => setNewName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') void add()
              }}
            />
            <button type="button" className="mc-btn mc-btn--primary" disabled={busy} onClick={() => void add()}>
              添加
            </button>
          </div>

          <p className="mc-hint">
            删除分类不会删除日程，该分类下的日程会移到「未分类」。「删除」与「未分类」是内置分类，不能删除。
          </p>

          {error && <p className="mc-error" role="alert">{error}</p>}

          <footer className="mc-dialog__foot">
            <div className="mc-dialog__spacer" />
            <button type="button" className="mc-btn" onClick={onClose} disabled={busy}>
              完成
            </button>
          </footer>
        </div>
      </div>
    </div>
  )
}
