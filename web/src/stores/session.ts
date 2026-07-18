import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import {
  clearSession, fetchSession, loadSessionCache, switchTenant, type SessionInfo,
} from '../api/session'

export const useSessionStore = defineStore('session', () => {
  const session = ref<SessionInfo | null>(loadSessionCache())

  const displayLabel = computed(() => {
    if (!session.value) return ''
    return `${session.value.user.displayName} · ${session.value.tenant.name}`
  })

  async function load(force = false) {
    if (!force && session.value) return session.value
    const info = await fetchSession()
    if (info) session.value = info
    return info
  }

  function clear() {
    clearSession()
    session.value = null
  }

  async function switchToTenant(tenantId: number) {
    const info = await switchTenant(tenantId)
    session.value = info
    window.location.reload()
    return info
  }

  return { session, displayLabel, load, clear, switchToTenant }
})
