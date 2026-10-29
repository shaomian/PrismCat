import { type Dispatch, type SetStateAction } from 'react'
import type { NavigateFunction } from 'react-router-dom'

import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { TraceCorrelationSetting } from '@/components/TraceCorrelationSetting'
import { LoggingRules, type LoggingRulesTab } from '@/pages/LoggingRules'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { TranslationFn } from './helpers'
import {
    FieldBlock,
    SettingSection,
    ToggleSetting,
} from './shared'

type LoggingTabProps = {
    t: TranslationFn
    loggingSection: 'general' | LoggingRulesTab
    navigate: NavigateFunction
    maxRequestBody: number
    setMaxRequestBody: Dispatch<SetStateAction<number>>
    maxResponseBody: number
    setMaxResponseBody: Dispatch<SetStateAction<number>>
    detachBodyOver: number
    setDetachBodyOver: Dispatch<SetStateAction<number>>
    bodyPreview: number
    setBodyPreview: Dispatch<SetStateAction<number>>
    retentionDays: number
    setRetentionDays: Dispatch<SetStateAction<number>>
    maxStorageGB: number
    setMaxStorageGB: Dispatch<SetStateAction<number>>
    earlyRequestBodySnapshot: boolean
    setEarlyRequestBodySnapshot: Dispatch<SetStateAction<boolean>>
    storeBase64: boolean
    setStoreBase64: Dispatch<SetStateAction<boolean>>
    sensitiveHeaders: string
    setSensitiveHeaders: Dispatch<SetStateAction<string>>
}

export function LoggingTab({
    t,
    loggingSection,
    navigate,
    maxRequestBody,
    setMaxRequestBody,
    maxResponseBody,
    setMaxResponseBody,
    detachBodyOver,
    setDetachBodyOver,
    bodyPreview,
    setBodyPreview,
    retentionDays,
    setRetentionDays,
    maxStorageGB,
    setMaxStorageGB,
    earlyRequestBodySnapshot,
    setEarlyRequestBodySnapshot,
    storeBase64,
    setStoreBase64,
    sensitiveHeaders,
    setSensitiveHeaders,
}: LoggingTabProps) {
    return (
        <div className="animate-in fade-in slide-in-from-bottom-4 duration-300 motion-reduce:animate-none motion-reduce:duration-0">
            <div className="mx-auto max-w-7xl space-y-6">
                <Tabs
                    value={loggingSection}
                    onValueChange={value => navigate(`/settings/logging/${value}`)}
                >
                    <TabsList>
                        <TabsTrigger value="general">{t('logging_rules.tabs.general')}</TabsTrigger>
                        <TabsTrigger value="models">{t('logging_rules.tabs.models')}</TabsTrigger>
                        <TabsTrigger value="ignored">{t('logging_rules.tabs.ignored')}</TabsTrigger>
                    </TabsList>
                </Tabs>

                {loggingSection === 'general' ? (
                <>
                <TraceCorrelationSetting />
                <SettingSection
                    title={t('settings.section_content_size')}
                    description={t('settings.section_content_size_desc')}
                >
                    <div className="grid grid-cols-1 gap-x-6 gap-y-6 sm:grid-cols-2 lg:grid-cols-4">
                        <FieldBlock
                            label={t('settings.max_request_body')}
                            hint={t('settings.max_request_body_hint')}
                            htmlFor="max-req"
                            unit="KB"
                        >
                            <Input
                                id="max-req"
                                type="number"
                                min="1"
                                value={maxRequestBody}
                                onChange={e => setMaxRequestBody(Number(e.target.value))}
                                className="h-9 w-full rounded-md border-input bg-background text-sm transition-colors focus-visible:bg-background"
                            />
                        </FieldBlock>
                        <FieldBlock
                            label={t('settings.max_response_body')}
                            hint={t('settings.max_response_body_hint')}
                            htmlFor="max-res"
                            unit="KB"
                        >
                            <Input
                                id="max-res"
                                type="number"
                                min="1"
                                value={maxResponseBody}
                                onChange={e => setMaxResponseBody(Number(e.target.value))}
                                className="h-9 w-full rounded-md border-input bg-background text-sm transition-colors focus-visible:bg-background"
                            />
                        </FieldBlock>
                        <FieldBlock
                            label={t('settings.detach_body_over_bytes')}
                            hint={t('settings.detach_body_over_bytes_hint')}
                            htmlFor="detach-over"
                            unit="KB"
                        >
                            <Input
                                id="detach-over"
                                type="number"
                                min="0"
                                value={detachBodyOver}
                                onChange={e => setDetachBodyOver(Number(e.target.value))}
                                className="h-9 w-full rounded-md border-input bg-background text-sm transition-colors focus-visible:bg-background"
                            />
                        </FieldBlock>
                        <FieldBlock
                            label={t('settings.body_preview_bytes')}
                            hint={t('settings.body_preview_bytes_hint')}
                            htmlFor="body-preview"
                            unit="KB"
                        >
                            <Input
                                id="body-preview"
                                type="number"
                                min="0"
                                value={bodyPreview}
                                onChange={e => setBodyPreview(Number(e.target.value))}
                                className="h-9 w-full rounded-md border-input bg-background text-sm transition-colors focus-visible:bg-background"
                            />
                        </FieldBlock>
                    </div>
                </SettingSection>

                <SettingSection
                    title={t('settings.section_retention')}
                    description={t('settings.section_retention_desc')}
                >
                    <div className="grid grid-cols-1 gap-x-6 gap-y-6 sm:grid-cols-2 lg:grid-cols-4">
                        <FieldBlock
                            label={t('settings.retention_days')}
                            hint={t('settings.retention_days_hint')}
                            htmlFor="retention-days"
                            unit={t('settings.days')}
                        >
                            <Input
                                id="retention-days"
                                type="number"
                                min="0"
                                value={retentionDays}
                                onChange={e => setRetentionDays(Number(e.target.value))}
                                className="h-9 w-full rounded-md border-input bg-background text-sm transition-colors focus-visible:bg-background"
                            />
                        </FieldBlock>
                        <FieldBlock
                            label={t('settings.max_storage_bytes')}
                            hint={t('settings.max_storage_bytes_hint')}
                            htmlFor="max-storage"
                            unit="GB"
                        >
                            <Input
                                id="max-storage"
                                type="number"
                                min="0"
                                step="0.1"
                                value={maxStorageGB}
                                onChange={e => setMaxStorageGB(Number(e.target.value))}
                                className="h-9 w-full rounded-md border-input bg-background text-sm transition-colors focus-visible:bg-background"
                            />
                        </FieldBlock>
                    </div>
                </SettingSection>

                <SettingSection
                    title={t('settings.section_logging_behavior')}
                    description={t('settings.section_logging_behavior_desc')}
                >
                    <div className="grid grid-cols-1 gap-x-6 gap-y-6 sm:grid-cols-2">
                        <ToggleSetting
                            label={t('settings.early_request_body_snapshot')}
                            description={t('settings.early_request_body_snapshot_hint')}
                            checked={earlyRequestBodySnapshot}
                            onCheckedChange={setEarlyRequestBodySnapshot}
                        />
                        <ToggleSetting
                            label={t('settings.store_base64')}
                            description={t('settings.store_base64_hint')}
                            checked={storeBase64}
                            onCheckedChange={setStoreBase64}
                        />
                    </div>
                </SettingSection>

                <SettingSection
                    title={t('settings.section_sensitive_headers')}
                    description={t('settings.section_sensitive_headers_desc')}
                >
                    <Textarea
                        value={sensitiveHeaders}
                        onChange={e => setSensitiveHeaders(e.target.value)}
                        rows={4}
                        className="min-h-[112px] w-full rounded-md border-input bg-background font-mono text-sm leading-relaxed transition-colors focus-visible:bg-background resize-y"
                        placeholder="Authorization&#10;x-api-key&#10;api-key"
                    />
                </SettingSection>
                </>
                ) : (
                    <LoggingRules tab={loggingSection} embedded />
                )}
            </div>
        </div>
    )
}
