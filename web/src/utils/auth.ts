import { getPortalUrl } from './runtimeConfig'

const TOKEN_KEY = 'uc_access_token'
const REFRESH_KEY = 'uc_refresh_token'
const EXPIRES_KEY = 'uc_expires_at'

let sessionVerified = false
let refreshPromise: Promise<boolean> | null = null
let keepAliveTimer: ReturnType<typeof setTimeout> | null = null
let keepAliveStarted = false

const RENEW_BEFORE_MS = 5 * 60 * 1000
const MIN_DELAY_MS = 15 * 1000

export interface SessionUser {
  id: number
  email: string
  displayName: string
  isPlatform: boolean
}

export interface SessionTenant {
  id: number
  companyId: number
  name: string
  code: string
}

export interface SessionInfo {
  user: SessionUser
  tenant: SessionTenant
  tenants: SessionTenant[]
}

export function getToken(): string | undefined {
  return localStorage.getItem(TOKEN_KEY) || undefined
}

export function getRefreshToken(): string | undefined {
  return localStorage.getItem(REFRESH_KEY) || undefined
}

export function getExpiresAt(): number | undefined {
  const raw = localStorage.getItem(EXPIRES_KEY)
  if (!raw) return undefined
  const n = Number(raw)
  return Number.isFinite(n) ? n : undefined
}

export function saveToken(token: string) {
  localStorage.setItem(TOKEN_KEY, token)
}

export function saveRefreshToken(token: string) {
  localStorage.setItem(REFRESH_KEY, token)
}

export function saveExpiresAt(expiresAt: number) {
  localStorage.setItem(EXPIRES_KEY, String(expiresAt))
}

/** Cookie SSO：仅记录过期时间；access/refresh 由 httpOnly Cookie 持有（过渡期仍可写入 localStorage） */
export function saveAuthTokens(accessToken: string, refreshToken?: string, expiresAt?: number) {
  // 不再依赖 localStorage 中的 JWT；若传入 token 仅用于解析 exp
  const exp = expiresAt || (accessToken ? readJwtExp(accessToken) : undefined)
  if (exp) saveExpiresAt(exp)
  // 清理历史遗留明文 JWT
  localStorage.removeItem(TOKEN_KEY)
  localStorage.removeItem(REFRESH_KEY)
}

function readJwtExp(token: string): number | undefined {
  try {
    const part = token.split('.')[1]
    if (!part) return undefined
    const json = atob(part.replace(/-/g, '+').replace(/_/g, '/'))
    const payload = JSON.parse(json) as { exp?: number }
    return payload.exp
  } catch {
    return undefined
  }
}

export function clearToken() {
  localStorage.removeItem(TOKEN_KEY)
  localStorage.removeItem(REFRESH_KEY)
  localStorage.removeItem(EXPIRES_KEY)
  sessionVerified = false
  sessionStorage.removeItem('uc_session_profile')
  stopTokenKeepAlive()
}

export function trustFreshToken() {
  sessionVerified = true
}

export function resetSessionVerification() {
  sessionVerified = false
}

export function redirectToPortal() {
  window.location.href = `${getPortalUrl()}/login`
}

export function portalAppsUrl() {
  return `${getPortalUrl()}/apps`
}

export function portalLoginUrl() {
  return `${getPortalUrl()}/login`
}

export function iamBase(): string {
  return (
    import.meta.env.VITE_IAM_API_URL ||
    (import.meta.env.VITE_API_GATEWAY ? '/api/v1' : '/iam')
  )
}

/** SSO：签发一次性 code（跨应用跳转时用目标应用的 redirectUri） */
export async function authorizeSso(opts: {
  appCode?: string
  redirectUri?: string
}): Promise<{ code: string; redirectUri: string; expiresIn: number }> {
  const token = getToken()
  if (!token) throw new Error('未登录')
  const res = await fetch(`${iamBase()}/auth/sso/authorize`, {
    method: 'POST',
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify({
      appCode: opts.appCode,
      redirectUri: opts.redirectUri,
    }),
  })
  const body = await res.json()
  if (body.code !== 200 || !body.data?.code || !body.data?.redirectUri) {
    throw new Error(body.message || 'SSO 授权失败')
  }
  return {
    code: body.data.code as string,
    redirectUri: body.data.redirectUri as string,
    expiresIn: body.data.expiresIn as number,
  }
}

/** SSO：用一次性 code 换 access/refresh（禁止从 URL 接收 JWT） */
export async function exchangeSsoCode(code: string): Promise<{
  accessToken: string
  refreshToken?: string
  expiresAt?: number
}> {
  const redirectUri = `${window.location.origin}/auth/callback`
  const res = await fetch(`${iamBase()}/auth/sso/token`, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ code, redirectUri }),
  })
  const body = await res.json()
  if (body.code !== 200 || !body.data?.accessToken) {
    throw new Error(body.message || 'SSO 换票失败')
  }
  return {
    accessToken: body.data.accessToken as string,
    refreshToken: body.data.refreshToken as string | undefined,
    expiresAt: body.data.expiresAt as number | undefined,
  }
}

/** 静默续期；并发复用同一次请求 */
export async function tryRefreshAccessToken(): Promise<boolean> {
  if (refreshPromise) return refreshPromise
  refreshPromise = (async () => {
    const refreshToken = getRefreshToken()
    try {
      const res = await fetch(`${iamBase()}/auth/refresh`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(refreshToken ? { refreshToken } : {}),
      })
      const body = await res.json()
      if (body.code !== 200 || !body.data?.accessToken) return false
      saveAuthTokens(body.data.accessToken, body.data.refreshToken, body.data.expiresAt)
      sessionVerified = true
      scheduleKeepAlive()
      return true
    } catch {
      return false
    } finally {
      refreshPromise = null
    }
  })()
  return refreshPromise
}

function scheduleKeepAlive() {
  if (keepAliveTimer) {
    clearTimeout(keepAliveTimer)
    keepAliveTimer = null
  }
  const exp = getExpiresAt()
  if (!exp || !getRefreshToken()) return
  const delay = Math.max(exp * 1000 - Date.now() - RENEW_BEFORE_MS, MIN_DELAY_MS)
  keepAliveTimer = setTimeout(() => {
    void (async () => {
      await tryRefreshAccessToken()
      scheduleKeepAlive()
    })()
  }, delay)
}

function onVisibility() {
  if (document.visibilityState === 'visible') {
    const exp = getExpiresAt()
    if (!exp || exp * 1000 - Date.now() <= RENEW_BEFORE_MS) {
      void tryRefreshAccessToken().then(() => scheduleKeepAlive())
    }
  }
}

export function startTokenKeepAlive() {
  if (!keepAliveStarted) {
    keepAliveStarted = true
    document.addEventListener('visibilitychange', onVisibility)
  }
  scheduleKeepAlive()
}

export function stopTokenKeepAlive() {
  if (keepAliveTimer) {
    clearTimeout(keepAliveTimer)
    keepAliveTimer = null
  }
  if (keepAliveStarted) {
    document.removeEventListener('visibilitychange', onVisibility)
    keepAliveStarted = false
  }
}

export async function verifySession(): Promise<boolean> {
  const { fetchSession } = await import('../api/session')
  const info = await fetchSession()
  return info !== null
}

export async function ensureSession(): Promise<boolean> {
  const exp = getExpiresAt()
  if (exp && exp * 1000 <= Date.now()) {
    const refreshed = await tryRefreshAccessToken()
    if (!refreshed) return false
  }
  if (sessionVerified) return true
  const ok = await verifySession()
  if (ok) {
    sessionVerified = true
    startTokenKeepAlive()
  }
  return ok
}
