import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { assignLogTrace, fetchLog, fetchTraceMetadata, type TraceMetadata, type RequestLog } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { CopyTraceIdButton } from '@/components/CopyTraceIdButton'
import { TraceSearch } from '@/components/TraceTaskManager'

interface EditorProps {
  log: RequestLog
  onChange?: (log: RequestLog) => void
}

export function TraceAssociationEditor(props: EditorProps) {
  return <Editor key={props.log.id} {...props} />
}

function Editor({ log, onChange }: EditorProps) {
  const { t } = useTranslation()
  const [target, setTarget] = useState('')
  const [busy, setBusy] = useState(false)
  const [editing, setEditing] = useState(false)
  const [observed, setObserved] = useState<TraceMetadata | null>(null)
  const metadata = observed && (observed.trace_association?.revision ?? 0) >= (log.trace_association?.revision ?? 0) ? observed : log
  const association = metadata.trace_association
  const explicit = association?.source === 'explicit' || (!!metadata.trace_id && !association)

  useEffect(() => {
    if (explicit) return
    let cancelled=false
    const load=()=>{void fetchTraceMetadata(log.id).then(value=>{if(!cancelled)setObserved(value)}).catch(()=>{})}
    load();const timer=window.setInterval(load,3000)
    return ()=>{cancelled=true;window.clearInterval(timer)}
  },[log.id,explicit])
  async function save(split: boolean) {
    if (busy || (!split && !target.trim())) return
    setBusy(true)
    try {
      await assignLogTrace(log.id, split ? '' : target.trim(), association?.revision ?? 0)
      const latest = await fetchTraceMetadata(log.id)
      setObserved(latest)
      onChange?.(await fetchLog(log.id))
      setEditing(false)
      toast.success(t('correlation.saved'))
    } catch (error) {
      toast.error((error as Error).message)
    } finally { setBusy(false) }
  }

  return <div className="space-y-2 text-xs">
    <div className="flex flex-wrap items-center gap-2">
      <span className="font-medium">{t('correlation.assignment')}</span>
      {metadata.trace_id ? <Link className="font-mono text-primary underline underline-offset-2 break-all" to={`/traces/${encodeURIComponent(metadata.trace_id)}`}>{metadata.trace_id}</Link> : <span className="text-muted-foreground">{t('correlation.unassigned')}</span>}
      {metadata.trace_id && <CopyTraceIdButton traceId={metadata.trace_id} />}
      {association && <span className="rounded border border-border px-1.5 py-0.5">{t(`correlation.sources.${association.source}`)}</span>}
      {!explicit && <Button size="sm" variant="ghost" disabled={busy} onClick={() => setEditing(!editing)}>{t('correlation.correct')}</Button>}
    </div>
    {explicit && <p className="text-muted-foreground">{t('correlation.explicit_locked')}</p>}
    {association && <p className="text-muted-foreground">{t(`correlation.reasons.${association.reason}`, { defaultValue: association.reason })}</p>}
    {association?.session_id && <p className="font-mono text-muted-foreground break-all">{t('correlation.session')}: {association.session_id}</p>}
    {association?.client && <p className="text-muted-foreground">{t('correlation.client')}: {association.client}</p>}
    {log.websocket && <div className="space-y-1 text-muted-foreground">
      <p className="font-mono break-all">WebSocket · {log.websocket.kind} · {log.websocket.connection_id}{log.websocket.stream_id ? ` / ${log.websocket.stream_id}` : ''}</p>
      {log.websocket.kind === 'response' && <p>{t('correlation.websocket_normalized')}</p>}
      {log.websocket.observation_error && <p className="text-warning">{t('correlation.websocket_incomplete')}: {log.websocket.observation_error}</p>}
    </div>}
    {association?.parents?.length ? <p className="font-mono text-muted-foreground break-all">{t('correlation.parents')}: {association.parents.join(', ')}</p> : null}
    {metadata.capture_run_id && <p className="font-mono text-muted-foreground break-all">{t('correlation.capture')}: {metadata.capture_run_id}</p>}
    {editing && !explicit && <fieldset disabled={busy} className="space-y-2 rounded border border-border p-3">
      <p className="text-muted-foreground">{t('correlation.correction_hint')}</p>
      <TraceSearch onSelect={setTarget} />
      <Input aria-label={t('correlation.target')} placeholder={t('correlation.target')} value={target} maxLength={200} disabled={busy} onChange={event => setTarget(event.target.value)} />
      <div className="flex gap-2">
        <Button size="sm" disabled={busy || !target.trim()} onClick={() => void save(false)}>{t('correlation.move')}</Button>
        <Button size="sm" variant="outline" disabled={busy} onClick={() => void save(true)}>{t('correlation.split')}</Button>
      </div>
    </fieldset>}
  </div>
}
