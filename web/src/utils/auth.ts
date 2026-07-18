import { getPortalUrl } from './runtimeConfig'

const TOKEN_KEY = 'uc_access_token'

let sessionVerified = false

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

export function saveToken(token: string) {
  localStorage.setItem(TOKEN_KEY, token)
}

export function clearToken() {
  localStorage.removeItem(TOKEN_KEY)
  sessionVerified = false
  sessionStorage.removeItem('uc_session_profile')
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

export async function verifySession(): Promise<boolean> {
  const { fetchSession } = await import('../api/session')
  const info = await fetchSession()
  return info !== null
}

export async function ensureSession(): Promise<boolean> {
  if (!getToken()) return false
  if (sessionVerified) return true
  const ok = await verifySession()
  if (ok) sessionVerified = true
  return ok
}
