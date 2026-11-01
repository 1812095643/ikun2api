export type MenuVisibility = Record<string, boolean>
export interface MenuPage { path: string; zh: string; en: string }

const pages = (items: [string, string, string][]): MenuPage[] =>
  items.map(([path, zh, en]) => ({ path, zh, en }))

export const menuPageGroups = [
  { id: 'site', zh: '站点页面', en: 'Site pages', pages: pages([
    ['/home', '首页', 'Home'], ['/tutorial', '新手教程', 'Tutorial'],
  ]) },
  { id: 'user', zh: '用户端', en: 'User pages', pages: pages([
    ['/dashboard', '仪表盘', 'Dashboard'], ['/keys', 'API 密钥', 'API keys'],
    ['/usage', '用量记录', 'Usage'], ['/model-square', '模型广场', 'Model square'],
    ['/image-studio', '生图工作室', 'Image studio'], ['/batch-image', '批量生图', 'Batch images'],
    ['/news', '时事新闻', 'News'], ['/reset-monitor', 'FUCK TIBO', 'FUCK TIBO'],
    ['/tickets', '我的工单', 'My tickets'], ['/lottery', '幸运抽奖', 'Lottery'],
    ['/checkin', '每日签到', 'Daily check-in'], ['/announcements', '公告', 'Announcements'],
    ['/available-channels', '可用渠道', 'Available channels'], ['/monitor', '渠道状态', 'Channel status'],
    ['/subscriptions', '我的订阅', 'My subscriptions'], ['/purchase', '充值与订阅', 'Purchase'],
    ['/orders', '我的订单', 'My orders'], ['/redeem', '兑换', 'Redeem'],
    ['/affiliate', '邀请好友', 'Referrals'], ['/profile', '个人资料', 'Profile'],
  ]) },
  { id: 'admin', zh: '管理员端', en: 'Admin pages', pages: pages([
    ['/admin/dashboard', '仪表盘', 'Dashboard'], ['/admin/ops', '运行监控', 'Operations'],
    ['/admin/users', '用户管理', 'Users'], ['/admin/groups', '分组管理', 'Groups'],
    ['/admin/channels', '渠道管理', 'Channels'], ['/admin/channels/pricing', '渠道定价', 'Channel pricing'],
    ['/admin/channels/monitor', '渠道监控', 'Channel monitor'],
    ['/admin/subscriptions', '订阅管理', 'Subscriptions'], ['/admin/accounts', '账号管理', 'Accounts'],
    ['/admin/upstream-balances', '上游余额', 'Upstream balances'], ['/admin/relogin', '失效重登', 'Relogin'],
    ['/admin/tickets', '工单管理', 'Tickets'], ['/admin/lottery', '抽奖管理', 'Lottery'],
    ['/admin/checkin', '签到管理', 'Check-in'], ['/admin/plugins', '插件管理', 'Plugins'],
    ['/admin/announcements', '公告管理', 'Announcements'], ['/admin/proxies', 'IP 管理', 'Proxies'],
    ['/admin/security-audit', '安全审计', 'Security audit'], ['/admin/risk-control', '内容审核', 'Content moderation'],
    ['/admin/prompt-audit', '提示词审计', 'Prompt audit'], ['/admin/user-request-audit', '用户请求审计', 'Request audit'],
    ['/admin/redeem', '兑换码', 'Redeem codes'], ['/admin/promo-codes', '优惠码', 'Promo codes'],
    ['/admin/affiliates', '邀请管理', 'Referrals'], ['/admin/affiliates/invites', '邀请记录', 'Invites'],
    ['/admin/affiliates/rebates', '返利记录', 'Rebates'], ['/admin/affiliates/transfers', '转账记录', 'Transfers'],
    ['/admin/orders', '订单管理', 'Orders'], ['/admin/orders/dashboard', '支付概览', 'Payment overview'],
    ['/admin/orders/plans', '套餐管理', 'Plans'], ['/admin/usage', '用量记录', 'Usage'],
    ['/admin/audit-logs', '审计日志', 'Audit logs'], ['/admin/settings', '系统设置', 'System settings'],
  ]) },
] as const

export function isMenuVisible(path: string, visibility: MenuVisibility = {}): boolean {
  const normalized = path.split(/[?#]/, 1)[0].replace(/\/+$/, '') || '/'
  return visibility[normalized] !== false
}

export function filterVisibleMenus<T extends { path: string; children?: T[] }>(items: T[], visibility: MenuVisibility = {}): T[] {
  return items.flatMap(item => {
    if (!isMenuVisible(item.path, visibility)) return []
    if (!item.children) return [item]
    const children = filterVisibleMenus(item.children, visibility)
    return children.length ? [{ ...item, children }] : []
  })
}
