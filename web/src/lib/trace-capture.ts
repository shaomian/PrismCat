import { useCallback, useRef, useState } from 'react'
import { fetchCorrelationStatus, type CaptureRun, type CorrelationStatus } from './api'
import { useVisiblePolling } from './use-visible-polling'

export const correlationSettingsPath = '/settings/logging/general#trace-correlation'
export const capturePageSize = 20
export const captureLogsPageSize = 50

export function captureNameBytes(name: string) {
  return new TextEncoder().encode(name.trim()).length
}

export function defaultCaptureName(upstream: string, date = new Date()) {
  const pad = (value: number) => String(value).padStart(2, '0')
  const suffix = ` · ${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
  let prefix = ''
  for (const character of upstream) {
    if (captureNameBytes(prefix + character + suffix) > 200) break
    prefix += character
  }
  return prefix + suffix
}

export function captureTokens(run: CaptureRun) {
  if (run.usage_input_tokens === undefined && run.usage_output_tokens === undefined) return undefined
  return (run.usage_input_tokens ?? 0) + (run.usage_output_tokens ?? 0)
}

export function useCorrelationStatus() {
  const [status, setStatus] = useState<CorrelationStatus | null>(null)
  const [error, setError] = useState('')
  const request = useRef(0)
  const refresh = useCallback(async (signal?: AbortSignal) => {
    const current = ++request.current
    try {
      const next = await fetchCorrelationStatus(signal)
      if (!signal?.aborted && current === request.current) { setStatus(next); setError('') }
    } catch (error) {
      if (!signal?.aborted && current === request.current) setError((error as Error).message)
    }
  }, [])
  useVisiblePolling(refresh, true, status?.active_captures.length || status?.enabled ? 5000 : 30000)
  return { status, error, refresh }
}
