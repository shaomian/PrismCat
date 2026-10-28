import { useEffect, useRef, useState } from 'react'
import { Check, Copy } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { copyText } from '@/lib/clipboard'
import { cn } from '@/lib/utils'

export function CopyTraceIdButton({ traceId, className, contextual = false }: { traceId: string; className?: string; contextual?: boolean }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)
  useEffect(() => () => { if (timer.current) clearTimeout(timer.current) }, [])

  return (
    <Button
      type="button"
      size="icon-xs"
      variant="ghost"
      className={cn('relative z-10 shrink-0 text-muted-foreground hover:text-foreground', contextual && 'task-copy-action', className)}
      aria-label={t('traces.copy_id')}
      title={t('traces.copy_id')}
      onClick={async event => {
        event.stopPropagation()
        if (await copyText(traceId)) {
          if (contextual) {
            if (timer.current) clearTimeout(timer.current)
            setCopied(true)
            timer.current = setTimeout(() => { setCopied(false); timer.current = null }, 1600)
          } else toast.success(t('traces.id_copied'))
        } else toast.error(t('log_detail.copy_failed'))
      }}
    >
      {copied ? <Check className="size-3.5 text-primary" /> : <Copy className="size-3.5" />}
      {contextual && <span className="sr-only" aria-live="polite">{copied ? t('common.copied') : ''}</span>}
    </Button>
  )
}
