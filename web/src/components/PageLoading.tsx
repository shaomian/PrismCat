import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Spinner } from '@/components/ui/spinner'

/** 整页/区块级加载态:转圈 + 文案,替换各处手写的重复结构 */
export function PageLoading({ className }: { className?: string }) {
    const { t } = useTranslation()
    return (
        <div className={cn('flex min-h-[40vh] flex-col items-center justify-center gap-3', className)}>
            <Spinner className="h-6 w-6" />
            <div className="text-sm text-muted-foreground">{t('common.loading')}</div>
        </div>
    )
}
