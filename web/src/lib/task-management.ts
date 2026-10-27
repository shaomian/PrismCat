import { apiRequest, jsonInit } from './api'

export type TaskKind = 'trace' | 'capture' | 'experiment' | 'template' | 'run'
export type TaskAction = 'archive' | 'restore' | 'delete'
export interface TaskItem { id: string; revision?: number }
export interface TaskChange { kind: TaskKind; action: TaskAction; items: TaskItem[]; experiment_id?: string }
export interface TaskPreviewItem extends TaskItem {
  name: string
  log_count: number
  run_count: number
  report_count: number
  blocked_reason?: string
  referenced_by?: { id: string; name: string }[]
}
export interface TaskPreview {
  items: TaskPreviewItem[]
  total_items: number
  log_count: number
  run_count: number
  report_count: number
  can_apply: boolean
}
export async function previewTaskChange(value: TaskChange, signal?: AbortSignal): Promise<TaskPreview> {
  const response = await apiRequest('/api/task-management/preview', { ...jsonInit('POST', value), signal }, 'task_management.failed')
  return response.json()
}
export async function applyTaskChange(value: TaskChange): Promise<{ affected: number; deleted_logs: number }> {
  const response = await apiRequest('/api/task-management/apply', jsonInit('POST', value), 'task_management.failed')
  return response.json()
}
