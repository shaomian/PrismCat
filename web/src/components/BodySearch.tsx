import { useEffect, useRef, useState, type FormEvent } from 'react'
import { ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { ApiError, searchTraceBodies, type BodySearchHit, type BodySearchResult } from '@/lib/api'
import { formatDate } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

interface BodySearchProps {
    traceId: string
    requestCount: number
    visibleLogIds?: ReadonlySet<string>
    onOpenLog: (hit: BodySearchHit) => void
}

function hitPriority(hit: BodySearchHit) {
    if (!hit.tool) return 0
    return hit.part === 'response' ? 2 : 1
}

export function BodySearch({ traceId, requestCount, visibleLogIds, onOpenLog }: BodySearchProps) {
    const { t, i18n } = useTranslation()
    const [query, setQuery] = useState('')
    const [data, setResult] = useState<BodySearchResult | null>(null)
    const [busy, setBusy] = useState(false)
    const [error, setError] = useState('')
    const inFlight = useRef<AbortController | null>(null)
    const queryLength = Array.from(query).length

    useEffect(() => () => inFlight.current?.abort(), [])

    async function search(before?: BodySearchResult['next']) {
        if (!query.trim() || queryLength > 256 || inFlight.current) return
        const controller = new AbortController()
        inFlight.current = controller
        setBusy(true)
        setError('')
        if (!before) setResult(null)
        try {
            const batch = await searchTraceBodies(traceId, query, controller.signal, before)
            if (controller.signal.aborted) return
            setResult(previous => {
                if (!before || !previous) return batch
                const seen = new Set(previous.hits.map(hit => hit.log_id))
                const hits = [...previous.hits, ...batch.hits.filter(hit => !seen.has(hit.log_id))]
                hits.sort((a, b) => hitPriority(b) - hitPriority(a))
                return {
                    ...batch,
                    hits,
                    matched: hits.length,
                    scanned: previous.scanned + batch.scanned,
                    incomplete: previous.incomplete + batch.incomplete,
                    next: batch.limited && batch.scanned === 0 ? before : batch.next,
                }
            })
        } catch (err) {
            if (!controller.signal.aborted) {
                setError(err instanceof ApiError && err.status === 429 ? 'log_body_search.busy' : 'log_body_search.failed')
            }
        } finally {
            if (!controller.signal.aborted) {
                inFlight.current = null
                setBusy(false)
            }
        }
    }

    function submit(event: FormEvent) {
        event.preventDefault()
        void search()
    }

    const showTotal = data && (data.limited ? data.scanned < requestCount : data.scanned === requestCount)
    const status = busy ? t('log_body_search.searching') : data && [
        t(showTotal ? 'log_body_search.progress' : 'log_body_search.count', { scanned: data.scanned, total: requestCount }),
        data.matched > 0 ? t('log_body_search.matches', { count: data.matched }) : t('log_body_search.empty'),
    ].join(' · ')

    return (
        <section id="trace-body-search" aria-label={t('log_body_search.title')} className="space-y-2 rounded-lg border border-border bg-card p-3">
            <form onSubmit={submit} className="flex items-center gap-2">
                <Input
                    type="search"
                    className="h-8 min-w-0 flex-1"
                    aria-label={t('log_body_search.placeholder')}
                    title={t('log_body_search.trace_scope')}
                    placeholder={t('log_body_search.placeholder')}
                    value={query}
                    maxLength={512}
                    autoFocus
                    onChange={event => {
                        inFlight.current?.abort()
                        inFlight.current = null
                        setBusy(false)
                        setQuery(event.target.value)
                        setResult(null)
                        setError('')
                    }}
                />
                <Button type="submit" size="sm" disabled={busy || !query.trim() || queryLength > 256}>
                    {t('common.search')}
                </Button>
            </form>
            {queryLength > 256 && <p className="text-xs text-muted-foreground">{t('log_body_search.query_limit')}</p>}
            {error && <p className="text-sm text-destructive" role="alert">{t(error)}</p>}
            {(busy || data) && (
                <div className="flex flex-wrap items-center justify-between gap-x-2">
                    <p className="py-1 text-xs text-muted-foreground tabular-nums" role="status">{status}</p>
                    {data?.next && <Button type="button" size="sm" variant="ghost" disabled={busy} onClick={() => void search(data.next)}>
                        {t('log_body_search.continue')}
                        <ArrowRight className="size-3.5" aria-hidden="true" />
                    </Button>}
                </div>
            )}
            {data?.limited && !data.next && <p className="text-xs text-warning">{t('log_body_search.limited')}</p>}
            {!!data?.incomplete && <p className="text-xs text-warning">{t('log_body_search.incomplete', { count: data.incomplete })}</p>}
            {!!data?.hits.length && (
                <ul className="max-h-72 overflow-y-auto divide-y divide-border border-t border-border">
                    {data.hits.map(hit => (
                        <li key={hit.log_id}>
                            <button type="button" className="block w-full space-y-0.5 px-1.5 py-2 text-left hover:bg-accent focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring"
                                onClick={() => onOpenLog(hit)}>
                                <span className="flex flex-wrap gap-x-2 gap-y-0.5 text-xs text-muted-foreground">
                                    <span>{formatDate(hit.created_at, i18n.language)}</span>
                                    <span className="break-all">{t(`log_body_search.${hit.part}`)}{hit.tool ? ` · ${hit.tool}` : ''}</span>
                                    {visibleLogIds && !visibleLogIds.has(hit.log_id) && <span>{t('log_body_search.not_shown')}</span>}
                                </span>
                                <span className="block whitespace-pre-wrap break-all font-mono text-xs">
                                    {hit.before}<mark className="rounded-sm bg-warning/20 text-foreground">{hit.match}</mark>{hit.after}
                                </span>
                            </button>
                        </li>
                    ))}
                </ul>
            )}
        </section>
    )
}
