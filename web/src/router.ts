import { createRouter, createWebHistory, type RouteLocationNormalized } from 'vue-router'
import { api } from '@/api'
import { loadSession, loadSettings, store } from '@/stores/app'

declare module 'vue-router' {
  interface RouteMeta {
    anonymous?: boolean
    bare?: boolean
  }
}

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/setup', component: () => import('@/views/SetupView.vue'), meta: { anonymous: true, bare: true } },
    { path: '/login', component: () => import('@/views/LoginView.vue'), meta: { anonymous: true, bare: true } },
    { path: '/public', component: () => import('@/views/PublicView.vue'), meta: { anonymous: true, bare: true } },
    { path: '/', component: () => import('@/views/HomeView.vue') },
    { path: '/p/:slug', component: () => import('@/views/HomeView.vue') },
    { path: '/monitoring', component: () => import('@/views/monitoring/OverviewView.vue') },
    { path: '/monitoring/nodes/:sourceId', component: () => import('@/views/monitoring/NodeView.vue') },
    { path: '/monitoring/services/:serviceId', component: () => import('@/views/monitoring/ServiceView.vue') },
    { path: '/sources', component: () => import('@/views/SourcesView.vue') },
    { path: '/settings/:tab?', component: () => import('@/views/SettingsView.vue') },
    { path: '/:rest(.*)*', redirect: '/' },
  ],
})

async function guard(to: RouteLocationNormalized) {
  if (to.meta.anonymous) return true
  if (store.session || (await loadSession())) {
    if (!store.settings) void loadSettings().catch(() => undefined)
    return true
  }
  try {
    if ((await api.setupRequired()).required) return '/setup'
  } catch {
    return { path: '/login', query: { next: to.fullPath } }
  }
  return { path: '/login', query: { next: to.fullPath } }
}

router.beforeEach(guard)

api.http.setUnauthorizedHandler(() => {
  store.session = null
  const current = router.currentRoute.value
  if (!current.meta.anonymous) void router.replace({ path: '/login', query: { next: current.fullPath } })
})
