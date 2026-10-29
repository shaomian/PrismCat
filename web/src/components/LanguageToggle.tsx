import { Globe } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

interface LanguageToggleProps {
    /** ghost: 侧边栏/移动顶栏小按钮;outline: 登录页带边框按钮 */
    variant?: 'ghost' | 'outline'
    className?: string
}

/** 中英切换按钮,三处布局共用,避免各自复制一份 */
export function LanguageToggle({ variant = 'ghost', className }: LanguageToggleProps) {
    const { t, i18n } = useTranslation()
    const isZh = i18n.language === 'zh'
    return (
        <button
            onClick={() => i18n.changeLanguage(isZh ? 'en' : 'zh')}
            className={cn(
                variant === 'ghost'
                    ? 'flex h-8 items-center justify-center gap-1.5 rounded-md px-2 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground'
                    : 'flex h-10 items-center justify-center gap-2 rounded-lg border border-border bg-accent/50 px-3 text-xs font-semibold text-muted-foreground transition-all hover:bg-accent hover:text-foreground active:scale-95 sm:min-w-[110px] sm:px-4',
                className
            )}
            aria-label={t('auth.switch_language')}
        >
            <Globe className="h-3.5 w-3.5" />
            <span>{variant === 'outline' ? (isZh ? 'English' : '中文') : (isZh ? 'EN' : '中')}</span>
        </button>
    )
}
