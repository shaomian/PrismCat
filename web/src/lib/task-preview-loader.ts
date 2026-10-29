// 只在悬停延迟结束后加载；缓存中不保存 DOM 节点或组件回调。
export class TaskPreviewLoader<T> {
  private timer: ReturnType<typeof setTimeout> | undefined
  private controller: AbortController | undefined
  private sequence = 0
  private cache = new Map<string, { expires: number; value: T }>()

  cancel() {
    this.sequence++
    if (this.timer !== undefined) clearTimeout(this.timer)
    this.timer = undefined
    this.controller?.abort()
    this.controller = undefined
  }

  reset() {
    this.cancel()
    this.cache.clear()
  }

  schedule(load: (signal: AbortSignal) => Promise<T> | T, ready: (value: T) => void, key?: string, ttl = 30_000, delay = 550) {
    this.cancel()
    const sequence = this.sequence
    this.timer = setTimeout(() => {
      this.timer = undefined
      const cached = key ? this.cache.get(key) : undefined
      if (cached && key && cached.expires > Date.now()) {
        this.cache.delete(key)
        this.cache.set(key, cached)
        ready(cached.value)
        return
      }
      if (key) this.cache.delete(key)
      const controller = new AbortController()
      this.controller = controller
      Promise.resolve().then(() => load(controller.signal)).then(value => {
        if (controller.signal.aborted || sequence !== this.sequence) return
        if (key) {
          this.cache.set(key, { value, expires: Date.now() + ttl })
          while (this.cache.size > 32) {
            const oldest = this.cache.keys().next().value
            if (oldest === undefined) break
            this.cache.delete(oldest)
          }
        }
        ready(value)
      }).catch(() => {
        // 预览失败保持安静，卡片原有的打开操作仍然可用。
      }).finally(() => {
        if (this.controller === controller) this.controller = undefined
      })
    }, delay)
  }
}
