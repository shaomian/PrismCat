import { Moon, Sun } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useSyncExternalStore } from 'react'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { getTheme, subscribeTheme, toggleTheme } from '@/lib/theme'

export function ThemeToggle() {
    const { t } = useTranslation()
    const isDark = useSyncExternalStore(subscribeTheme, getTheme) === 'dark'

    const label = isDark ? t('common.theme_to_light') : t('common.theme_to_dark')

    return (
        <Tooltip>
            <TooltipTrigger asChild>
                <Button
                    variant="ghost"
                    size="icon"
                    onClick={toggleTheme}
                    className="h-9 w-9 rounded-md hover:bg-accent"
                    aria-label={label}
                >
                    {isDark ? (
                        <Sun className="h-[1.2rem] w-[1.2rem]" />
                    ) : (
                        <Moon className="h-[1.2rem] w-[1.2rem]" />
                    )}
                </Button>
            </TooltipTrigger>
            <TooltipContent>{label}</TooltipContent>
        </Tooltip>
    )
}