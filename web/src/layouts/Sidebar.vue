<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { HomeFilled, List, Share, Link } from '@element-plus/icons-vue'

const route = useRoute()
const router = useRouter()
const collapsed = defineModel<boolean>('collapsed', { default: false })

const activeMenu = computed(() => {
  if (route.path.startsWith('/orders/')) return '/orders'
  return route.path
})
const logoText = computed(() => (collapsed.value ? 'OC' : '订单中心'))

function navigate(path: string) {
  router.push(path)
}
</script>

<template>
  <aside class="sidebar" :class="{ collapsed }">
    <div class="logo">{{ logoText }}</div>
    <el-menu
      :default-active="activeMenu"
      :collapse="collapsed"
      :default-openeds="['orders', 'fulfill']"
      background-color="#001529"
      text-color="#ffffffa6"
      active-text-color="#fff"
    >
      <el-menu-item index="/dashboard" @click="navigate('/dashboard')">
        <el-icon><HomeFilled /></el-icon><span>工作台</span>
      </el-menu-item>

      <el-sub-menu index="orders">
        <template #title><el-icon><List /></el-icon><span>订单</span></template>
        <el-menu-item index="/orders" @click="navigate('/orders')">订单列表</el-menu-item>
      </el-sub-menu>

      <el-sub-menu index="fulfill">
        <template #title><el-icon><Share /></el-icon><span>履约分配</span></template>
        <el-menu-item index="/allocate" @click="navigate('/allocate')">分配管理</el-menu-item>
        <el-menu-item index="/bindings" @click="navigate('/bindings')">
          <el-icon><Link /></el-icon><span>厂家绑定</span>
        </el-menu-item>
      </el-sub-menu>
    </el-menu>
  </aside>
</template>

<style scoped>
.sidebar {
  width: 220px;
  background: #001529;
  color: #fff;
  display: flex;
  flex-direction: column;
  transition: width 0.2s;
  flex-shrink: 0;
}
.sidebar.collapsed { width: 64px; }
.logo {
  height: 56px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 700;
  font-size: 16px;
  color: #fff;
  border-bottom: 1px solid #ffffff14;
  letter-spacing: 0.04em;
}
.sidebar :deep(.el-menu) { border-right: none; }
.sidebar :deep(.el-menu-item.is-active) { background: #1677ff33 !important; }
</style>
