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
        { path: 'allocate', name: 'Allocate', component: () => import('../views/order/AllocateList.vue'), meta: { title: '分配管理' } },
        { path: 'bindings', name: 'Bindings', component: () => import('../views/binding/BindingList.vue'), meta: { title: '厂家绑定' } },
        { path: 'settings', name: 'Settings', component: () => import('../views/settings/Settings.vue'), meta: { title: '同步与推送' } },
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
