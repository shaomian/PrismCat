import { Copy } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { copyText } from '@/lib/clipboard'

export function CopyTraceIdButton({ traceId }: { traceId: string }) {
  const { t } = useTranslation()

  return (
    <Button
      type="button"
      size="icon-xs"
      variant="ghost"
      className="relative z-10 shrink-0 text-muted-foreground hover:text-foreground"
      aria-label={t('traces.copy_id')}
      title={t('traces.copy_id')}
      onClick={async event => {
        event.stopPropagation()
        if (await copyText(traceId)) toast.success(t('traces.id_copied'))
        else toast.error(t('log_detail.copy_failed'))
      }}
    >
      <Copy className="size-3.5" />
    </Button>
  )
}
