import { describe, expect, it } from 'vitest'
import { Limiter } from './queue'

// Ручной "отложенный" промис, чтобы управлять моментом завершения задачи.
function deferred<T = void>() {
  let resolve!: (v: T) => void
  const promise = new Promise<T>((r) => (resolve = r))
  return { promise, resolve }
}

const flush = () => new Promise((r) => setTimeout(r, 0))

describe('Limiter', () => {
  it('runs at most N tasks concurrently', async () => {
    const limiter = new Limiter(4)
    let active = 0
    let peak = 0
    const gates = Array.from({ length: 10 }, () => deferred())
    const runs = gates.map((g) =>
      limiter.run(async () => {
        active++
        peak = Math.max(peak, active)
        await g.promise
        active--
      }),
    )
    await flush()
    expect(limiter.running).toBe(4)
    expect(limiter.queued).toBe(6)

    for (const g of gates) {
      g.resolve()
      await flush()
    }
    await Promise.all(runs)
    expect(peak).toBe(4)
    expect(limiter.running).toBe(0)
    expect(limiter.queued).toBe(0)
  })

  it('releases slot when a task fails', async () => {
    const limiter = new Limiter(1)
    await expect(limiter.run(async () => Promise.reject(new Error('boom')))).rejects.toThrow('boom')
    await expect(limiter.run(async () => 42)).resolves.toBe(42)
  })

  it('drops queued task on abort without consuming a slot', async () => {
    const limiter = new Limiter(1)
    const gate = deferred()
    const first = limiter.run(() => gate.promise)
    const controller = new AbortController()
    let started = false
    const second = limiter.run(async () => {
      started = true
    }, controller.signal)
    await flush()
    expect(limiter.queued).toBe(1)

    controller.abort()
    await expect(second).rejects.toBeDefined()
    expect(limiter.queued).toBe(0)

    gate.resolve()
    await first
    expect(started).toBe(false)
    expect(limiter.running).toBe(0)
  })

  it('rejects immediately if signal already aborted', async () => {
    const limiter = new Limiter(2)
    const c = new AbortController()
    c.abort()
    await expect(limiter.run(async () => 1, c.signal)).rejects.toBeDefined()
    expect(limiter.running).toBe(0)
  })
})
