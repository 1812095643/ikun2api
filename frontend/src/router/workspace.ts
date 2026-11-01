import type { RouteRecordRaw } from 'vue-router'

export const imageStudioUrl = '/image-studio/'

export const nativeMenuIDs = new Set([
  'tutorial',
  'tickets',
  'tickets-admin',
  'model-square',
  'image-studio',
  'news',
  'reset-monitor',
  'relogin'
])
const aliases: Record<string, string> = {
  tutorial: '/tutorial',
  tickets: '/tickets',
  'tickets-admin': '/admin/tickets',
  'model-square': '/model-square',
  news: '/news',
  'reset-monitor': '/reset-monitor',
  relogin: '/admin/relogin'
}
export const workspaceRoutes: RouteRecordRaw[] = [
  { path: '/checkin', name: 'WorkspaceCheckin', component: () => import('@/views/workspace/CheckinView.vue'), meta: { requiresAuth: true, title: '每日签到' } },
  { path: '/admin/checkin', name: 'WorkspaceCheckinAdmin', component: () => import('@/views/workspace/CheckinAdminView.vue'), meta: { requiresAuth: true, requiresAdmin: true, title: '签到管理' } },
  { path: '/lottery', name: 'WorkspaceLottery', component: () => import('@/views/workspace/LotteryView.vue'), meta: { requiresAuth: true, title: '幸运抽奖' } },
  { path: '/admin/lottery', name: 'WorkspaceLotteryAdmin', component: () => import('@/views/workspace/LotteryAdminView.vue'), meta: { requiresAuth: true, requiresAdmin: true, title: '抽奖管理' } },
  {
    path: '/tutorial/:article?',
    name: 'WorkspaceTutorial',
    component: () => import('@/views/workspace/TutorialView.vue'),
    meta: { requiresAuth: false, title: '新手教程' }
  },
  {
    path: '/tickets/:id?',
    name: 'WorkspaceTickets',
    component: () => import('@/views/workspace/TicketsView.vue'),
    meta: { requiresAuth: true, title: '我的工单' }
  },
  {
    path: '/admin/tickets/:id?',
    name: 'WorkspaceAdminTickets',
    component: () => import('@/views/workspace/TicketsView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, title: '工单管理' }
  },
  {
    path: '/model-square',
    name: 'WorkspaceModels',
    component: () => import('@/views/workspace/ModelSquareView.vue'),
    meta: { requiresAuth: true, title: '模型广场' }
  },
  {
    path: '/image-studio',
    alias: '/custom/image-studio',
    name: 'WorkspaceStudio',
    component: null,
    children: [],
    beforeEnter: () => {
      window.location.replace(imageStudioUrl)
      return false
    },
    meta: { requiresAuth: true, title: '生图工作室' }
  },
  {
    path: '/news',
    name: 'WorkspaceNews',
    component: () => import('@/views/workspace/NewsView.vue'),
    meta: { requiresAuth: true, title: '时事新闻' }
  },
  {
    path: '/reset-monitor',
    name: 'WorkspaceReset',
    component: () => import('@/views/workspace/ResetMonitorView.vue'),
    meta: { requiresAuth: true, title: 'FUCK TIBO' }
  },
  {
    path: '/admin/relogin',
    name: 'WorkspaceRelogin',
    component: () => import('@/views/workspace/ReloginView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, title: '失效重登' }
  },
  { path: '/relogin', redirect: '/admin/relogin' },
  ...Object.entries(aliases).map(([id, target]) => ({ path: `/custom/${id}`, redirect: target }))
]
