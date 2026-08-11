<script setup lang="ts">
import { onMounted } from 'vue'
import { clearToken, iamBase, redirectToPortal } from '../utils/auth'

onMounted(async () => {
  try {
    await fetch(`${iamBase()}/auth/logout`, {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({}),
    })
  } catch {
    // ignore
  }
  clearToken()
  if (window.self !== window.top) return
  redirectToPortal()
})
</script>

<template>
  <div class="auth-logout">正在退出…</div>
</template>

<style scoped>
.auth-logout {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #909399;
}
</style>
