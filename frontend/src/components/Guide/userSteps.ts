import type { DriveStep } from 'driver.js'

export interface UserTourStep extends DriveStep {
  route?: string
  action?: 'click' | 'group'
  keyForm?: boolean
  announcement?: boolean
  section?: string
}

interface PageGuide {
  path: string
  title: string
  description: string
  target?: string
}

// 只编排真实用户页面；支付、领奖和创建密钥均由用户自行确认。
const pageGuides: PageGuide[] = [
  { path: '/dashboard', title: '查看账户概况', description: '这里可以查看余额、请求量和消费趋势。下方提供最近调用记录，方便确认请求是否到达本站。' },
  { path: '/model-square', title: '先确定模型和分组', description: '搜索模型名称，按服务商和分组筛选。点击模型卡片查看支持分组与计价；创建密钥时请选择相应分组。', target: '.catalog-toolbar' },
  { path: '/purchase', title: '购买余额或订阅', description: '选择充值金额或订阅套餐，再按页面提供的支付方式付款。先核对套餐额度、有效期和适用分组；引导期间无需下单。' },
  { path: '/redeem', title: '使用兑换码', description: '在这里粘贴购买或领取的兑换码，核对后点击兑换。余额、订阅等兑换结果以页面提示和兑换记录为准；无需为完成引导兑换。', target: '#code' },
  { path: '/keys', title: '创建自己的 API 密钥', description: '每个密钥绑定一个分组，客户端通过它使用该分组的模型。下面请实际打开创建窗口，了解名称、服务商和分组的设置。', target: '[data-tour="keys-create-btn"]' },
  { path: '/usage', title: '查询每次调用和消费', description: '按时间、密钥和模型筛选记录，查看输入输出 Token、费用、耗时等详情。客户端报错时，先核对时间范围与对应记录，再联系管理员。' },
  { path: '/available-channels', title: '确认可用渠道', description: '这里列出当前账号可以使用的渠道和分组。先确认模型、计价和额度，再为客户端选择相应密钥。' },
  { path: '/monitor', title: '查看渠道状态', description: '查看渠道可用状态和监测情况。遇到调用异常时，可先检查对应渠道，再到用量记录核对具体请求。' },
  { path: '/subscriptions', title: '管理我的订阅', description: '这里查看已购买订阅、有效期和额度使用情况。订阅通常对应指定分组，密钥分组也需要与之对应。' },
  { path: '/orders', title: '查看购买记录', description: '在订单列表核对金额和支付状态。付款后未到账时，请保留订单号并通过工单联系管理员，避免反复支付。' },
  { path: '/affiliate', title: '邀请好友', description: '复制专属邀请链接或邀请码发给好友。这里可以查看邀请记录、返利和可用收益；转入余额前请先核对金额与规则。' },
  { path: '/checkin', title: '每日签到和盲盒', description: '点击签到可领取当天奖励，方块日历记录每次签到，统计展示累计天数、连续天数和总奖励。下方盲盒需满足当日签到与消费条件；参与前请阅读价格、奖池、概率和开奖时间。' },
  { path: '/lottery', title: '参与活动抽奖', description: '查看活动条件、奖项、人数与开奖时间，符合条件后再报名。参与记录和中奖记录可在这里查询，中奖结果经后端核对后发放。' },
  { path: '/tickets', title: '通过工单获得帮助', description: '点击新建工单描述问题，可附上时间、模型和报错截图。每人每天可新建一单，同一问题请在原工单继续回复；管理员来信也在这里查看，侧边栏红点提示未读消息。' },
  { path: '/news', title: '阅读时事新闻', description: '查看新闻列表，按来源或关键词寻找内容，点击新闻阅读详情。这里只浏览信息，不需要配置 API 密钥。' },
  { path: '/reset-monitor', title: '查看 FUCK TIBO 监测', description: '这里展示额度重置相关的监测信息。可切换页面提供的分类和刷新查看，具体状态以当前监测结果为准。' },
  { path: '/batch-image', title: '批量生图', description: '选择可用密钥和模型，填写提示词、数量等参数后再提交任务。任务会实际调用模型并产生消费，引导期间只需了解输入与结果区域。' },
  { path: '/profile', title: '维护个人资料和安全', description: '在这里查看账号信息，并按页面提供的功能维护密码、安全验证和账号绑定。修改安全信息前请核对当前登录账号。' },
]

const navSelector = (path: string) => `[data-tour-path="${path.replace(/["\\]/g, '\\$&')}"]`
const escapeHtml = (value: string) => value.replace(/[&<>"']/g, char => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char]!)

export function getUserSteps(t: (key: string) => string): UserTourStep[] {
  const steps: UserTourStep[] = [{
    popover: {
      title: '开始使用 IKUN2api',
      description: '接下来会带您进入实际页面，重点练习创建密钥和选择分组，再了解充值、用量与其他功能。请点击高亮入口继续，可随时关闭并从头像菜单重新开始。',
      nextBtnText: '开始操作',
    },
  }]

  const customPages: PageGuide[] = Array.from(document.querySelectorAll<HTMLElement>('[data-tour-path^="/custom/"]')).map(nav => ({
    path: nav.dataset.tourPath!,
    title: escapeHtml(nav.querySelector('.sidebar-label')?.textContent?.trim() || '自定义页面'),
    description: '这里展示管理员提供的扩展内容，请按照本页说明使用。页面后续更新时，可通过侧边栏提示重新查看。',
  }))
  for (const page of [...pageGuides, ...customPages]) {
    const nav = document.querySelector<HTMLElement>(navSelector(page.path))
    if (!nav) continue
    const label = nav.querySelector('.sidebar-label')?.textContent?.trim() || page.title
    steps.push({
      element: navSelector(page.path),
      route: page.path,
      action: 'click',
      section: label,
      popover: { title: `进入${escapeHtml(label)}`, description: `请点击高亮的「${escapeHtml(label)}」，进入页面后继续操作。`, side: 'right', showButtons: ['previous', 'close'] },
    }, {
      element: page.target || '[data-tour="user-page-content"]',
      route: page.path,
      section: label,
      popover: { title: page.title, description: page.description, side: 'bottom' },
    })

    if (page.path === '/keys') {
      steps.pop()
      steps.push(
        { element: '[data-tour="keys-create-btn"]', route: '/keys', action: 'click', section: label,
          popover: { title: '点击「创建密钥」', description: page.description, side: 'bottom', showButtons: ['previous', 'close'] } },
        { element: '[data-tour="key-form-name"]', route: '/keys', keyForm: true, section: label,
          popover: { title: '1. 为密钥命名', description: '填写便于区分用途的名称，例如「日常对话」或「代码助手」。这只是本站的备注名称，不是模型名称。填写后点击下一步。', side: 'bottom' } },
        { element: '[data-tour="key-form-provider"]', route: '/keys', keyForm: true, section: label,
          popover: { title: '2. 选择服务商', description: '请点击准备使用的服务商卡片。它会筛选下面的可选分组；不可选的卡片表示当前没有可用分组。选择后点击下一步。', side: 'bottom' } },
        { element: '[data-tour="key-form-group"]', route: '/keys', keyForm: true, action: 'group', section: label,
          popover: { title: '3. 选择一个分组', description: '请展开下拉框并点击一个可用分组。核对模型支持范围、计费倍率和订阅类型，选中后引导会继续。如果没有可用分组，可返回调整服务商或关闭引导联系管理员。', side: 'bottom', showButtons: ['previous', 'close'] } },
        { element: '[data-tour="key-form-submit"]', route: '/keys', keyForm: true, section: label,
          popover: { title: '4. 确认后创建', description: '名称和分组确认无误后，可点击表单的「创建」保存真实密钥。额度、有效期和 IP 限制按需要设置。若暂时不创建，点击「先了解导入」即可继续。', side: 'top', nextBtnText: '先了解导入' } },
        { element: '[data-tour="keys-connect-help"]', route: '/keys', section: label,
          popover: { title: '5. 将密钥接入客户端', description: '创建成功后，在列表复制自己的 API Key。客户端填写本站提供的 API 地址、API Key 和所选分组支持的模型；需要导入配置时，使用密钥行的配置或导入操作。不同客户端的地址格式请按新手教程设置，不要把密钥发给他人。', side: 'bottom' } },
      )
    }
  }

  if (document.querySelector('[data-tour="sidebar-announcements"]')) steps.push(
    { element: '[data-tour="sidebar-announcements"]', action: 'click', announcement: true,
      popover: { title: '打开公告', description: '请点击高亮的「公告」，打开消息列表。', side: 'right', showButtons: ['previous', 'close'] } },
    { element: '[data-tour="announcements-panel"]', announcement: true,
      popover: { title: '阅读通知和历史公告', description: '点击公告查看详情，读完后标记已读。未读内容会在侧边栏显示红点或数量；这里只介绍查看方法，不会代您标记全部已读。', side: 'bottom' } },
  )
  const extras = [
    { selector: '[data-tour="sidebar-image-studio"]', title: '打开生图工作室', description: '点击这里会在新标签页打开独立生图工作室。按工作室页面选择模型、填写提示词并查看作品；提交生成任务前请核对费用。' },
    { selector: '[data-tour="header-tutorial"]', title: '需要详细步骤时查看教程', description: '顶部「新手教程」会在新标签页打开完整文档，包含网站使用、购买兑换和客户端导入说明。以后也可以从头像菜单重新开始本次引导。' },
  ]
  for (const extra of extras) {
    if (document.querySelector(extra.selector)) steps.push({ element: extra.selector, popover: { title: extra.title, description: extra.description, side: 'bottom' } })
  }
  steps.push({ popover: { title: '可以开始使用了', description: '使用顺序是：确认模型与分组 → 准备余额或订阅 → 创建密钥 → 按教程接入客户端。调用后到用量查看记录，有问题在同一工单继续沟通。', nextBtnText: t('onboarding.finishTour') } })
  return steps
}
