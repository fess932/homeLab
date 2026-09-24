export class Limiter {
  private active = 0
  private readonly waiting: { resolve: () => void; reject: (e: unknown) => void; signal?: AbortSignal; onAbort?: () => void }[] = []

  constructor(readonly limit: number) {}

  get running(): number {
    return this.active
  }

  get queued(): number {
    return this.waiting.length
  }

  async run<T>(task: () => Promise<T>, signal?: AbortSignal): Promise<T> {
    await this.acquire(signal)
    try {
      return await task()
    } finally {
      this.release()
    }
  }

  private acquire(signal?: AbortSignal): Promise<void> {
    if (signal?.aborted) return Promise.reject(signal.reason ?? new DOMException('Aborted', 'AbortError'))
    if (this.active < this.limit) {
      this.active++
      return Promise.resolve()
    }
    return new Promise<void>((resolve, reject) => {
      const entry: (typeof this.waiting)[number] = { resolve, reject, signal }
      if (signal) {
        entry.onAbort = () => {
          const i = this.waiting.indexOf(entry)
          if (i >= 0) this.waiting.splice(i, 1)
          reject(signal.reason ?? new DOMException('Aborted', 'AbortError'))
        }
        signal.addEventListener('abort', entry.onAbort, { once: true })
      }
      this.waiting.push(entry)
    })
  }

  private release(): void {
    const next = this.waiting.shift()
    if (next) {
      if (next.signal && next.onAbort) next.signal.removeEventListener('abort', next.onAbort)
      next.resolve()
    } else {
      this.active--
    }
  }
}

export const chartLimiter = new Limiter(4)
