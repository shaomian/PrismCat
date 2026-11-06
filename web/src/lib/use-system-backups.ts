import { useCallback, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { fetchBackups, resumeStorageCleanup, type BackupJob, type BackupImportJob } from './api'
import { useVisiblePolling } from './use-visible-polling'

export function useSystemBackups(initiallyPaused: boolean) {
    const { t } = useTranslation()
    const [jobs, setJobs] = useState<BackupJob[]>([])
    const [cleanupPaused, setCleanupPaused] = useState(initiallyPaused)
    const [error, setError] = useState('')
    const [importJob, setImportJob] = useState<BackupImportJob | null>(null)
    const [importSupported, setImportSupported] = useState(false)
    const [importError, setImportError] = useState('')
    const request = useRef(0)
    const refresh = useCallback(async (signal?: AbortSignal) => {
        const current = ++request.current
        try {
            const result = await fetchBackups(signal)
            if (signal?.aborted || current !== request.current) return
            setJobs(result.backups)
            setCleanupPaused(result.cleanup_paused)
            setImportJob(result.import_job ?? null)
            setImportSupported(result.import_supported ?? false)
            setImportError(result.import_error ?? '')
            setError('')
        } catch {
            if (!signal?.aborted && current === request.current) setError(t('backups.load_failed'))
        }
    }, [t])
    const running = jobs.some(job => job.state === 'running') || importJob?.state === 'checking' || importJob?.state === 'uploading'
    useVisiblePolling(refresh, true, running ? 1500 : 10000)

    function created(job: BackupJob) {
        setJobs(current => [job, ...current.filter(item => item.id !== job.id)])
    }

    async function resumeCleanup() {
        await resumeStorageCleanup()
        // Invalidate any response sampled before cleanup resumed.
        request.current++
        setCleanupPaused(false)
        await refresh()
    }

    return { jobs, cleanupPaused, error, running, refresh, created, resumeCleanup, importJob, importSupported, importError }
}

export type SystemBackupStatus = ReturnType<typeof useSystemBackups>
