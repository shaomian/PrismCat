import { useTranslation } from 'react-i18next'
import { CopyTraceIdButton } from '@/components/CopyTraceIdButton'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'

// Abbreviation is presentation-only. Links, tooltips and copying keep the original ID.
function shortTraceId(traceId: string) {
  const characters = Array.from(traceId)
  return characters.length <= 22 ? traceId : `${characters.slice(0, 13).join('')}…${characters.slice(-4).join('')}`
}

export function TraceIdReference({ traceId, showCopy = true }: { traceId: string; showCopy?: boolean }) {
  const { t } = useTranslation()
  return <div className="inline-flex min-w-0 max-w-full items-center gap-1 text-xs text-muted-foreground">
    <Tooltip>
      <TooltipTrigger asChild>
        <span tabIndex={0} aria-label={`${t('traces.trace_id')}: ${traceId}`} onClick={event => event.stopPropagation()} className="min-w-0 truncate rounded-sm font-mono font-normal outline-none focus-visible:outline-2 focus-visible:outline-primary">
          {shortTraceId(traceId)}
        </span>
      </TooltipTrigger>
      <TooltipContent side="bottom" align="start" className="max-w-[min(24rem,calc(100vw-2rem))] break-all font-mono text-xs">{traceId}</TooltipContent>
    </Tooltip>
    {showCopy && <CopyTraceIdButton traceId={traceId} contextual />}
  </div>
}
