declare global {
  interface Window {
    __RUNTIME_CONFIG__?: {
      portalUrl?: string
    }
  }
}

/** 部署时可由 runtime-config.js + 环境变量 VITE_PORTAL_URL / PUBLIC_HOST 覆盖 */
export function getPortalUrl(): string {
  const fromRuntime = window.__RUNTIME_CONFIG__?.portalUrl?.trim()
  if (fromRuntime) return fromRuntime.replace(/\/$/, '')

  const fromEnv = import.meta.env.VITE_PORTAL_URL?.trim()
  if (fromEnv) return fromEnv.replace(/\/$/, '')

  // 兜底：与当前访问主机同机的 UserCore 应用中心（避免热更新冲掉 runtime-config 后落到 localhost）
  if (typeof window !== 'undefined' && window.location?.hostname) {
    const { protocol, hostname } = window.location
    if (hostname && hostname !== 'localhost' && hostname !== '127.0.0.1') {
      return `${protocol}//${hostname}:5174`
    }
  }
  return 'http://localhost:5174'
}
