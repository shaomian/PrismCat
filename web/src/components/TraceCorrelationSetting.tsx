import { useEffect, useRef, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { updateConfig } from '@/lib/api'
import { useCorrelationStatus } from '@/lib/trace-capture'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'

export function TraceCorrelationSetting() {
  const { t } = useTranslation()
  const { status, error, refresh } = useCorrelationStatus()
  const [busy, setBusy] = useState(false)
  const section = useRef<HTMLElement>(null)
  const { hash } = useLocation()
  useEffect(() => {
    if (hash !== '#trace-correlation') return
    const frame = requestAnimationFrame(() => section.current?.scrollIntoView({ block: 'start' }))
    return () => cancelAnimationFrame(frame)
  }, [hash])
  async function change(enabled: boolean) {
    setBusy(true)
    try {
      await updateConfig({ trace_correlation: { enabled } })
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
    {status && status.dropped_observations > 0 && <p className="mt-3 text-xs text-warning">{t('correlation.dropped', { count: status.dropped_observations })}</p>}
    {error && <div className="mt-3 flex items-center gap-2 text-xs"><p role="alert" className="text-danger">{error}</p><Button variant="link" size="xs" disabled={busy} onClick={() => void refresh()}>{t('common.refresh')}</Button></div>}
  </section>
}
