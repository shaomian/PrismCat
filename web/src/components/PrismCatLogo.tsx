import { useId } from 'react'
import { cn } from '@/lib/utils'

interface PrismCatLogoProps {
    className?: string
}

export function PrismCatLogo({ className }: PrismCatLogoProps) {
    // 同页会同时渲染多个 logo（侧边栏 + 移动端头部 + 登录页），
    // 硬编码的 id 会重复（无效 HTML，且 url(#prismGradient) 全部解析到
    // 第一个实例的 defs）。useId 保证每个实例引用自己的渐变。
    const gradientId = `prismGradient-${useId()}`
    const glowId = `glow-${useId()}`
    return (
        <svg
            viewBox="0 0 100 100"
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
            className={cn("w-10 h-10", className)}
        >
            <defs>
                {/* 渐变与 primary 同源(紫→青的光谱切片),不要另起色号。
                    用逗号 hsl 语法保证 SVG presentation attribute 兼容 */}
                <linearGradient id={gradientId} x1="0%" y1="0%" x2="100%" y2="100%">
                    <stop offset="0%" stopColor="hsl(263, 70%, 50%)" />
                    <stop offset="100%" stopColor="hsl(185, 90%, 45%)" />
                </linearGradient>

                <filter id={glowId} x="-20%" y="-20%" width="140%" height="140%">
                    <feGaussianBlur stdDeviation="2.5" result="blur" />
                    <feComposite in="SourceGraphic" in2="blur" operator="over" />
                </filter>
            </defs>

            {/* 外部发光阴影层 */}
            <path
                d="M32 18 L44 32 H56 L68 18 L82 40 V68 L50 88 L18 68 V40 L32 18 Z"
                fill={`url(#${gradientId})`}
                fillOpacity="0.2"
                style={{ filter: 'blur(5px)' }}
            />

            {/* 主体形状 */}
            <path
                d="M32 18 L44 32 H56 L68 18 L82 40 V68 L50 88 L18 68 V40 L32 18 Z"
                fill={`url(#${gradientId})`}
                fillOpacity="0.1"
                stroke={`url(#${gradientId})`}
                strokeWidth="3"
                strokeLinejoin="round"
            />

            {/* 内部棱镜切割线 */}
            <path
                d="M50 32 V88 M18 40 L50 60 L82 40"
                stroke="currentColor"
                strokeWidth="2"
                strokeOpacity="0.4"
                strokeLinecap="round"
                strokeLinejoin="round"
                className="text-white dark:text-white"
            />

            {/* 核心光点 */}
            <circle cx="50" cy="60" r="1.5" fill="white" fillOpacity="0.8" />
        </svg>
    )
}