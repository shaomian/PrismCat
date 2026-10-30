import { useEffect, useRef, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { updateConfig } from '@/lib/api'
import { useCorrelationStatus } from '@/lib/trace-capture'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

const MIN_IDLE_MINUTES = 1
const MAX_IDLE_MINUTES = 1440
const MIN_MAX_ENTRIES = 16
const MAX_MAX_ENTRIES = 100000

export function TraceCorrelationSetting() {
  const { t } = useTranslation()
  const { status, error, refresh } = useCorrelationStatus()
  const [busy, setBusy] = useState(false)
  const [idleDraft, setIdleDraft] = useState('')
  const [maxDraft, setMaxDraft] = useState('')
  const [touched, setTouched] = useState(false)
  const section = useRef<HTMLElement>(null)
  const { hash } = useLocation()
  useEffect(() => {
    if (hash !== '#trace-correlation') return
    const frame = requestAnimationFrame(() => section.current?.scrollIntoView({ block: 'start' }))
    return () => cancelAnimationFrame(frame)
  }, [hash])

  // 草稿跟随状态回填;用户正在编辑时不覆盖
  useEffect(() => {
    if (status && !touched) {
      setIdleDraft(String(status.idle_minutes))
      setMaxDraft(String(status.max_entries))
    }
  }, [status, touched])

  async function change(enabled: boolean) {
    setBusy(true)
    try {
      await updateConfig({ trace_correlation: { enabled } })
      await refresh()
      toast.success(t('correlation.settings_saved'))
    } catch (error) { toast.error((error as Error).message) }
    finally { setBusy(false) }
  }

  const parsedIdle = Number(idleDraft)
  const parsedMax = Number(maxDraft)
  const idleValid = Number.isInteger(parsedIdle) && parsedIdle >= MIN_IDLE_MINUTES && parsedIdle <= MAX_IDLE_MINUTES
  const maxValid = Number.isInteger(parsedMax) && parsedMax >= MIN_MAX_ENTRIES && parsedMax <= MAX_MAX_ENTRIES
  const dirty = touched && status !== null
    && (idleDraft.trim() !== String(status.idle_minutes) || maxDraft.trim() !== String(status.max_entries))
  const tuningInvalid = touched && (!idleValid || !maxValid)

  async function saveTuning() {
    if (!idleValid || !maxValid || !status) return
    setBusy(true)
    try {
      await updateConfig({ trace_correlation: { idle_minutes: parsedIdle, max_entries: parsedMax } })
      setTouched(false)
      await refresh()
      toast.success(t('correlation.settings_saved'))
    } catch (error) { toast.error((error as Error).message) }
    finally { setBusy(false) }
  }

  return <section ref={section} id="trace-correlation" className="scroll-mt-20 rounded-lg border border-input bg-card px-6 py-5">
    <div className="flex items-start justify-between gap-5">
      <div className="min-w-0 space-y-2">
        <h3 className="text-sm font-semibold">{t('correlation.section_title')}</h3>
        <label htmlFor="trace-correlation-switch" className="block text-sm">{t('correlation.enabled')}</label>
        <p className="max-w-3xl text-xs leading-relaxed text-muted-foreground">{t('correlation.enabled_hint')}</p>
      </div>
      <Switch id="trace-correlation-switch" className="mt-1 shrink-0" checked={status?.enabled ?? false} disabled={!status || busy} onCheckedChange={next => void change(next)} />
    </div>
    {/* 自动关联的内存边界调优。显式 Trace ID 和单任务录制不走这套索引,不受影响 */}
    <div className="mt-4 grid gap-5 lg:grid-cols-2">
      <div className="space-y-2">
        <Label htmlFor="trace-correlation-idle">{t('correlation.idle_minutes_label')}</Label>
        <Input
          id="trace-correlation-idle"
          type="number"
          min={MIN_IDLE_MINUTES}
          max={MAX_IDLE_MINUTES}
          value={idleDraft}
          onChange={event => { setTouched(true); setIdleDraft(event.target.value) }}
          disabled={!status || busy}
          className="w-40"
        />
        <p className="max-w-3xl text-xs leading-relaxed text-muted-foreground">{t('correlation.idle_minutes_hint')}</p>
      </div>
      <div className="space-y-2">
        <Label htmlFor="trace-correlation-max">{t('correlation.max_entries_label')}</Label>
        <Input
          id="trace-correlation-max"
          type="number"
          min={MIN_MAX_ENTRIES}
          max={MAX_MAX_ENTRIES}
          value={maxDraft}
          onChange={event => { setTouched(true); setMaxDraft(event.target.value) }}
          disabled={!status || busy}
          className="w-40"
        />
        <p className="max-w-3xl text-xs leading-relaxed text-muted-foreground">{t('correlation.max_entries_hint')}</p>
      </div>
    </div>
    <div className="mt-3 flex items-center gap-3">
      <Button size="sm" variant="outline" disabled={!status || busy || !dirty || !idleValid || !maxValid} onClick={() => void saveTuning()}>
        {t('common.save')}
      </Button>
      {tuningInvalid && <p role="alert" className="text-xs text-danger">{t('correlation.tuning_invalid')}</p>}
    </div>
    {status && status.dropped_observations > 0 && <p className="mt-3 text-xs text-warning">{t('correlation.dropped', { count: status.dropped_observations })}</p>}
    {error && <div className="mt-3 flex items-center gap-2 text-xs"><p role="alert" className="text-danger">{error}</p><Button variant="link" size="xs" disabled={busy} onClick={() => void refresh()}>{t('common.refresh')}</Button></div>}
  </section>
}