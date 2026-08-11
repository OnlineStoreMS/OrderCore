import { createRouter, createWebHistory } from 'vue-router'
import AdminLayout from '../layouts/AdminLayout.vue'
import { getToken, redirectToPortal, ensureSession, clearToken } from '../utils/auth'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/auth/callback',
      name: 'AuthCallback',
      component: () => import('../views/AuthCallback.vue'),
      meta: { public: true },
    },
    {
      path: '/auth/logout',
      name: 'AuthLogout',
      component: () => import('../views/AuthLogout.vue'),
      meta: { public: true },
    },
    {
      path: '/',
      component: AdminLayout,
      redirect: '/dashboard',
      children: [
        { path: 'dashboard', name: 'Dashboard', component: () => import('../views/Dashboard.vue'), meta: { title: '工作台' } },
        { path: 'orders', name: 'Orders', component: () => import('../views/order/OrderList.vue'), meta: { title: '订单列表', section: '订单' } },
        { path: 'orders/:id', name: 'OrderDetail', component: () => import('../views/order/OrderDetail.vue'), meta: { title: '订单详情', section: '订单' } },
        { path: 'allocate', name: 'Allocate', component: () => import('../views/order/AllocateList.vue'), meta: { title: '分配列表', section: '分配管理' } },
        { path: 'allocate/settings', name: 'AllocSettings', component: () => import('../views/allocate/AllocSettings.vue'), meta: { title: '分配设置', section: '分配管理' } },
        { path: 'bindings', name: 'Bindings', component: () => import('../views/binding/BindingList.vue'), meta: { title: '厂家绑定' } },
        { path: 'settings', redirect: '/settings/sync' },
        { path: 'settings/sync', name: 'SyncSettings', component: () => import('../views/settings/SyncSettings.vue'), meta: { title: '同步设置' } },
        { path: 'settings/push', name: 'PushSettings', component: () => import('../views/settings/PushSettings.vue'), meta: { title: '推送设置' } },
      ],
    },
  ],
})

router.beforeEach(async (to) => {
  if (to.meta.public) return true
  if (!getToken()) {
    redirectToPortal()
    return false
  }
  const ok = await ensureSession()
  if (!ok) {
    clearToken()
    redirectToPortal()
    return false
  }
  return true
})

export default router
