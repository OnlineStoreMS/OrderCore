declare global {
  interface Window {
    __RUNTIME_CONFIG__?: {
      portalUrl?: string
      shippingCoreUrl?: string
    }
  }
}

function trimUrl(v?: string | null): string {
  return (v || '').trim().replace(/\/$/, '')
}

function isLocalHost(hostname: string): boolean {
  return hostname === 'localhost' || hostname === '127.0.0.1' || hostname === '::1'
}

/** 与当前访问主机同机的 UserCore 应用中心 */
function portalFromLocation(): string {
  if (typeof window === 'undefined' || !window.location?.hostname) return ''
  const { protocol, hostname } = window.location
  if (!hostname || isLocalHost(hostname)) return ''
  return `${protocol}//${hostname}:5174`
}

function deriveHostBase(port: number, fallbackHost = 'localhost'): string {
  const portal = getPortalUrl()
  try {
    const u = new URL(portal)
    return `${u.protocol}//${u.hostname}:${port}`
  } catch {
    return `http://${fallbackHost}:${port}`
  }
}

/**
 * 门户地址优先级：
 * 1) runtime-config.js（部署注入）
 * 2) 当前访问主机推导（避免局域网打开订单中心却跳到 localhost）
 * 3) 构建期 VITE_PORTAL_URL（仅非 localhost 才用，防止误写死）
 * 4) http://localhost:5174
 */
export function getPortalUrl(): string {
  const fromRuntime = trimUrl(window.__RUNTIME_CONFIG__?.portalUrl)
  if (fromRuntime) return fromRuntime

  const fromHost = portalFromLocation()
  if (fromHost) return fromHost

  const fromEnv = trimUrl(import.meta.env.VITE_PORTAL_URL)
  if (fromEnv && !/^https?:\/\/(localhost|127\.0\.0\.1)(:|\/|$)/i.test(fromEnv)) {
    return fromEnv
  }

  return 'http://localhost:5174'
}

/** 发货中心 Web 根地址（默认 :5181） */
export function getShippingCoreUrl(): string {
  const fromRuntime = trimUrl(window.__RUNTIME_CONFIG__?.shippingCoreUrl)
  if (fromRuntime) return fromRuntime

  const fromEnv = trimUrl(import.meta.env.VITE_SHIPPINGCORE_URL)
  if (fromEnv) return fromEnv

  // 与当前打开的订单中心同主机，避免 portal 推导到 localhost 导致跳错
  if (typeof window !== 'undefined' && window.location?.hostname) {
    const { protocol, hostname } = window.location
    if (hostname) return `${protocol}//${hostname}:5181`
  }

  return deriveHostBase(5181)
}
