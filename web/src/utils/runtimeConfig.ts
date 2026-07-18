declare global {
  interface Window {
    __RUNTIME_CONFIG__?: {
      portalUrl?: string
    }
  }
}

/** 部署时可由 runtime-config.js + 环境变量 VITE_PORTAL_URL / PUBLIC_HOST 覆盖 */
export function getPortalUrl(): string {
  return (
    window.__RUNTIME_CONFIG__?.portalUrl?.trim() ||
    import.meta.env.VITE_PORTAL_URL?.trim() ||
    'http://localhost:5174'
  )
}
