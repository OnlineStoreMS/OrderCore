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

/** 保存门户下发的 access / refresh；expiresAt 可选，缺省时从 JWT payload 解析 */
export function saveAuthTokens(accessToken: string, refreshToken?: string, expiresAt?: number) {
  saveToken(accessToken)
  if (refreshToken) saveRefreshToken(refreshToken)
  const exp = expiresAt || readJwtExp(accessToken)
  if (exp) saveExpiresAt(exp)
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

function iamBase(): string {
  return (
    import.meta.env.VITE_IAM_API_URL ||
    (import.meta.env.VITE_API_GATEWAY ? '/api/v1' : '/iam')
  )
}

/** 静默续期；并发复用同一次请求 */
export async function tryRefreshAccessToken(): Promise<boolean> {
  if (refreshPromise) return refreshPromise
  refreshPromise = (async () => {
    const refreshToken = getRefreshToken()
    if (!refreshToken) return false
    try {
      const res = await fetch(`${iamBase()}/auth/refresh`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refreshToken }),
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
  if (!getToken()) return false
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
