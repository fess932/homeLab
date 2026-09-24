import { onBeforeUnmount, ref, watchEffect, type Ref } from 'vue'
import { assetUrl, type Theme } from '@/api'
import { breakpointFor } from '@/lib/grid'

export function contrastText(hex: string): string {
  const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(hex)
  if (!m) return '#ffffff'
  const [r, g, b] = [m[1]!, m[2]!, m[3]!].map((h) => {
    const c = parseInt(h, 16) / 255
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
  }) as [number, number, number]
  const lum = 0.2126 * r + 0.7152 * g + 0.0722 * b
  return lum > 0.4 ? '#0b1220' : '#ffffff'
}

export function applyTheme(theme: Theme | null | undefined) {
  const root = document.documentElement
  if (!theme) {
    root.removeAttribute('data-theme')
    root.removeAttribute('data-density')
    root.style.removeProperty('--accent')
    root.style.removeProperty('--accent-contrast')
    root.style.removeProperty('--page-bg-image')
    return
  }
  if (theme.mode === 'system') root.removeAttribute('data-theme')
  else root.setAttribute('data-theme', theme.mode)
  root.setAttribute('data-density', theme.density)
  root.style.setProperty('--accent', theme.accent)
  root.style.setProperty('--accent-contrast', contrastText(theme.accent))
  const bg = assetUrl(theme.background_asset_id)
  if (bg) root.style.setProperty('--page-bg-image', `url("${bg}")`)
  else root.style.removeProperty('--page-bg-image')
}

export function usePageTheme(theme: Ref<Theme | null | undefined>) {
  watchEffect(() => applyTheme(theme.value))
  onBeforeUnmount(() => applyTheme(null))
}

export function useViewportBreakpoint() {
  const bp = ref(breakpointFor(typeof window === 'undefined' ? 1280 : window.innerWidth))
  const update = () => (bp.value = breakpointFor(window.innerWidth))
  window.addEventListener('resize', update, { passive: true })
  onBeforeUnmount(() => window.removeEventListener('resize', update))
  return bp
}
