import { useEffect, useRef, useState } from 'react'
import { ApiError, createEvent, deleteEvent, updateEvent } from '../api'
import {
  REMINDER_OFFSETS,
  draftStart,
  draftToInput,
  toggleAllDay,
  type EventDraft,
} from '../eventMapping'
import { BROWSER_TZ, toDateTimeLocal } from '../time'

interface Props {
  draft: EventDraft
  onClose: () => void
  onSaved: (message: string) => void
}

/**
 * Create, edit and delete a single event.
 *
 * Deliberately plain: a handful of native inputs, one save and one delete.
 * The server owns all the normalisation rules, so this form only has to send
 * the fields the user actually changed.
 */
export function EventDialog({ draft: initialDraft, onClose, onSaved }: Props) {
  const [draft, setDraft] = useState<EventDraft>(initialDraft)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const titleRef = useRef<HTMLInputElement>(null)

  const isNew = draft.id === null
  const MAX_REMINDERS = 50

  // Focus the title so a keyboard-only flow works: click a slot, type, save.
  useEffect(() => {
    titleRef.current?.focus()
    titleRef.current?.select()
  }, [])

  // Escape closes, which is what everyone expects from a modal.
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !busy) onClose()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [onClose, busy])

  const patch = (fields: Partial<EventDraft>) => setDraft((d) => ({ ...d, ...fields }))

  const setReminder = (index: number, value: string) =>
    setDraft((d) => ({ ...d, reminders: d.reminders.map((r, i) => (i === index ? value : r)) }))

  const removeReminder = (index: number) =>
    setDraft((d) => ({ ...d, reminders: d.reminders.filter((_, i) => i !== index) }))

  const addReminder = () =>
    setDraft((d) => (d.reminders.length >= MAX_REMINDERS ? d : { ...d, reminders: [...d.reminders, ''] }))

  // Offsets are a convenience over an absolute remind_at: each one adds a
  // reminder at the event's start minus that much.
  const addOffset = (minutes: number) => {
    const start = draftStart(draft)
    if (!start) {
      setError('请先填写有效的开始时间，再使用快捷提醒')
      return
    }
    const at = toDateTimeLocal(new Date(start.getTime() - minutes * 60_000))
    setDraft((d) =>
      d.reminders.includes(at) || d.reminders.length >= MAX_REMINDERS
        ? d
        : { ...d, reminders: [...d.reminders, at] },
    )
  }

  const handleSave = async () => {
    const validation = draftToInput(draft)
    if (!validation.ok) {
      setError(validation.message)
      return
    }

    setBusy(true)
    setError(null)
    try {
      if (isNew) {
        await createEvent(validation.input, BROWSER_TZ)
        onSaved('已创建日程')
      } else {
        await updateEvent(draft.id as number, validation.input, BROWSER_TZ)
        onSaved('已保存修改')
      }
    } catch (err) {
      setError(err instanceof ApiError ? err.fullMessage : String(err))
      setBusy(false)
    }
  }

  const handleDelete = async () => {
    if (isNew) return
    if (!window.confirm(`确定删除「${draft.title || '未命名'}」吗？`)) return

    setBusy(true)
    setError(null)
    try {
      await deleteEvent(draft.id as number)
      onSaved('已删除日程')
    } catch (err) {
      setError(err instanceof ApiError ? err.fullMessage : String(err))
      setBusy(false)
    }
  }

  return (
    <div
      className="mc-overlay"
      role="presentation"
      onMouseDown={(e) => {
        // Only a click on the backdrop itself closes the dialog.
        if (e.target === e.currentTarget && !busy) onClose()
      }}
    >
      <div className="mc-dialog" role="dialog" aria-modal="true" aria-label={isNew ? '新建日程' : '编辑日程'}>
        <header className="mc-dialog__head">
          <h2>{isNew ? '新建日程' : '编辑日程'}</h2>
          <button type="button" className="mc-icon-btn" onClick={onClose} disabled={busy} aria-label="关闭">
            ✕
          </button>
        </header>

        <form
          className="mc-dialog__body"
          onSubmit={(e) => {
            e.preventDefault()
            void handleSave()
          }}
        >
          <label className="mc-field">
            <span className="mc-field__label">标题</span>
            <input
              ref={titleRef}
              className="mc-input"
              value={draft.title}
              maxLength={500}
              placeholder="添加标题"
              onChange={(e) => patch({ title: e.target.value })}
            />
          </label>

          <label className="mc-field mc-field--inline">
            <input
              type="checkbox"
              checked={draft.isAllDay}
              onChange={(e) => setDraft((d) => toggleAllDay(d, e.target.checked))}
            />
            <span>全天</span>
          </label>

          {draft.isAllDay ? (
            <label className="mc-field">
              <span className="mc-field__label">日期</span>
              <input
                type="date"
                className="mc-input"
                value={draft.eventDate}
                onChange={(e) => patch({ eventDate: e.target.value })}
              />
            </label>
          ) : (
            <div className="mc-field-row">
              <label className="mc-field">
                <span className="mc-field__label">开始</span>
                <input
                  type="datetime-local"
                  className="mc-input"
                  value={draft.start}
                  onChange={(e) => patch({ start: e.target.value })}
                />
              </label>
              <label className="mc-field">
                <span className="mc-field__label">结束</span>
                <input
                  type="datetime-local"
                  className="mc-input"
                  value={draft.end}
                  onChange={(e) => patch({ end: e.target.value })}
                />
              </label>
            </div>
          )}

          <label className="mc-field">
            <span className="mc-field__label">备注</span>
            <textarea
              className="mc-input mc-textarea"
              rows={3}
              value={draft.notes}
              placeholder="添加备注"
              onChange={(e) => patch({ notes: e.target.value })}
            />
          </label>

          <div className="mc-field">
            <span className="mc-field__label">提醒</span>
            {draft.reminders.length === 0 && <p className="mc-hint">尚未设置提醒</p>}
            {draft.reminders.map((value, index) => (
              // Index keys are fine here: rows are positional and carry no identity.
              <div className="mc-reminder" key={index}>
                <input
                  type="datetime-local"
                  className="mc-input"
                  aria-label={`第 ${index + 1} 个提醒时间`}
                  value={value}
                  onChange={(e) => setReminder(index, e.target.value)}
                />
                <button
                  type="button"
                  className="mc-icon-btn"
                  aria-label={`删除第 ${index + 1} 个提醒`}
                  onClick={() => removeReminder(index)}
                >
                  ✕
                </button>
              </div>
            ))}
            <div className="mc-reminder-actions">
              <button type="button" className="mc-btn mc-btn--small" onClick={addReminder}>
                ＋ 添加提醒
              </button>
              {REMINDER_OFFSETS.map((offset) => (
                <button
                  key={offset.minutes}
                  type="button"
                  className="mc-chip"
                  onClick={() => addOffset(offset.minutes)}
                >
                  {offset.label}
                </button>
              ))}
            </div>
          </div>

          <div className="mc-field mc-field--inline">
            <input
              id="mc-is-task"
              type="checkbox"
              checked={draft.isTask}
              onChange={(e) =>
                patch({ isTask: e.target.checked, isCompleted: e.target.checked ? draft.isCompleted : false })
              }
            />
            <label htmlFor="mc-is-task">这是一个任务</label>
          </div>

          {draft.isTask && (
            <div className="mc-field mc-field--inline">
              <input
                id="mc-is-completed"
                type="checkbox"
                checked={draft.isCompleted}
                onChange={(e) => patch({ isCompleted: e.target.checked })}
              />
              <label htmlFor="mc-is-completed">已完成</label>
            </div>
          )}

          {error && <p className="mc-error" role="alert">{error}</p>}

          <footer className="mc-dialog__foot">
            {!isNew && (
              <button type="button" className="mc-btn mc-btn--danger" onClick={() => void handleDelete()} disabled={busy}>
                删除
              </button>
            )}
            <div className="mc-dialog__spacer" />
            <button type="button" className="mc-btn" onClick={onClose} disabled={busy}>
              取消
            </button>
            <button type="submit" className="mc-btn mc-btn--primary" disabled={busy}>
              {busy ? '保存中…' : '保存'}
            </button>
          </footer>
        </form>
      </div>
    </div>
  )
}
