import { onBeforeUnmount, onMounted, readonly, ref, watch, type Ref } from 'vue'

export const REFRESH_MS = 15_000

const paused = ref(false)
const visible = ref(typeof document === 'undefined' ? true : document.visibilityState !== 'hidden')
const tick = ref(0)

let listeners = 0
let timer: ReturnType<typeof setInterval> | undefined

function onVisibility() {
  const was = visible.value
  visible.value = document.visibilityState !== 'hidden'
  if (!was && visible.value && !paused.value) tick.value++
}

function start() {
  stop()
  if (paused.value || !visible.value) return
  timer = setInterval(() => tick.value++, REFRESH_MS)
}

function stop() {
  if (timer) clearInterval(timer)
  timer = undefined
}

watch([paused, visible], start)

export function useRefreshClock() {
  onMounted(() => {
    if (listeners++ === 0) {
      document.addEventListener('visibilitychange', onVisibility)
      start()
    }
  })
  onBeforeUnmount(() => {
    if (--listeners === 0) {
      document.removeEventListener('visibilitychange', onVisibility)
      stop()
    }
  })
  return {
    tick: readonly(tick),
    paused,
    visible: readonly(visible),
    refresh: () => tick.value++,
    toggle: () => (paused.value = !paused.value),
  }
}

export function usePolling(fn: (signal: AbortSignal) => Promise<void>, deps: Ref<unknown>[] = []) {
  const clock = useRefreshClock()
  let controller: AbortController | undefined
  const loading = ref(false)

  async function run() {
    controller?.abort()
    const c = new AbortController()
    controller = c
    loading.value = true
    try {
      await fn(c.signal)
    } finally {
      if (controller === c) loading.value = false
    }
  }

  watch([clock.tick, ...deps], () => void run())
  onMounted(() => void run())
  onBeforeUnmount(() => controller?.abort())

  return { loading, run, ...clock }
}
