import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import mermaid from 'mermaid'
import { Copy, Check, Download } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { toast } from 'sonner'

let mermaidInitialized = false
let renderSeq = 0

function initMermaid() {
  const dark = document.documentElement.classList.contains('dark')
  mermaid.initialize({
    startOnLoad: false,
    theme: dark ? 'dark' : 'default',
    sequence: { actorMargin: 60, messageMargin: 36, useMaxWidth: true },
  })
  mermaidInitialized = true
}

export function TraceDiagram({ code }: { code: string }) {
  const { t } = useTranslation()
  const containerRef = useRef<HTMLDivElement>(null)
  const [error, setError] = useState(false)
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    if (!code) return
    if (!mermaidInitialized) initMermaid()
    let cancelled = false
    setError(false)
    mermaid
      .render(`trace-diagram-${++renderSeq}`, code)
      .then(({ svg }) => {
        if (!cancelled && containerRef.current) {
          containerRef.current.innerHTML = svg
        }
      })
      .catch(() => {
        if (!cancelled) setError(true)
      })
    return () => {
      cancelled = true
    }
  }, [code])

  const copySource = async () => {
    try {
      await navigator.clipboard.writeText(code)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
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

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Button variant="outline" size="sm" className="gap-1.5 h-7 text-xs" onClick={copySource}>
          {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
          {t('timeline.diagram.copy_source')}
        </Button>
        <Button variant="outline" size="sm" className="gap-1.5 h-7 text-xs" onClick={downloadSvg} disabled={error}>
          <Download className="h-3 w-3" />
          SVG
        </Button>
      </div>
      {error ? (
        <div className="rounded-md border border-warning/30 bg-warning/5 p-3">
          <p className="text-xs text-warning mb-2">{t('timeline.diagram.render_failed')}</p>
          <pre className="text-xs font-mono whitespace-pre-wrap text-foreground/70">{code}</pre>
        </div>
      ) : (
        <div ref={containerRef} className="overflow-x-auto rounded-md border border-border bg-card p-4 [&_svg]:max-w-none" />
      )}
    </div>
  )
}
