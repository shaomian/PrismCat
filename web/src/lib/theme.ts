// 全局主题状态：单一数据源，供 ThemeToggle、Toaster、Mermaid 等共享，
// 避免（曾经出现的）多个 Toggle 实例各持 useState 导致的显示不同步。
export type Theme = 'dark' | 'light'

let current: Theme = 'dark'
let initialized = false
const listeners = new Set<() => void>()

function readStoredTheme(): Theme {
    const stored = localStorage.getItem('theme')
    if (stored === 'dark' || stored === 'light') return stored
    // 首次访问跟随系统偏好
    if (window.matchMedia?.('(prefers-color-scheme: light)').matches) return 'light'
    return 'dark'
}

function applyThemeClass(theme: Theme) {
    document.documentElement.classList.toggle('dark', theme === 'dark')
}

// initTheme 在应用启动时调用一次：读取存储/系统偏好并应用到 DOM，
// 消除首帧闪错主题的问题
export function initTheme() {
    if (initialized) return
    initialized = true
    current = readStoredTheme()
    applyThemeClass(current)
}

export function getTheme(): Theme {
    return current
}

export function setTheme(theme: Theme) {
    current = theme
    localStorage.setItem('theme', theme)
    applyThemeClass(theme)
    listeners.forEach((cb) => cb())
}

export function toggleTheme() {
    setTheme(current === 'dark' ? 'light' : 'dark')
}

export function subscribeTheme(listener: () => void) {
    listeners.add(listener)
    return () => {
        listeners.delete(listener)
    }
}