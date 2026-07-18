import { clearToken, getToken, resetSessionVerification, saveToken, type SessionInfo } from '../utils/auth'

const IAM_API =
  import.meta.env.VITE_IAM_API_URL
  || (import.meta.env.VITE_API_GATEWAY ? '/api/v1' : '/iam')

export type { SessionInfo }

export async function fetchSession(): Promise<SessionInfo | null> {
  const token = getToken()
  if (!token) return null
  try {
    const res = await fetch(`${IAM_API}/auth/me`, {
      headers: { Authorization: `Bearer ${token}` },
    })
    const body = await res.json()
    if (body.code !== 200 || !body.data) return null
    const info: SessionInfo = {
      user: body.data.user,
      tenant: body.data.tenant,
      tenants: body.data.tenants?.length ? body.data.tenants : [body.data.tenant],
    }
    saveSessionCache(info)
    return info
  } catch {
    return null
  }
}

const SESSION_CACHE_KEY = 'uc_session_profile'

export function saveSessionCache(info: SessionInfo) {
  sessionStorage.setItem(SESSION_CACHE_KEY, JSON.stringify(info))
}

export function loadSessionCache(): SessionInfo | null {
  const raw = sessionStorage.getItem(SESSION_CACHE_KEY)
  if (!raw) return null
  try {
    return JSON.parse(raw) as SessionInfo
  } catch {
    return null
  }
}

export function clearSessionCache() {
  sessionStorage.removeItem(SESSION_CACHE_KEY)
}

export function clearSession() {
  clearToken()
  clearSessionCache()
}

export async function switchTenant(tenantId: number): Promise<SessionInfo> {
  const token = getToken()
  if (!token) throw new Error('未登录')
  const res = await fetch(`${IAM_API}/auth/switch-tenant`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ tenantId }),
  })
  const body = await res.json()
  if (body.code !== 200 || !body.data?.accessToken) {
    throw new Error(body.message || '切换租户失败')
  }
  saveToken(body.data.accessToken)
  resetSessionVerification()
  const info: SessionInfo = {
    user: body.data.user,
    tenant: body.data.tenant,
    tenants: body.data.tenants?.length ? body.data.tenants : [body.data.tenant],
  }
  saveSessionCache(info)
  return info
}
