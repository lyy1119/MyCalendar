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
  onBack: () => void
  /** The list changed; the calendar has to refetch and recolour. */
  onChanged: () => void
}

/** The seeded rows the API refuses to delete. */
const PROTECTED = new Set<number>([CATEGORY_DELETED, CATEGORY_UNCATEGORIZED])

/**
 * Category management as its own page rather than a dialog.
 *
 * Each row saves on its own -- a rename when the field loses focus, a colour
 * when the picker stops moving -- because every row is an independent request
 * and a half-applied batch would be worse than one that failed.
 */
export function CategoriesPage({ categories, onBack, onChanged }: Props) {
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [newName, setNewName] = useState('')
  const [newColor, setNewColor] = useState('#1a73e8')
  // Colours are dragged, not clicked: the picker fires a change per pixel, so
  // the swatch follows the pointer locally and the request goes out once the
  // dragging stops.
  const [drafts, setDrafts] = useState<Record<number, string>>({})
  const colorTimers = useRef(new Map<number, number>())
  const firstInput = useRef<HTMLInputElement>(null)

  useEffect(() => {
    document.title = '分类 · MyCalendar'
    firstInput.current?.focus()
    return () => {
      document.title = 'MyCalendar'
    }
  }, [])

  // Leaving the page mid-drag must not leave timer callbacks writing to an
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
    // Deleting a category moves its events to the default one, so say so.
    if (!window.confirm(`删除分类「${category.name}」？该分类下的日程会移到「默认」。`)) return
    void run(async () => {
      await deleteCategory(category.id)
    })
  }

  return (
    <div className="mc-page">
      <header className="mc-page__head">
        <button type="button" className="mc-btn mc-btn--ghost" onClick={onBack}>
          ← 返回日历
        </button>
        <h1 className="mc-page__title">分类</h1>
        <p className="mc-page__lead">
          日程用所属分类的颜色显示。改名称或颜色会立刻作用到所有属于它的日程——日程里存的是分类 id，不是颜色。
        </p>
      </header>

      <div className="mc-page__body">
        <ul className="mc-cats">
          {categories.map((category) => {
            const nameFixed = category.id === CATEGORY_DELETED
            return (
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
                  disabled={busy || nameFixed}
                  title={nameFixed ? '「删除」是内置状态名称，不能修改；颜色可以改' : undefined}
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

                {category.id === CATEGORY_DELETED && (
                  <span className="mc-cats__tag" title="内置状态：名称固定，颜色可改">
                    内置 · 名称固定
                  </span>
                )}
                {category.id === CATEGORY_UNCATEGORIZED && (
                  <span className="mc-cats__tag" title="新日程默认进入这里">
                    内置 · 默认
                  </span>
                )}
                {!PROTECTED.has(category.id) && (
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
            )
          })}
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
            添加分类
          </button>
        </div>

        <p className="mc-hint">
          删除分类不会删除日程，该分类下的日程会移到「默认」。「删除」与「默认」是内置分类，不能删除；其中「删除」的名称固定，
          颜色仍可修改。
        </p>

        {error && (
          <p className="mc-error" role="alert">
            {error}
          </p>
        )}
      </div>
    </div>
  )
}
