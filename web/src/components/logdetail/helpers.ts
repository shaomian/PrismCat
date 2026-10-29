import type { LiveLogEvent, RequestLog } from '@/lib/api'

export interface LogDetailProps {
    log: RequestLog | null
    loading?: boolean
    onClose: () => void
    onLogChange?: (log: RequestLog) => void
    onNavigateLog?: (direction: LogNavigationDirection) => void
    canNavigatePreviousLog?: boolean
    canNavigateNextLog?: boolean
}

export type LogNavigationDirection = 'previous' | 'next'
export type BodyViewMode = 'pretty' | 'raw'
export type RequestViewMode = BodyViewMode | 'diff'
export type ResponseViewMode = BodyViewMode | 'merged'
export type PanelWidthMode = 'wide' | 'full'

export const logDetailWidthStorageKey = 'prismcat.logDetail.width'
export const logDetailExpandedStorageKey = 'prismcat.logDetail.expanded'
const autoLoadFullBodyLimit = 10 * 1024 * 1024

const defaultExpandedSections = {
    requestHeaders: false,
    requestBody: false,
    responseHeaders: false,
    responseBody: false,
}

export type LogDetailSection = keyof typeof defaultExpandedSections

export function getInitialPanelWidthMode(): PanelWidthMode {
    if (typeof window === 'undefined') return 'wide'

    const stored = window.localStorage.getItem(logDetailWidthStorageKey)
    if (stored === 'full') {
        return stored
    }

    return 'wide'
}

export function getInitialExpandedSections(): typeof defaultExpandedSections {
    if (typeof window === 'undefined') return defaultExpandedSections
    try {
        const raw = window.localStorage.getItem(logDetailExpandedStorageKey)
        if (!raw) return defaultExpandedSections
        const parsed = JSON.parse(raw) as Partial<typeof defaultExpandedSections>
        return {
            requestHeaders: typeof parsed?.requestHeaders === 'boolean' ? parsed.requestHeaders : defaultExpandedSections.requestHeaders,
            requestBody: typeof parsed?.requestBody === 'boolean' ? parsed.requestBody : defaultExpandedSections.requestBody,
            responseHeaders: typeof parsed?.responseHeaders === 'boolean' ? parsed.responseHeaders : defaultExpandedSections.responseHeaders,
            responseBody: typeof parsed?.responseBody === 'boolean' ? parsed.responseBody : defaultExpandedSections.responseBody,
        }
    } catch {
        return defaultExpandedSections
    }
}

export function shouldSubscribeLive(log: RequestLog): boolean {
    return log.latency_ms === 0 && !log.error
}

export function parseLiveEvent(event: MessageEvent<string>): LiveLogEvent | null {
    try {
        const payload = JSON.parse(event.data) as LiveLogEvent
        if (!payload || typeof payload !== 'object' || !('type' in payload)) {
            return null
        }
        return payload
    } catch {
        return null
    }
}

export function isBinaryPlaceholder(body: string) {
    return body.trimStart().startsWith('[binary content omitted;')
}

export function shouldAutoLoadBody({
    blobRef,
    bodySize,
    contentType,
    previewBody,
    fullBody,
    loading,
    previewOnly,
}: {
    blobRef?: string
    bodySize?: number
    contentType: string
    previewBody: string
    fullBody: string | null
    loading: boolean
    previewOnly: boolean
}) {
    if (!blobRef || fullBody !== null || loading || previewOnly) return false
    if (!bodySize || bodySize > autoLoadFullBodyLimit) return false
    if (isBinaryPlaceholder(previewBody)) return false
    return isAutoLoadableTextContent(contentType, previewBody)
}

function isAutoLoadableTextContent(contentType: string, previewBody: string) {
    const mediaType = contentType.split(';')[0]?.trim().toLowerCase() ?? ''
    if (!mediaType) return Boolean(previewBody.trim())
    if (mediaType.startsWith('text/')) return true
    if (
        mediaType === 'application/json' ||
        mediaType === 'application/xml' ||
        mediaType === 'application/x-ndjson' ||
        mediaType === 'application/x-www-form-urlencoded' ||
        mediaType === 'application/graphql' ||
        mediaType === 'application/javascript'
    ) {
        return true
    }
    return mediaType.endsWith('+json') || mediaType.endsWith('+xml')
}

export function normalizedAnnotation(annotation: RequestLog['annotation'] | undefined) {
    return {
        saved: annotation?.saved ?? false,
        status: annotation?.status ?? 'none',
        note: annotation?.note ?? '',
        labels: annotation?.labels ?? [],
    }
}

export function parseLabelDraft(value: string) {
    const seen = new Set<string>()
    const labels: string[] = []
    value
        .split(/[,，\n]/)
        .map(item => item.trim())
        .filter(Boolean)
        .forEach((label) => {
            if (seen.has(label)) return
            seen.add(label)
            labels.push(label)
        })
    return labels
}

export function textByteSize(text: string) {
    return new TextEncoder().encode(text).length
}

export function firstHeaderValue(headers: Record<string, string[]> | undefined, name: string) {
    if (!headers) return ''
    const direct = headers[name]
    if (direct?.length) return direct[0]

    const lowerName = name.toLowerCase()
    for (const [key, values] of Object.entries(headers)) {
        if (key.toLowerCase() === lowerName && values.length) {
            return values[0]
        }
    }
    return ''
}
