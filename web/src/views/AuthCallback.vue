<script setup lang="ts">
import { onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { fetchSession } from '../api/session'
import { redirectToPortal, saveAuthTokens, startTokenKeepAlive, trustFreshToken } from '../utils/auth'

const route = useRoute()
const router = useRouter()

onMounted(async () => {
  const token = route.query.token as string | undefined
  if (!token) {
    redirectToPortal()
    return
  }
  const refresh = route.query.refresh as string | undefined
  saveAuthTokens(token, refresh)
  trustFreshToken()
  startTokenKeepAlive()
  await fetchSession()
  router.replace('/dashboard')
})
</script>

<template>
  <div class="auth-callback">正在登录…</div>
</template>

<style scoped>
.auth-callback {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #909399;
}
</style>
