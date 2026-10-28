export const maxTraceLabels = 20

// Stored labels remain atomic; only newly typed/pasted text is split. Match the
// server's case-sensitive normalization without dropping an unfinished draft.
export function collectTraceLabels(labels: readonly string[], input: string): string[] {
  return [...new Set([...labels, ...input.split(/[,，\r\n]+/)].map(label => label.trim()).filter(Boolean))]
}
