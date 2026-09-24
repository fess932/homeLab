import {
  Activity,
  Archive,
  Camera,
  Cloud,
  Cpu,
  Database,
  Download,
  Film,
  Gamepad2,
  Globe,
  HardDrive,
  House,
  Image,
  Lightbulb,
  Lock,
  Mail,
  Monitor,
  Music,
  Network,
  Printer,
  Router,
  Server,
  Shield,
  Terminal,
  Thermometer,
  Tv,
  Wifi,
  Wrench,
  type LucideIcon,
} from 'lucide-vue-next'

export const builtinIcons: Record<string, LucideIcon> = {
  activity: Activity,
  archive: Archive,
  camera: Camera,
  cloud: Cloud,
  cpu: Cpu,
  database: Database,
  download: Download,
  film: Film,
  gamepad: Gamepad2,
  globe: Globe,
  'hard-drive': HardDrive,
  house: House,
  image: Image,
  lightbulb: Lightbulb,
  lock: Lock,
  mail: Mail,
  monitor: Monitor,
  music: Music,
  network: Network,
  printer: Printer,
  router: Router,
  server: Server,
  shield: Shield,
  terminal: Terminal,
  thermometer: Thermometer,
  tv: Tv,
  wifi: Wifi,
  wrench: Wrench,
}

export type ParsedIcon = { kind: 'builtin'; name: string } | { kind: 'asset'; id: string } | { kind: 'favicon' } | { kind: 'none' }

export function parseIcon(icon: string | undefined | null): ParsedIcon {
  if (!icon) return { kind: 'none' }
  if (icon.startsWith('builtin:')) return { kind: 'builtin', name: icon.slice(8) }
  if (icon.startsWith('asset:')) return { kind: 'asset', id: icon.slice(6) }
  if (icon === 'favicon') return { kind: 'favicon' }
  return { kind: 'none' }
}
