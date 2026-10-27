import { useEffect, useId, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Loader2, RefreshCw } from 'lucide-react'
import { syncAnalysisModels, updateConfig, type AnalysisModel, type AppConfig } from '@/lib/api'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

const selectClass = 'border-input bg-background text-foreground h-9 w-full min-w-0 rounded-md border px-3 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px] disabled:opacity-50'

export default function AnalysisModelSettings({ value, onSaved, onClose }: {
  value: AppConfig['analysis'] | null
  onSaved: (value: AppConfig['analysis']) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const id = useId()
  const [protocol, setProtocol] = useState<'openai' | 'anthropic'>(value?.protocol || 'openai')
  const [base, setBase] = useState(value?.base_url ?? '')
  const [model, setModel] = useState(value?.model ?? '')
  const [key, setKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [models, setModels] = useState<AnalysisModel[]>([])
  const [manual, setManual] = useState(true)
  const [syncing, setSyncing] = useState(false)
  const [syncNotice, setSyncNotice] = useState('')
  const [syncError, setSyncError] = useState('')
  const controller = useRef<AbortController | null>(null)
  useEffect(() => () => { controller.current?.abort() }, [])

  const resetModels = () => {
    setModels([])
    setManual(true)
    setSyncNotice('')
    setSyncError('')
  }
  const sync = async () => {
    if (busy || syncing || !base.trim()) return
    const request = new AbortController()
    controller.current = request
    setSyncing(true)
    setSyncNotice('')
    setSyncError('')
    try {
      const result = await syncAnalysisModels({ protocol, base_url: base.trim(), ...(key.trim() ? { api_key: key.trim() } : {}) }, request.signal)
      if (request.signal.aborted) return
      setModels(result.models)
      setManual(result.models.length === 0)
      setSyncNotice(result.models.length ? t(result.truncated ? 'analysis.models_truncated' : 'analysis.models_synced', { count: result.models.length }) : t('analysis.models_empty'))
    } catch (e) {
      if (!request.signal.aborted) {
        const code = e instanceof Error ? e.message : ''
        const key = `analysis.errors.${code}`
        setSyncError(t(key) === key ? t('analysis.models_failed') : t(key))
        setManual(true)
      }
    } finally {
      if (controller.current === request) { controller.current = null; setSyncing(false) }
    }
  }
  const save = async () => {
    if (busy || syncing || !base.trim() || !model.trim()) return
    setBusy(true)
    setError('')
    const next = { enabled: true, protocol, base_url: base.trim(), model: model.trim(), has_api_key: !!key.trim() || !!value?.has_api_key }
    try {
      await updateConfig({ analysis: { enabled: true, protocol, base_url: next.base_url, model: next.model, ...(key.trim() ? { api_key: key.trim() } : {}) } })
      onSaved(next)
    } catch (e) {
      setError(e instanceof Error ? e.message : t('errors.update_config'))
    } finally {
      setBusy(false)
    }
  }

  return <Dialog open onOpenChange={open => { if (!open && !busy) onClose() }}>
    <DialogContent showCloseButton={!busy} className="max-h-[85dvh] overflow-y-auto">
      <DialogHeader>
        <DialogTitle>{t('analysis.model_settings')}</DialogTitle>
        <DialogDescription>{t('analysis.model_settings_hint')}</DialogDescription>
      </DialogHeader>
      <form className="space-y-4" onSubmit={event => { event.preventDefault(); void save() }}>
        <div className="space-y-1.5">
          <Label htmlFor={`${id}-protocol`}>{t('analysis.protocol')}</Label>
          <select id={`${id}-protocol`} className={selectClass} value={protocol} disabled={busy || syncing} onChange={e => { setProtocol(e.target.value === 'anthropic' ? 'anthropic' : 'openai'); resetModels() }}>
            <option value="openai">{t('analysis.protocol_openai')}</option>
            <option value="anthropic">{t('analysis.protocol_anthropic')}</option>
          </select>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor={`${id}-base`}>{t('analysis.base_url')}</Label>
          <Input id={`${id}-base`} value={base} onChange={e => { setBase(e.target.value); resetModels() }} placeholder={protocol === 'anthropic' ? 'https://api.anthropic.com' : 'https://example.com/v1'} autoComplete="off" disabled={busy || syncing} />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor={`${id}-key`}>{t('analysis.api_key')}</Label>
          <Input id={`${id}-key`} type="password" value={key} onChange={e => { setKey(e.target.value); resetModels() }} placeholder={value?.has_api_key ? t('analysis.api_key_keep') : 'sk-...'} autoComplete="new-password" disabled={busy || syncing} />
        </div>
        <div className="space-y-1.5">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <Label htmlFor={`${id}-model`}>{t('analysis.model')}</Label>
            <div className="flex gap-1">
              {models.length > 0 && <Button type="button" size="xs" variant="ghost" disabled={busy || syncing} onClick={() => setManual(v => !v)}>{t(manual ? 'analysis.choose_synced_model' : 'analysis.add_model_manually')}</Button>}
              <Button type="button" size="xs" variant="outline" disabled={busy || syncing || !base.trim()} onClick={() => void sync()}>{syncing ? <Loader2 className="size-3 animate-spin" /> : <RefreshCw className="size-3" />}{t(syncing ? 'analysis.models_syncing' : 'analysis.sync_models')}</Button>
            </div>
          </div>
          {manual || models.length === 0 ? <Input id={`${id}-model`} value={model} onChange={e => setModel(e.target.value)} placeholder={t('analysis.model_manual_hint')} autoComplete="off" disabled={busy || syncing} /> : <select id={`${id}-model`} className={selectClass} value={model} disabled={busy || syncing} onChange={e => setModel(e.target.value)}>
            <option value="" disabled>{t('analysis.choose_model')}</option>
            {model && !models.some(item => item.id === model) && <option value={model}>{t('analysis.current_custom_model', { model })}</option>}
            {models.map(item => <option key={item.id} value={item.id}>{item.name === item.id ? item.id : `${item.name} · ${item.id}`}</option>)}
          </select>}
          <p className="text-xs text-muted-foreground">{t('analysis.model_picker_hint')}</p>
          {syncNotice && <p role="status" className="text-xs text-muted-foreground">{syncNotice}</p>}
          {syncError && <p role="alert" className="text-xs text-danger">{syncError}</p>}
        </div>
        {error && <p role="alert" className="text-xs text-danger">{error}</p>}
        <div className="flex gap-2">
          <Button type="submit" disabled={busy || syncing || !base.trim() || !model.trim()}>{busy && <Loader2 className="size-4 animate-spin" />}{t('common.save')}</Button>
          <Button type="button" variant="outline" disabled={busy} onClick={onClose}>{t('common.cancel')}</Button>
        </div>
      </form>
    </DialogContent>
  </Dialog>
}
