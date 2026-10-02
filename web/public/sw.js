const SHELL = 'homedeck-shell'
const ASSETS = 'homedeck-assets'
const STATIC = 'homedeck-static:'
const SHELL_KEY = '/public/'

const staticName = (html) => STATIC + (/\/static\/index-[\w-]+\.js/.exec(html)?.[0] ?? 'default')

async function cachedShell() {
  const cache = await caches.open(SHELL)
  return cache.match(SHELL_KEY)
}

async function currentStaticName() {
  const shell = await cachedShell()
  return staticName(shell ? await shell.text() : '')
}

async function refreshShell(request) {
  const res = await fetch(request)
  if (res.ok && (res.headers.get('Content-Type') ?? '').startsWith('text/html')) {
    const cache = await caches.open(SHELL)
    await cache.put(SHELL_KEY, res.clone())
  }
  return res
}

async function pruneStatic(keep) {
  for (const name of await caches.keys()) {
    if (name.startsWith(STATIC) && name !== keep) await caches.delete(name)
  }
}

async function navigate(event) {
  const cached = await cachedShell()
  if (!cached) return refreshShell(event.request)
  const keep = staticName(await cached.clone().text())
  event.waitUntil(
    pruneStatic(keep)
      .then(() => refreshShell(event.request))
      .catch(() => undefined),
  )
  return cached
}

async function cacheFirst(request, cacheName) {
  const hit = await caches.match(request)
  if (hit) return hit
  const res = await fetch(request)
  if (res.ok) {
    const cache = await caches.open(await cacheName())
    await cache.put(request, res.clone())
  }
  return res
}

self.addEventListener('install', () => self.skipWaiting())
self.addEventListener('activate', (event) => event.waitUntil(self.clients.claim()))

self.addEventListener('fetch', (event) => {
  const { request } = event
  if (request.method !== 'GET') return
  const url = new URL(request.url)
  if (url.origin !== self.location.origin) return
  if (request.mode === 'navigate') {
    if (url.pathname.startsWith('/public/')) event.respondWith(navigate(event))
    return
  }
  if (url.pathname.startsWith('/static/')) {
    event.respondWith(cacheFirst(request, currentStaticName))
  } else if (url.pathname.startsWith('/assets/') || url.pathname === '/favicon.svg') {
    event.respondWith(cacheFirst(request, async () => ASSETS))
  }
})
