import { useEffect, useState } from 'react'

// Navigation-only memory; no log bodies or credentials are persisted to disk.
const values = new Map<string, unknown>()
export function useListMemory<T>(key: string, initial: T | (() => T)) {
  const [value, setValue] = useState<T>(() => values.has(key) ? values.get(key) as T : typeof initial === 'function' ? (initial as () => T)() : initial)
  useEffect(() => { values.set(key, value) }, [key, value])
  return [value, setValue] as const
}
