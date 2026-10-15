import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Sparkles, Send, Loader2, Settings2, AlertCircle, Wrench, FileSearch } from 'lucide-react'
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

  // 对话状态
  const [messages, setMessages] = useState<DisplayMessage[]>([])
  const [input, setInput] = useState('')
  const [streaming, setStreaming] = useState(false)
  const [error, setError] = useState('')
  const scrollRef = useRef<HTMLDivElement>(null)

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
      setError('')
    }
  }, [open, loadConfig])

  // 切换 trace 时重置对话
  useEffect(() => {
    setMessages([])
    setInput('')
    setError('')
  }, [traceId])

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight })
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

  const handleSend = useCallback(async (question: string) => {
    const q = question.trim()
    if (!q || streaming) return
    setError('')
    const history: AnalysisMessage[] = [
      ...messages.map(m => ({ role: m.role, content: m.content })),
      { role: 'user', content: q },
    ]
    setMessages(prev => [...prev,
      { role: 'user', content: q },
      { role: 'assistant', content: '', activity: [] },
    ])
    setInput('')
    setStreaming(true)
    try {
      let acc = ''
      const updateLast = (fn: (m: DisplayMessage) => DisplayMessage) => {
        setMessages(prev => {
          const next = [...prev]
          if (next.length > 0 && next[next.length - 1].role === 'assistant') {
            next[next.length - 1] = fn(next[next.length - 1])
          }
          return next
        })
      }
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
        })
      if (result.error) setError(result.error)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setStreaming(false)
    }
  }, [messages, streaming, traceId])

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
      {/* 配置摘要 */}
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground min-w-0">
        <Sparkles className="h-3.5 w-3.5 text-primary shrink-0" />
        <span className="font-medium shrink-0">{config.model}</span>
        <span className="opacity-50 shrink-0">·</span>
        <span className="font-mono truncate">{config.base_url}</span>
      </div>

      {/* 消息列表 */}
      <div ref={scrollRef} className="flex-1 min-h-0 overflow-y-auto space-y-3 pr-1">
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
        <Button size="sm" className="gap-1 shrink-0" onClick={() => handleSend(input)} disabled={streaming || !input.trim()}>
          {streaming ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Send className="h-3.5 w-3.5" />}
          {t('analysis.send')}
        </Button>
      </div>
    </div>
  )
}