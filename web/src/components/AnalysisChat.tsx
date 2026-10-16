import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from 'react'
import { useTranslation } from 'react-i18next'
import { Sparkles, Send, Loader2, Settings2, AlertCircle, Wrench, FileSearch, Trash2, Square } from 'lucide-react'
import {
  fetchConfig, updateConfig, streamAnalysisChat,
  type AnalysisMessage, type AppConfig,
} from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import Markdown from 'react-markdown'
import { toast } from 'sonner'

// 展示用的消息（在 API 消息基础上附加工具活动记录，不上传回服务端）
interface DisplayMessage {
  role: 'user' | 'assistant'
  content: string
  activity?: { kind: 'tool_call' | 'tool_result'; name: string; args?: string; chars?: number }[]
}

const PRESET_QUESTIONS = [
  'analysis.preset_summary',
  'analysis.preset_errors',
  'analysis.preset_tokens',
] as const

// ---------------------------------------------------------------------------
// 模块级会话存储：对话状态与流式输出不依赖组件生命周期。
// 关闭分析面板（组件卸载）后流式更新与持久化仍在后台继续；
// 重新打开（甚至离开 trace 页面再回来）都能看到进行中/已完成的回答。
// ---------------------------------------------------------------------------

// 对话历史按 trace 保存在浏览器 localStorage：误触关闭面板、刷新页面后可恢复
const chatStorageKey = (traceId: string) => `prismcat-analysis-chat:${traceId}`
const maxSavedMessages = 200

function loadSavedMessages(traceId: string): DisplayMessage[] {
  if (!traceId) return []
  try {
    const raw = window.localStorage.getItem(chatStorageKey(traceId))
    if (!raw) return []
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed.filter((m): m is DisplayMessage =>
      !!m && typeof m === 'object'
      && ((m as DisplayMessage).role === 'user' || (m as DisplayMessage).role === 'assistant')
      && typeof (m as DisplayMessage).content === 'string'
      // 跳过空占位（无内容且无工具活动的半截消息）
      && ((m as DisplayMessage).content !== '' || ((m as DisplayMessage).activity ?? []).length > 0),
    )
  } catch {
    return []
  }
}

interface ChatSession {
  messages: DisplayMessage[]
  streaming: boolean
  error: string
}

const sessions = new Map<string, ChatSession>()
const sessionListeners = new Map<string, Set<() => void>>()
// 每个 trace 的进行中请求；用于「停止」按钮中止后台流
const abortControllers = new Map<string, AbortController>()

// localStorage 持久化防抖：流式输出期间每个 token 都会 mutate 会话，
// 逐次同步写 localStorage 会卡主线程（长回答可达数千次写/回答）
const PERSIST_DEBOUNCE_MS = 1000
const persistTimers = new Map<string, number>()

function persistSession(traceId: string) {
  try {
    const save = getSession(traceId).messages
      .filter(m => m.content !== '' || (m.activity ?? []).length > 0)
      .slice(-maxSavedMessages)
    if (save.length > 0) {
      window.localStorage.setItem(chatStorageKey(traceId), JSON.stringify(save))
    } else {
      window.localStorage.removeItem(chatStorageKey(traceId))
    }
  } catch {
    // 存储配额溢出等异常时忽略，不影响当前对话
  }
}

function schedulePersist(traceId: string, immediate = false) {
  const existing = persistTimers.get(traceId)
  if (immediate) {
    if (existing !== undefined) {
      clearTimeout(existing)
      persistTimers.delete(traceId)
    }
    persistSession(traceId)
    return
  }
  if (existing !== undefined) return
  persistTimers.set(traceId, window.setTimeout(() => {
    persistTimers.delete(traceId)
    persistSession(traceId)
  }, PERSIST_DEBOUNCE_MS))
}

function flushPersistAll() {
  for (const traceId of persistTimers.keys()) {
    schedulePersist(traceId, true)
  }
}

// 页面关闭/隐藏时立即落盘，防抖窗口内的更新不丢失
window.addEventListener('pagehide', flushPersistAll)

function emitSession(traceId: string) {
  sessionListeners.get(traceId)?.forEach(fn => fn())
}

function subscribeSession(traceId: string, cb: () => void) {
  let set = sessionListeners.get(traceId)
  if (!set) {
    set = new Set()
    sessionListeners.set(traceId, set)
  }
  set.add(cb)
  return () => { set.delete(cb) }
}

function getSession(traceId: string): ChatSession {
  let s = sessions.get(traceId)
  if (!s) {
    s = { messages: loadSavedMessages(traceId), streaming: false, error: '' }
    sessions.set(traceId, s)
  }
  return s
}

function updateSession(traceId: string, mutate: (s: ChatSession) => ChatSession) {
  sessions.set(traceId, mutate(getSession(traceId)))
  schedulePersist(traceId)
  emitSession(traceId)
}

// 发起一轮提问；运行在模块作用域，面板关闭后依旧继续执行到底
async function runSend(traceId: string, question: string) {
  if (!traceId || getSession(traceId).streaming) return
  // 历史里跳过空内容占位（上一轮流被中断/出错时残留），否则会把空消息发给服务端
  const history: AnalysisMessage[] = [
    ...getSession(traceId).messages
      .filter(m => m.content !== '')
      .map(m => ({ role: m.role, content: m.content })),
    { role: 'user', content: question },
  ]
  updateSession(traceId, s => ({
    ...s,
    error: '',
    streaming: true,
    messages: [...s.messages,
      { role: 'user', content: question },
      { role: 'assistant', content: '', activity: [] },
    ],
  }))

  const controller = new AbortController()
  abortControllers.set(traceId, controller)

  let acc = ''
  const updateLast = (fn: (m: DisplayMessage) => DisplayMessage) => {
    updateSession(traceId, s => {
      const messages = [...s.messages]
      if (messages.length > 0 && messages[messages.length - 1].role === 'assistant') {
        messages[messages.length - 1] = fn(messages[messages.length - 1])
      }
      return { ...s, messages }
    })
  }

  try {
    const result = await streamAnalysisChat(traceId, history,
      delta => {
        acc += delta
        updateLast(m => ({ ...m, content: acc }))
      },
      ev => {
        if (ev.kind === 'tool_call') {
          updateLast(m => ({ ...m, activity: [...(m.activity ?? []), { kind: 'tool_call', name: ev.name, args: ev.args }] }))
        } else if (ev.kind === 'tool_result') {
          updateLast(m => ({ ...m, activity: [...(m.activity ?? []), { kind: 'tool_result', name: ev.name, chars: ev.chars }] }))
        } else if (ev.kind === 'reset') {
          acc = ''
          updateLast(m => ({ ...m, content: '' }))
        }
      },
      controller.signal)
    const err = result.error
    if (err) updateSession(traceId, s => ({ ...s, error: err }))
  } catch (e) {
    // 用户主动停止不算错误，保留已收到的部分内容
    if ((e as Error).name !== 'AbortError') {
      updateSession(traceId, s => ({ ...s, error: (e as Error).message }))
    }
  } finally {
    abortControllers.delete(traceId)
    updateSession(traceId, s => ({ ...s, streaming: false }))
    // 流结束后立即落盘，不等防抖窗口
    schedulePersist(traceId, true)
  }
}

// 中止进行中的分析流（「停止」按钮）
function stopStream(traceId: string) {
  abortControllers.get(traceId)?.abort()
}

function clearSession(traceId: string) {
  try { window.localStorage.removeItem(chatStorageKey(traceId)) } catch { /* ignore */ }
  sessions.delete(traceId)
  emitSession(traceId)
}

// ---------------------------------------------------------------------------

function isConfigured(cfg: AppConfig['analysis'] | null): cfg is AppConfig['analysis'] {
  return !!cfg && cfg.enabled && !!cfg.base_url && !!cfg.model
}

export function AnalysisChat({ traceId, open }: { traceId: string; open: boolean }) {
  const { t } = useTranslation()
  const [config, setConfig] = useState<AppConfig['analysis'] | null>(null)
  const [configLoading, setConfigLoading] = useState(false)

  // 配置表单
  const [baseUrl, setBaseUrl] = useState('')
  const [apiKey, setApiKey] = useState('')
  const [model, setModel] = useState('')
  const [saving, setSaving] = useState(false)

  const subscribe = useCallback((cb: () => void) => subscribeSession(traceId, cb), [traceId])
  const getSnapshot = useCallback(() => getSession(traceId), [traceId])
  const session = useSyncExternalStore(subscribe, getSnapshot)
  const { messages, streaming, error } = session

  const [input, setInput] = useState('')
  const scrollRef = useRef<HTMLDivElement>(null)
  // 用户向上翻阅时不再强制滚回底部，避免「跟读者抢滚动条」
  const stickToBottomRef = useRef(true)

  const loadConfig = useCallback(async () => {
    setConfigLoading(true)
    try {
      const cfg = await fetchConfig()
      setConfig(cfg.analysis ?? null)
      if (cfg.analysis) {
        setBaseUrl(cfg.analysis.base_url ?? '')
        setModel(cfg.analysis.model ?? '')
      }
    } catch {
      setConfig(null)
    } finally {
      setConfigLoading(false)
    }
  }, [])

  useEffect(() => {
    if (open) {
      loadConfig()
    }
  }, [open, loadConfig])

  useEffect(() => {
    setInput('')
  }, [traceId])

  useEffect(() => {
    if (stickToBottomRef.current) {
      scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight })
    }
  }, [messages])

  const saveConfig = async () => {
    setSaving(true)
    try {
      await updateConfig({
        analysis: {
          enabled: true,
          base_url: baseUrl.trim(),
          model: model.trim(),
          // 留空表示保持已保存的 key 不变（后端语义）
          api_key: apiKey.trim() || undefined,
        },
      })
      toast.success(t('analysis.config_saved'))
      await loadConfig()
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setSaving(false)
    }
  }

  const handleSend = useCallback((question: string) => {
    const q = question.trim()
    if (!q) return
    void runSend(traceId, q)
  }, [traceId])

  const handleStop = useCallback(() => {
    stopStream(traceId)
  }, [traceId])

  // 清空当前 trace 的对话历史
  const handleClear = useCallback(() => {
    clearSession(traceId)
    toast.success(t('analysis.cleared'))
  }, [traceId, t])

  if (configLoading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    )
  }

  // ---------- 未配置：内嵌配置表单 ----------
  const hasApiKey = !!config?.has_api_key
  if (!isConfigured(config)) {
    return (
      <div className="space-y-4 px-1">
        <div className="flex items-center gap-2 text-sm font-medium">
          <Settings2 className="h-4 w-4 text-primary" />
          {t('analysis.setup_title')}
        </div>
        <p className="text-xs text-muted-foreground leading-relaxed">{t('analysis.setup_hint')}</p>
        <div className="space-y-3">
          <div className="space-y-1.5">
            <Label htmlFor="analysis-base-url" className="text-xs">{t('analysis.base_url')}</Label>
            <Input
              id="analysis-base-url"
              value={baseUrl}
              onChange={e => setBaseUrl(e.target.value)}
              placeholder="http://localhost:8711/_proxy/openai 或 https://api.openai.com/v1"
              className="text-xs font-mono"
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="analysis-api-key" className="text-xs">{t('analysis.api_key')}</Label>
            <Input
              id="analysis-api-key"
              type="password"
              value={apiKey}
              onChange={e => setApiKey(e.target.value)}
              placeholder={hasApiKey ? t('analysis.api_key_keep') : 'sk-...'}
              className="text-xs font-mono"
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="analysis-model" className="text-xs">{t('analysis.model')}</Label>
            <Input
              id="analysis-model"
              value={model}
              onChange={e => setModel(e.target.value)}
              placeholder="gpt-4o / claude-sonnet-4-5 / ..."
              className="text-xs font-mono"
            />
          </div>
          <Button size="sm" className="w-full gap-1.5" onClick={saveConfig} disabled={saving || !baseUrl.trim() || !model.trim()}>
            {saving && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
            {t('analysis.save_and_start')}
          </Button>
        </div>
      </div>
    )
  }

  // ---------- 已配置：对话面板 ----------
  const empty = messages.length === 0
  return (
    <div className="flex flex-col h-full min-h-0 gap-3">
      {/* 配置摘要 + 清空历史 */}
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground min-w-0">
        <Sparkles className="h-3.5 w-3.5 text-primary shrink-0" />
        <span className="font-medium shrink-0">{config.model}</span>
        <span className="opacity-50 shrink-0">·</span>
        <span className="font-mono truncate">{config.base_url}</span>
        {messages.length > 0 && (
          <button
            type="button"
            onClick={handleClear}
            disabled={streaming}
            title={t('analysis.clear')}
            aria-label={t('analysis.clear')}
            className="ml-auto shrink-0 inline-flex items-center justify-center h-6 w-6 rounded-md text-muted-foreground hover:bg-muted hover:text-danger transition-colors disabled:opacity-50 disabled:pointer-events-none"
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        )}
      </div>

      {/* 消息列表 */}
      <div
        ref={scrollRef}
        className="flex-1 min-h-0 overflow-y-auto space-y-3 pr-1"
        onScroll={() => {
          const el = scrollRef.current
          if (!el) return
          stickToBottomRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40
        }}
      >
        {empty && (
          <div className="space-y-2 pt-4">
            <p className="text-xs text-muted-foreground text-center">{t('analysis.empty_hint')}</p>
            <div className="flex flex-col gap-1.5">
              {PRESET_QUESTIONS.map(key => (
                <button
                  key={key}
                  type="button"
                  onClick={() => handleSend(t(key))}
                  className="text-left text-xs rounded-md border border-border bg-card hover:bg-muted/50 px-3 py-2 transition-colors"
                >
                  {t(key)}
                </button>
              ))}
            </div>
          </div>
        )}

        {messages.map((msg, i) => (
          <div key={i} className={msg.role === 'user' ? 'flex justify-end' : 'flex justify-start'}>
            {msg.role === 'user' ? (
              <div className="max-w-[85%] rounded-lg bg-primary/10 px-3 py-2 text-xs leading-relaxed whitespace-pre-wrap">
                {msg.content}
              </div>
            ) : (
              <div className="max-w-full w-full rounded-lg border border-border bg-card px-3 py-2 text-xs leading-relaxed space-y-2 [&_p]:mb-1.5 [&_p:last-child]:mb-0 [&_ul]:list-disc [&_ul]:pl-4 [&_ol]:list-decimal [&_ol]:pl-4 [&_li]:mb-0.5 [&_code]:font-mono [&_code]:text-[11px] [&_pre]:overflow-x-auto [&_h1]:text-sm [&_h2]:text-sm [&_h2]:font-semibold [&_h3]:text-xs [&_h3]:font-semibold [&_strong]:font-semibold">
                {/* 工具调用活动条（drill-down） */}
                {(msg.activity ?? []).length > 0 && (
                  <div className="flex flex-wrap gap-1.5">
                    {msg.activity!.map((act, j) => act.kind === 'tool_call' ? (
                      <span key={j} className="inline-flex items-center gap-1 h-5 px-1.5 rounded bg-warning/10 text-warning text-[11px] font-medium">
                        <Wrench className="h-3 w-3" />
                        {t('analysis.tool_fetching')}
                      </span>
                    ) : (
                      <span key={j} className="inline-flex items-center gap-1 h-5 px-1.5 rounded bg-success/10 text-success text-[11px] font-medium">
                        <FileSearch className="h-3 w-3" />
                        {t('analysis.tool_fetched', { chars: act.chars ?? 0 })}
                      </span>
                    ))}
                  </div>
                )}
                {msg.content
                  ? <Markdown>{msg.content}</Markdown>
                  : streaming && i === messages.length - 1 && !(msg.activity ?? []).length
                    ? <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
                    : null}
              </div>
            )}
          </div>
        ))}

        {error && (
          <div className="flex items-start gap-1.5 rounded-md border border-danger/30 bg-danger/5 px-2.5 py-2 text-xs text-danger">
            <AlertCircle className="h-3.5 w-3.5 shrink-0 mt-0.5" />
            <span className="break-all">{error}</span>
          </div>
        )}
      </div>

      {/* 输入区 */}
      <div className="flex items-center gap-2">
        <Input
          value={input}
          onChange={e => setInput(e.target.value)}
          onKeyDown={e => {
            if (e.key === 'Enter' && !e.nativeEvent.isComposing && !e.shiftKey) {
              e.preventDefault()
              handleSend(input)
            }
          }}
          placeholder={t('analysis.input_placeholder')}
          className="text-xs"
          disabled={streaming}
        />
        {streaming ? (
          <Button
            size="sm"
            variant="outline"
            className="gap-1 shrink-0"
            onClick={handleStop}
            title={t('analysis.stop')}
          >
            <Square className="h-3 w-3" />
            {t('analysis.stop')}
          </Button>
        ) : (
          <Button size="sm" className="gap-1 shrink-0" onClick={() => handleSend(input)} disabled={!input.trim()}>
            <Send className="h-3.5 w-3.5" />
            {t('analysis.send')}
          </Button>
        )}
      </div>
    </div>
  )
}