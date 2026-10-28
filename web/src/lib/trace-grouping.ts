import { apiRequest, jsonInit } from '@/lib/api'

export type GroupingAction = 'merge' | 'move' | 'split'
export interface GroupingRequest { action: GroupingAction; trace_ids: string[]; log_ids?: string[]; target_trace_id?: string; version?: string }
export interface GroupingPreview { version: string; log_count: number; trace_count: number; target_trace_id: string; target_name: string; can_apply: boolean; reason?: string }
export interface GroupingResult { target_trace_id: string; log_count: number }
export async function previewTraceGrouping(value: GroupingRequest, signal?: AbortSignal): Promise<GroupingPreview> {
  const response = await apiRequest('/api/trace-grouping/preview', { ...jsonInit('POST', value), signal }, 'trace_grouping.failed')
  return response.json()
}
export async function applyTraceGrouping(value: GroupingRequest): Promise<GroupingResult> {
  const response = await apiRequest('/api/trace-grouping/apply', jsonInit('POST', value), 'trace_grouping.failed')
  return response.json()
}
