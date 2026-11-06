import type { AppConfig } from './api'

const BYTES_PER_GB = 1024 ** 3
// Keep day-based cleanup within Go's time.Duration range.
export const MAX_RETENTION_DAYS = 106751

export type StoragePolicy = Pick<AppConfig['storage'], 'retention_days' | 'max_storage_bytes'>
export type StoragePolicyDraft = { days: string; limitGB: string }

export function storagePolicyDraft(policy: StoragePolicy): StoragePolicyDraft {
    return { days: String(policy.retention_days), limitGB: String(policy.max_storage_bytes / BYTES_PER_GB) }
}

export function parseStoragePolicy(draft: StoragePolicyDraft): StoragePolicy | null {
    const days = Number(draft.days)
    const limitGB = Number(draft.limitGB)
    const bytes = Math.round(limitGB * BYTES_PER_GB)
    if (!draft.days.trim() || !Number.isSafeInteger(days) || days < 0 || days > MAX_RETENTION_DAYS) return null
    if (!draft.limitGB.trim() || !Number.isFinite(limitGB) || limitGB < 0 || !Number.isSafeInteger(bytes) || (limitGB > 0 && bytes === 0)) return null
    return { retention_days: days, max_storage_bytes: bytes }
}

export function storagePolicyChanged(draft: StoragePolicyDraft, saved: StoragePolicy): boolean {
    const parsed = parseStoragePolicy(draft)
    return !parsed || parsed.retention_days !== saved.retention_days || parsed.max_storage_bytes !== saved.max_storage_bytes
}
