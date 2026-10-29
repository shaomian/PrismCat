import { useEffect, useRef, useState, useSyncExternalStore } from 'react'
import { useTranslation } from 'react-i18next'
import mermaid from 'mermaid'
import { Copy, Check, Download, FileText } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from '@/components/ui/dialog'
import { toast } from 'sonner'
import type { DiagramLabel } from '@/lib/api'
import { getTheme, subscribeTheme, type Theme } from '@/lib/theme'

let mermaidTheme = ''
let renderSeq = 0

// 原生 <title> 悬停提示的长度上限（完整内容点击弹窗查看）
const TOOLTIP_CHARS = 300

// 渲染结果缓存：流程图 tab 每次切入都会重挂载组件并从头执行 mermaid
// 渲染（大图可能数秒）；缓存 key 含主题（SVG 的配色在渲染时固化）。
// LRU 上限 8 条，超出后淘汰最旧。
const SVG_CACHE_LIMIT = 8
const svgCache = new Map<string, string>()
const pendingRenders = new Map<string, Promise<string>>()

function getCachedSvg(key: string): string | undefined {
  const hit = svgCache.get(key)
  if (hit !== undefined) {
    svgCache.delete(key)
    svgCache.set(key, hit)
  }
  return hit
}

function putCachedSvg(key: string, svg: string) {
  if (svgCache.has(key)) svgCache.delete(key)
  svgCache.set(key, svg)
  while (svgCache.size > SVG_CACHE_LIMIT) {
    const oldest = svgCache.keys().next().value
    if (oldest === undefined) break
    svgCache.delete(oldest)
  }
}

// 主题变化时重新初始化并触发重渲染，否则首次渲染后主题被冻结，
// 暗色切换后流程图仍是旧主题（浅色文字在暗色卡片上不可读）
function ensureMermaid(theme: Theme) {
  if (mermaidTheme === theme) return
  mermaid.initialize({
    startOnLoad: false,
    theme: theme === 'dark' ? 'dark' : 'default',
    sequence: { actorMargin: 60, messageMargin: 36, useMaxWidth: true },
  })
  mermaidTheme = theme
}

// 与后端 esc() 保持一致：Mermaid 源码中 ";" 与 "#" 被替换为全角标点
const escapeMermaid = (s: string) => s.replace(/;/g, '；').replace(/#/g, '＃')
const normalize = (s: string) => s.replace(/\s+/g, ' ').trim()

// bindTruncatedLabels 给被截断的消息标签绑定交互：
// 悬停显示原生 <title> 预览，点击弹出完整内容。
// Mermaid 渲染出的每条消息/注释文本（text.messageText / text.noteText）
// 就是源码中 esc(截断文本)，用「去掉尾部省略号后是全文前缀」来匹配。
function bindTruncatedLabels(
  container: HTMLElement,
  labels: DiagramLabel[],
  onSelect: (label: DiagramLabel) => void,
) {
  const els = Array.from(container.querySelectorAll<SVGElement>('text.messageText, text.noteText'))
  const bound = new Set<SVGElement>()
  for (const label of labels) {
    if (!label.truncated) continue
    const full = escapeMermaid(normalize(label.text))
    const el = els.find((e) => {
      if (bound.has(e)) return false
      const raw = normalize(e.textContent ?? '')
      // 只考虑带省略号的元素，避免误绑到恰好是全文前缀的短标签
      if (!raw.endsWith('…')) return false
      const displayed = raw.slice(0, -1)
      return displayed.length > 0 && full.startsWith(displayed)
    })
    if (!el) continue
    bound.add(el)
    el.classList.add('diagram-label-truncated')
    const title = document.createElementNS('http://www.w3.org/2000/svg', 'title')
    title.textContent = label.text.length > TOOLTIP_CHARS
      ? label.text.slice(0, TOOLTIP_CHARS) + '…'
      : label.text
    el.appendChild(title)
    el.addEventListener('click', () => onSelect(label))
  }
}

export function TraceDiagram({
  code,
  labels,
  onOpenLog,
}: {
  code: string
  labels?: DiagramLabel[] | null
  onOpenLog?: (logId: string) => void
}) {
  const { t } = useTranslation()
  const theme = useSyncExternalStore(subscribeTheme, getTheme)
  const containerRef = useRef<HTMLDivElement>(null)
  const [error, setError] = useState(false)
  const [rendering, setRendering] = useState(true)
  const [copied, setCopied] = useState(false)
  const [detail, setDetail] = useState<DiagramLabel | null>(null)
  const [detailCopied, setDetailCopied] = useState(false)

  useEffect(() => {
    if (!code) return
    ensureMermaid(theme)
    let cancelled = false
    setError(false)
    setRendering(true)

    const applySvg = (svg: string) => {
      if (cancelled || !containerRef.current) return
      containerRef.current.innerHTML = svg
      if (labels?.length) {
        bindTruncatedLabels(containerRef.current, labels, setDetail)
      }
      setRendering(false)
    }

    // 命中缓存时同步应用，tab 切换即时出图
    const cacheKey = `${theme}\n${code}`
    const cached = getCachedSvg(cacheKey)
    if (cached !== undefined) {
      applySvg(cached)
      return () => {
        cancelled = true
      }
    }

    // 快速切换页签或 StrictMode 重挂载时，复用尚未完成的渲染任务。
    // 即使原组件离开也保存结果，下次打开无需再次进入 Mermaid 的渲染队列。
    let pending = pendingRenders.get(cacheKey)
    if (!pending) {
      pending = mermaid.render(`trace-diagram-${++renderSeq}`, code).then(({ svg }) => {
        putCachedSvg(cacheKey, svg)
        return svg
      }).finally(() => pendingRenders.delete(cacheKey))
      pendingRenders.set(cacheKey, pending)
    }
    pending
      .then(svg => {
        if (cancelled) return
        applySvg(svg)
      })
      .catch(() => {
        if (!cancelled) {
          setError(true)
          setRendering(false)
        }
      })
    return () => {
      cancelled = true
    }
  }, [code, labels, theme])

  const copySource = async () => {
    try {
      await navigator.clipboard.writeText(code)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      toast.error(t('timeline.diagram.copy_failed'))
    }
  }

  const copyDetail = async () => {
    if (!detail) return
    try {
      await navigator.clipboard.writeText(detail.text)
      setDetailCopied(true)
      setTimeout(() => setDetailCopied(false), 1500)
    } catch {
      toast.error(t('timeline.diagram.copy_failed'))
    }
  }

  const downloadSvg = () => {
    const svg = containerRef.current?.querySelector('svg')
    if (!svg) return
    const blob = new Blob([svg.outerHTML], { type: 'image/svg+xml' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'trace-diagram.svg'
    a.click()
    URL.revokeObjectURL(url)
  }

  const hasTruncated = !!labels?.some((l) => l.truncated)

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2 flex-wrap">
        <Button variant="outline" size="sm" className="gap-1.5 h-7 text-xs" onClick={copySource}>
          {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
          {t('timeline.diagram.copy_source')}
        </Button>
        <Button variant="outline" size="sm" className="gap-1.5 h-7 text-xs" onClick={downloadSvg} disabled={error || rendering}>
          <Download className="h-3 w-3" />
          SVG
        </Button>
        {hasTruncated && (
          <span className="text-[11px] text-muted-foreground">
            {t('timeline.diagram.truncated_hint')}
          </span>
        )}
      </div>
      {rendering && <div role="status" className="flex items-center justify-center gap-2 py-6 text-sm text-muted-foreground">
        <Spinner />{t('common.loading')}
      </div>}
      {error ? (
        <div className="rounded-md border border-warning/30 bg-warning/5 p-3">
          <p className="text-xs text-warning mb-2">{t('timeline.diagram.render_failed')}</p>
          <pre className="text-xs font-mono whitespace-pre-wrap text-foreground/70">{code}</pre>
        </div>
      ) : null}
      <div
        hidden={error}
        ref={containerRef}
        className="overflow-x-auto rounded-md border border-border bg-card p-4 [&_svg]:max-w-none
          [&_.diagram-label-truncated]:cursor-pointer
          [&_.diagram-label-truncated:hover]:fill-info"
      />

      <Dialog open={!!detail} onOpenChange={(open) => !open && setDetail(null)}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle className="text-sm">{t('timeline.diagram.full_text')}</DialogTitle>
            <DialogDescription className="text-xs">
              {t('timeline.diagram.full_text_desc', { count: detail?.text.length ?? 0 })}
            </DialogDescription>
          </DialogHeader>
          <pre className="max-h-[60vh] overflow-y-auto whitespace-pre-wrap break-words rounded-md border border-border bg-muted/40 p-3 font-mono text-xs leading-relaxed text-foreground/85">
            {detail?.text}
          </pre>
          <DialogFooter>
            {detail?.log_id && onOpenLog && (
              <Button
                variant="outline"
                size="sm"
                className="gap-1.5"
                onClick={() => {
                  const logId = detail?.log_id
                  if (!logId || !onOpenLog) return
                  setDetail(null)
                  onOpenLog(logId)
                }}
              >
                <FileText className="h-3.5 w-3.5" />
                {t('timeline.open_log')}
              </Button>
            )}
            <Button variant="outline" size="sm" className="gap-1.5" onClick={copyDetail}>
              {detailCopied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
              {t('timeline.diagram.copy_full')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
