import { createContext, useContext } from 'react'
import type { LogDetailSection } from './helpers'

// UI 辅助上下文：CopyButton/SectionHeader 等小组件提升到模块作用域后
//（避免流式刷新时每次 render 都产生新组件类型导致整棵子树重挂载），
// 通过 context 拿到与本次渲染相关的回调/状态。
export interface LogDetailUIHelpers {
    copiedField: string | null
    copyToClipboard: (text: string, field: string) => void
    expandedSections: Record<LogDetailSection, boolean>
    toggleSection: (section: LogDetailSection) => void
}

export const LogDetailUIContext = createContext<LogDetailUIHelpers | null>(null)

export function useLogDetailUI(): LogDetailUIHelpers {
    const helpers = useContext(LogDetailUIContext)
    if (!helpers) throw new Error('LogDetailUIContext missing')
    return helpers
}
