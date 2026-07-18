<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useSessionStore } from '../stores/session'

const sessionStore = useSessionStore()
const switching = ref(false)

const show = computed(() => {
  const session = sessionStore.session
  return session?.user.isPlatform && (session.tenants?.length ?? 0) >= 1
})

async function onChange(tenantId: number) {
  if (tenantId === sessionStore.session?.tenant.id) return
  switching.value = true
  try {
    await sessionStore.switchToTenant(tenantId)
    ElMessage.success('已切换租户')
  } catch (e) {
    ElMessage.error((e as Error).message)
    switching.value = false
  }
}
</script>

<template>
  <el-select
    v-if="show && sessionStore.session"
    :model-value="sessionStore.session.tenant.id"
    :loading="switching"
    size="small"
    placeholder="切换租户"
    style="width: 168px"
    @change="onChange"
  >
    <el-option
      v-for="t in sessionStore.session.tenants"
      :key="t.id"
      :label="t.name"
      :value="t.id"
    />
  </el-select>
</template>
