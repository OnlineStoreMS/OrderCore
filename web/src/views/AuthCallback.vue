<script setup lang="ts">
import { onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { fetchSession } from '../api/session'
import {
  exchangeSsoCode,
  redirectToPortal,
  saveAuthTokens,
  startTokenKeepAlive,
  trustFreshToken,
} from '../utils/auth'

const route = useRoute()
const router = useRouter()

onMounted(async () => {
  const code = route.query.code as string | undefined
  if (code) {
    try {
      const tokens = await exchangeSsoCode(code)
      saveAuthTokens(tokens.accessToken, tokens.refreshToken, tokens.expiresAt)
      trustFreshToken()
      startTokenKeepAlive()
      await fetchSession()
      router.replace('/dashboard')
      return
    } catch {
      redirectToPortal()
      return
    }
  }
  // Cookie SSO：共享父域直达时无 code，凭 cookie 拉会话
  const info = await fetchSession()
  if (!info) {
    redirectToPortal()
    return
  }
  trustFreshToken()
  startTokenKeepAlive()
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
