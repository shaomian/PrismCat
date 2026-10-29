import type { ReactNode } from 'react'
import type { LucideIcon } from 'lucide-react'
import { cn } from '@/lib/utils'

interface EmptyStateProps {
    icon?: LucideIcon
    title: ReactNode
    description?: ReactNode
    /** 可选的引导操作(按钮等) */
    action?: ReactNode
    className?: string
}

/** 页面级空状态统一容器:图标圆点 + 标题 + 可选描述/操作 */
export function EmptyState({ icon: Icon, title, description, action, className }: EmptyStateProps) {
    return (
        <div className={cn('flex flex-col items-center justify-center px-6 py-12 text-center', className)}>
            {Icon ? (
                <div className="mb-3 flex h-10 w-10 items-center justify-center rounded-full bg-muted text-muted-foreground">
                    <Icon className="h-5 w-5" />
                </div>
            ) : null}
            <p className="text-sm font-medium text-foreground">{title}</p>
            {description ? (
                <p className="mt-1.5 max-w-sm text-xs leading-relaxed text-muted-foreground">{description}</p>
            ) : null}
            {action ? <div className="mt-4">{action}</div> : null}
        </div>
    )
}
