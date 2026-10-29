import { BrowserRouter, Navigate, Routes, Route, NavLink, useLocation, useNavigate } from 'react-router-dom'
import { FlaskConical, LayoutDashboard, LogOut, Network, Settings as SettingsIcon, Zap } from 'lucide-react'
import { PrismCatLogo } from '@/components/PrismCatLogo'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { ThemeToggle } from '@/components/ThemeToggle'
import { LanguageToggle } from '@/components/LanguageToggle'
import { PageLoading } from '@/components/PageLoading'
import { Toaster } from '@/components/ui/sonner'
import { Suspense, lazy, useState, useEffect } from 'react'
import { fetchAuthStatus, fetchConfig, login, logout as logoutRequest, onAuthExpired, setupPassword } from '@/lib/api'
import { isSettingsTab, logRequestDiffRoute, settingsTabLabelKeys, settingsTabPath, settingsTabs } from '@/lib/routes'
import { Login } from '@/pages/Login'
import { TaskPreviewProvider } from '@/components/TaskHover'

const DashboardPage = lazy(async () => {
  const module = await import('@/pages/Dashboard')
  return { default: module.Dashboard }
})

const TracesPage = lazy(async () => {
  const module = await import('@/pages/Traces')
  return { default: module.Traces }
})

const PlaygroundPage = lazy(async () => {
  const module = await import('@/pages/Playground')
  return { default: module.Playground }
})

const SettingsPage = lazy(async () => {
  const module = await import('@/pages/Settings')
  return { default: module.Settings }
})

const LogDiffPage = lazy(async () => {
  const module = await import('@/pages/LogDiff')
  return { default: module.LogDiff }
})

const TraceDetailPage = lazy(async () => {
  const module = await import('@/pages/TraceDetail')
  return { default: module.TraceDetail }
})

const ExperimentsPage = lazy(async () => { const module = await import('@/pages/Experiments'); return { default: module.Experiments } })

const listScrollPositions = new Map<string, number>()

interface AppLayoutProps {
  onSignOut: () => void
}

function AppLayout({ onSignOut }: AppLayoutProps) {
  const { t } = useTranslation()
  const location = useLocation()
  const [version, setVersion] = useState<string>('') // 接口返回前显示空占位，避免版本号过期误导

  useEffect(() => {
    fetchConfig()
      .then(cfg => {
        if (cfg.version) {
          setVersion(cfg.version.startsWith('v') ? cfg.version : `v${cfg.version}`)
        }
      })
      .catch(err => console.error('Failed to fetch version:', err))
  }, [])

  useEffect(() => {
    const key = location.pathname + location.search
    const isList = ['/', '/traces', '/experiments'].includes(location.pathname)
    const target = isList ? listScrollPositions.get(key) || 0 : 0
    let restoring = target > 0
    let lastPosition = target
    window.scrollTo(0, target)
    const restore = () => {
      if (!restoring) return
      window.scrollTo(0, target)
      if (window.scrollY >= target) restoring = false
    }
    const observer = new ResizeObserver(restore)
    if (restoring) observer.observe(document.body)
    const cancel = () => { restoring = false }
    const record = () => { if (!restoring) lastPosition = window.scrollY }
    window.addEventListener('scroll', record, { passive: true })
    window.addEventListener('wheel', cancel, { passive: true })
    window.addEventListener('touchstart', cancel, { passive: true })
    window.addEventListener('keydown', cancel)
    const timer = window.setTimeout(() => { restoring = false; observer.disconnect() }, 5000)
    return () => {
      if (isList) listScrollPositions.set(key, lastPosition)
      observer.disconnect(); window.clearTimeout(timer)
      window.removeEventListener('scroll', record)
      window.removeEventListener('wheel', cancel)
      window.removeEventListener('touchstart', cancel)
      window.removeEventListener('keydown', cancel)
    }
  }, [location.pathname, location.search])

  // 按实际使用频率排序:调用链是给 agent 场景准备的,多数人用不到,不占第二位
  const navItems = [
    { to: '/', labelKey: 'nav.dashboard', icon: LayoutDashboard },
    { to: '/playground', labelKey: 'nav.playground', icon: Zap },
    { to: '/traces', labelKey: 'nav.traces', icon: Network },
    { to: '/experiments', labelKey: 'experiments.title', icon: FlaskConical },
    { to: '/settings', labelKey: 'nav.settings', icon: SettingsIcon },
  ]

  const isNavActive = (to: string) =>
    to === '/' ? location.pathname === '/' : location.pathname.startsWith(to)

  // 设置有子分区,标题栏显示分区名而不是笼统的"设置"
  const settingsTab = location.pathname.startsWith('/settings/')
    ? location.pathname.slice('/settings/'.length).split('/')[0]
    : null
  const pageTitle = isSettingsTab(settingsTab)
    ? settingsTabLabelKeys[settingsTab]
    : navItems.find(item => isNavActive(item.to))?.labelKey ?? 'app.title'

  const routeFallback = <PageLoading />

  return (
    <div className="min-h-screen bg-background">
      {/* 侧边栏 - 桌面 */}
      <aside
        className="fixed inset-y-0 left-0 z-40 hidden w-[200px] flex-col border-r border-border bg-muted/40 md:flex"
      >
        <a
          href="https://github.com/shaomian/PrismCat"
          target="_blank"
          rel="noopener noreferrer"
          className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-3 transition-colors hover:bg-accent/60"
        >
          <PrismCatLogo className="h-5 w-5 shrink-0" />
          <span className="truncate text-sm font-medium text-foreground">{t('app.title')}</span>
        </a>

        <nav className="flex-1 space-y-0.5 overflow-y-auto p-2">
          {navItems.map((item) => {
            const Icon = item.icon
            return (
              <div key={item.to}>
                <NavLink
                  // 有子项时直接进第一个分区,保证 URL 总是带分区、子项高亮不落空
                  to={item.to === '/settings' ? settingsTabPath('routing') : item.to}
                  className={cn(
                    'flex items-center gap-2.5 rounded-md px-2.5 py-1.5 text-sm transition-colors',
                    isNavActive(item.to)
                      // 有子项时父项只加重文字、不上底色,底色留给子项,层级才分得开
                      ? item.to === '/settings'
                        ? 'font-medium text-foreground'
                        : 'bg-accent font-medium text-foreground'
                      : 'text-muted-foreground hover:bg-accent/60 hover:text-foreground'
                  )}
                >
                  <Icon className="h-4 w-4 shrink-0" />
                  <span className="truncate">{t(item.labelKey)}</span>
                </NavLink>

                {/* 设置的四个分区常驻展开:侧栏本来就空,且位置固定不会跳 */}
                {item.to === '/settings' && (
                  <div className="mt-0.5 ml-[1.4rem] space-y-0.5 border-l border-border pl-2">
                    {settingsTabs.map((tab) => (
                      <NavLink
                        key={tab}
                        to={settingsTabPath(tab)}
                        className={({ isActive }) => cn(
                          'block truncate rounded-md px-2 py-1 text-sm transition-colors',
                          isActive
                            ? 'bg-accent font-medium text-foreground'
                            : 'text-muted-foreground hover:bg-accent/60 hover:text-foreground'
                        )}
                      >
                        {t(settingsTabLabelKeys[tab])}
                      </NavLink>
                    ))}
                  </div>
                )}
              </div>
            )
          })}
        </nav>

        <div className="shrink-0 border-t border-border p-2">
          <div className="flex items-center gap-1">
            <ThemeToggle />
            <LanguageToggle className="flex-1 px-0" />
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  onClick={onSignOut}
                  className="flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                  aria-label={t('auth.sign_out')}
                >
                  <LogOut className="h-4 w-4" />
                </button>
              </TooltipTrigger>
              <TooltipContent>{t('auth.sign_out')}</TooltipContent>
            </Tooltip>
          </div>
          <p className="mt-2 select-none px-1 text-xs text-muted-foreground/70">
            PrismCat {version || '…'}
          </p>
        </div>
      </aside>

      <div className="flex min-h-screen flex-col md:pl-[200px]">
        {/* 移动端顶栏 */}
        <header className="sticky top-0 z-30 border-b border-border bg-background md:hidden">
          <div className="flex items-center justify-between gap-3 px-4 py-2.5">
            <div className="flex min-w-0 items-center gap-2">
              <PrismCatLogo className="h-5 w-5 shrink-0" />
              <span className="truncate text-sm font-medium">{t('app.title')}</span>
            </div>
            <div className="flex shrink-0 items-center gap-1">
              <ThemeToggle />
              <LanguageToggle />
              {/* 移动端顶栏不挂 Tooltip:触屏不触发悬停,aria-label 已覆盖读屏器 */}
              <button
                onClick={onSignOut}
                className="flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                aria-label={t('auth.sign_out')}
              >
                <LogOut className="h-4 w-4" />
              </button>
            </div>
          </div>
          <nav className="flex items-center gap-1 overflow-x-auto px-2 pb-2">
            {navItems.map((item) => {
              const Icon = item.icon
              return (
                <NavLink
                  key={item.to}
                  to={item.to === '/settings' ? settingsTabPath('routing') : item.to}
                  className={cn(
                    'flex flex-1 items-center justify-center gap-1.5 whitespace-nowrap rounded-md px-3 py-1.5 text-xs transition-colors',
                    isNavActive(item.to)
                      ? 'bg-accent font-medium text-foreground'
                      : 'text-muted-foreground hover:bg-accent/60 hover:text-foreground'
                  )}
                >
                  <Icon className="h-4 w-4 shrink-0" />
                  <span>{t(item.labelKey)}</span>
                </NavLink>
              )
            })}
          </nav>
          {location.pathname.startsWith('/settings') && (
            <nav className="flex items-center gap-1 overflow-x-auto border-t border-border px-2 py-2">
              {settingsTabs.map(tab => (
                <NavLink
                  key={tab}
                  to={settingsTabPath(tab)}
                  className={({ isActive }) => cn(
                    'shrink-0 rounded-md px-3 py-1.5 text-xs transition-colors',
                    isActive
                      ? 'bg-accent font-medium text-foreground'
                      : 'text-muted-foreground hover:bg-accent/60 hover:text-foreground'
                  )}
                >
                  {t(settingsTabLabelKeys[tab])}
                </NavLink>
              ))}
            </nav>
          )}
        </header>

        {/* 页面标题栏 - 给内容区一个"顶" */}
        <div className="sticky top-0 z-20 hidden h-12 shrink-0 items-center border-b border-border bg-background px-5 md:flex">
          <h1 className="text-sm font-medium text-foreground">{t(pageTitle)}</h1>
        </div>

        {/* 主内容 */}
        <main className="w-full flex-1 px-4 py-4 sm:px-5">
          {/* key 按 pathname 重放淡入,查询串变化(翻页/筛选)不重复动画 */}
          <div key={location.pathname} className="mx-auto w-full max-w-[1600px] animate-fade-in">
            <Suspense fallback={routeFallback}>
              <Routes>
                <Route path="/" element={<DashboardPage />} />
                <Route path={logRequestDiffRoute} element={<LogDiffPage />} />
                <Route path="/traces" element={<TracesPage />} />
                <Route path="/traces/:traceId" element={<TraceDetailPage />} />
                <Route path="/experiments" element={<ExperimentsPage />} />
                <Route path="/experiments/:experimentId" element={<ExperimentsPage />} />
                <Route path="/playground" element={<PlaygroundPage />} />
                <Route path="/logging-rules" element={<Navigate to="/settings/logging/models" replace />} />
                <Route path="/logging-rules/models" element={<Navigate to="/settings/logging/models" replace />} />
                <Route path="/logging-rules/ignored" element={<Navigate to="/settings/logging/ignored" replace />} />
                <Route path="/logging-rules/*" element={<Navigate to="/settings/logging/models" replace />} />
                <Route path="/settings" element={<SettingsPage />} />
                <Route path="/settings/:tab" element={<SettingsPage />} />
                <Route path="/settings/:tab/:subtab" element={<SettingsPage />} />
              </Routes>
            </Suspense>
          </div>
        </main>
      </div>
    </div>
  )
}

function AppContent() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [isCheckingAuth, setIsCheckingAuth] = useState(true)
  const [isAuthenticated, setIsAuthenticated] = useState(false)
  const [setupRequired, setSetupRequired] = useState(false)

  useEffect(() => {
    let cancelled = false
    fetchAuthStatus()
      .then((status) => {
        if (cancelled) return
        setIsAuthenticated(status.authenticated)
        setSetupRequired(status.setup_required)
      })
      .catch((err) => {
        console.error('Failed to fetch auth status:', err)
        if (!cancelled) {
          setIsAuthenticated(false)
        }
      })
      .finally(() => {
        if (!cancelled) {
          setIsCheckingAuth(false)
        }
      })

    return () => {
      cancelled = true
    }
  }, [])

  // 数据接口收到 401（会话过期）时统一回到登录界面，
  // 而不是各页面各自弹一堆"加载失败"而无从得知需要重新登录
  useEffect(() => {
    onAuthExpired(() => {
      setIsAuthenticated(false)
    })
    return () => {
      onAuthExpired(null)
    }
  }, [])

  const handleSignedIn = async (password: string) => {
    const status = setupRequired
      ? await setupPassword(password)
      : await login(password)
    if (!status.authenticated) {
      throw new Error(t('auth.sign_in_failed'))
    }
    setIsAuthenticated(true)
    setSetupRequired(status.setup_required)
    navigate('/', { replace: true })
  }

  const handleSignOut = async () => {
    await logoutRequest().catch(err => console.error('Failed to sign out:', err))
    setIsAuthenticated(false)
    navigate('/', { replace: true })
  }

  if (isCheckingAuth) {
    return <PageLoading className="min-h-screen" />
  }

  if (!isAuthenticated) {
    return <Login setupRequired={setupRequired} onSignedIn={handleSignedIn} />
  }

  return <AppLayout onSignOut={handleSignOut} />
}

function App() {
  return (
    <BrowserRouter>
      <TooltipProvider>
        <TaskPreviewProvider><AppContent /></TaskPreviewProvider>
        <Toaster position="top-right" expand={true} richColors />
      </TooltipProvider>
    </BrowserRouter>
  )
}

export default App
