import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  User, Bot, Wrench, Terminal, Brain, Image as ImageIcon, Shield,
  ChevronDown, ChevronUp, AlertCircle, FoldVertical,
} from 'lucide-react'
import type { TimelineEvent, TimelineRequest } from '@/lib/api'
import { cn, formatLatency, getStatusColor } from '@/lib/utils'

const PREVIEW_CHARS = 600

const ROLE_STYLE: Record<string, { icon: typeof User; badge: string; accent: string }> = {
  system: { icon: Shield, badge: 'bg-purple-500/10 text-purple-500', accent: 'border-l-purple-500/50' },
  user: { icon: User, badge: 'bg-info/10 text-info', accent: 'border-l-info/50' },
  assistant: { icon: Bot, badge: 'bg-success/10 text-success', accent: 'border-l-success/50' },
  tool: { icon: Terminal, badge: 'bg-warning/10 text-warning', accent: 'border-l-warning/50' },
}

const FALLBACK_STYLE = { icon: User, badge: 'bg-muted text-muted-foreground', accent: 'border-l-border' }

function EventCard({ event, defaultExpanded }: { event: TimelineEvent; defaultExpanded?: boolean }) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(defaultExpanded ?? event.kind === 'message')
  const style = ROLE_STYLE[event.role] ?? FALLBACK_STYLE
  const Icon = event.kind === 'tool_call' ? Wrench
    : event.kind === 'tool_result' ? Terminal
    : event.kind === 'thinking' ? Brain
    : event.kind === 'image' ? ImageIcon
    : style.icon

  const rawText = event.kind === 'tool_call' ? (event.tool_args ?? '') : (event.content ?? '')
  const isLong = rawText.length > PREVIEW_CHARS
  const text = expanded || !isLong ? rawText : rawText.slice(0, PREVIEW_CHARS) + '…'

  return (
    <div className={cn('rounded-md border border-border border-l-2 bg-card p-2.5', style.accent)}>
      <div className="flex items-center gap-1.5 flex-wrap">
        <span className={cn('inline-flex items-center gap-1 h-5 px-1.5 rounded text-[11px] font-semibold', style.badge)}>
          <Icon className="h-3 w-3" />
          {t(`timeline.role.${event.role}`, event.role)}
        </span>
        {event.kind !== 'message' && (
          <span className="text-[11px] font-medium text-muted-foreground bg-muted/60 px-1.5 py-0.5 rounded">
            {t(`timeline.kind.${event.kind}`, event.kind)}
          </span>
        )}
        {event.tool_name && (
          <span className="font-mono text-xs font-semibold text-warning bg-warning/10 px-1.5 py-0.5 rounded">
            {event.tool_name}
          </span>
        )}
        {event.truncated && (
          <span className="text-[11px] text-muted-foreground/70">({t('timeline.truncated')})</span>
        )}
        {(isLong || event.kind === 'thinking') && (
          <button
            type="button"
            onClick={() => setExpanded(v => !v)}
            className="ml-auto inline-flex items-center gap-0.5 text-[11px] text-muted-foreground hover:text-foreground transition-colors"
          >
            {expanded ? <ChevronUp className="h-3 w-3" /> : <ChevronDown className="h-3 w-3" />}
            {expanded ? t('timeline.collapse') : t('timeline.expand')}
          </button>
        )}
      </div>
      {text && (expanded || event.kind !== 'thinking') && (
        <pre className="mt-1.5 whitespace-pre-wrap break-words font-mono text-xs leading-relaxed text-foreground/85 max-h-[480px] overflow-y-auto">
          {text}
        </pre>
      )}
    </div>
  )
}

function RequestGroup({ request, onOpenLog }: { request: TimelineRequest; onOpenLog?: (logId: string) => void }) {
  const { t } = useTranslation()
  const newEvents = request.new_events ?? []
  const responseEvents = request.response_events ?? []

  return (
    <div className="space-y-2">
      {/* 请求头 */}
      <div className="flex items-center gap-2 flex-wrap">
        <button
          type="button"
          onClick={() => onOpenLog?.(request.log_id)}
          className="text-xs font-mono font-medium text-info bg-info/10 hover:bg-info/20 px-1.5 py-0.5 rounded transition-colors"
          title={t('timeline.open_log')}
        >
          #{request.seq || '?'}
        </button>
        {request.model && (
          <span className="text-xs font-mono font-semibold text-foreground/80">{request.model}</span>
        )}
        <span className="text-xs text-muted-foreground bg-muted/50 px-1.5 py-0.5 rounded">{request.upstream}</span>
        <span className={cn('font-mono text-xs font-medium', getStatusColor(request.status_code))}>
          {request.status_code || '---'}
        </span>
        {request.error && <AlertCircle className="h-3 w-3 text-danger" />}
        <span className="ml-auto text-xs font-mono text-muted-foreground">{formatLatency(request.latency_ms)}</span>
        <span className="text-xs text-muted-foreground/60">{new Date(request.created_at).toLocaleTimeString()}</span>
      </div>

      {/* 折叠的历史消息 */}
      {request.carried > 0 && (
        <div className="flex items-center gap-2 px-2.5 py-1.5 rounded-md bg-muted/30 border border-dashed border-border text-xs text-muted-foreground">
          <FoldVertical className="h-3 w-3 shrink-0" />
          {t('timeline.carried', { count: request.carried })}
        </div>
      )}

      {/* 解析失败提示 */}
      {request.parse_error && (
        <div className="px-2.5 py-1.5 rounded-md bg-warning/5 border border-warning/20 text-xs text-warning">
          {t('timeline.parse_error', { reason: request.parse_error })}
        </div>
      )}

      {/* 新增消息 */}
      {newEvents.map((ev, i) => <EventCard key={`n-${i}`} event={ev} />)}

      {/* 响应事件 */}
      {responseEvents.length > 0 && (
        <div className="ml-4 space-y-2 pt-1">
          {responseEvents.map((ev, i) => <EventCard key={`r-${i}`} event={ev} />)}
        </div>
      )}
    </div>
  )
}

export function TraceTimelineView({ requests, onOpenLog }: { requests: TimelineRequest[]; onOpenLog?: (logId: string) => void }) {
  const { t } = useTranslation()
  if (requests.length === 0) {
    return <div className="py-12 text-center text-sm text-muted-foreground">{t('timeline.empty')}</div>
  }
  return (
    <div className="space-y-6">
      {requests.map(req => (
        <div key={req.log_id} className="relative">
          <RequestGroup request={req} onOpenLog={onOpenLog} />
        </div>
      ))}
    </div>
  )
}
