<script setup lang="ts">
import { onMounted } from 'vue'
import { clearSession } from '../api/session'
import { portalLoginUrl } from '../utils/auth'

onMounted(() => {
  clearSession()
  window.location.href = portalLoginUrl()
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
