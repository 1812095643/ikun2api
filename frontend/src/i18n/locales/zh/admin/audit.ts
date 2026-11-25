export default {
  audit: {
    title: '操作日志',
    description: '记录管理员与用户的管理面操作，请求头凭证仅保留首尾、请求体已脱敏。日志无法单条删除，全量清理需二次验证。',
    clearAll: '全部清理',
    empty: '暂无操作日志',
    loadFailed: '加载操作日志失败',
    otherAction: '其他操作',
    actionPattern: '{action}{resource}',
    resourceOperation: '{resource}操作',
    actionNames: {
      controlIncident: '处理异常事件', configureMonitor: '配置监控规则', saveNotificationChannel: '保存通知渠道', testNotificationChannel: '测试通知渠道',
      viewAccountTimer: '查看定时启停计划', previewAccountTimer: '预览定时启停计划', saveAccountTimer: '保存定时启停计划', cancelAccountTimer: '取消定时启停计划', viewConnections: '查看连接诊断',
      navigationRead: '标记导航消息已读', apiKeyUsage: '查看 API Key 用量',
      login: '登录', login2fa: '两步验证登录', register: '注册', tokenRefresh: '刷新登录凭据', sessionMismatch: '登录会话校验不匹配',
      stepUp: '二次验证', clearLogs: '清理操作日志', mcpAuthorize: '授权 MCP', mcpRevoke: '撤销 MCP 授权'
    },
    verbs: {
      create: '创建', read: '查看', update: '更新', delete: '删除', import: '导入', export: '导出',
      refresh: '刷新', restore: '恢复', download: '下载', regenerate: '重新生成', probe: '检测',
      batch_delete: '批量删除', delete_preview: '预览待删除的', filter_delete: '按条件删除'
    },
    resources: {
      accountTimers: '定时启停计划', connectionDiagnostics: '连接诊断',
      paymentOrders: '支付订单', paymentPlans: '订阅套餐', paymentProviders: '支付服务商',
      accountCredentials: '账号凭据', userApiKeys: '用户 API Key', groupApiKeys: '分组 API Key', backupStorage: '备份存储配置', adminApiKey: '管理员 API Key',
      users: '用户', accounts: '账号', groups: '分组', apiKeys: 'API Key', proxies: '代理',
      subscriptions: '订阅', redeemCodes: '兑换码', backups: '备份', settings: '系统设置',
      payment: '支付配置', channels: '渠道', auditLogs: '操作日志', monitoring: '监控配置',
      usage: '使用记录', announcements: '公告', navigation: '导航消息', relogin: '失效重登设置',
      tickets: '工单', downstreams: '下游设置', receivables: '记账记录', accountVault: '账号资料',
      upstreamBalances: '上游余额设置', workspace: '工作台', promptAudit: '提示词审计',
      codexTicket: 'Codex 票据', dataManagement: '数据管理设置', authentication: '登录设置', profile: '个人设置'
    },
    filters: {
      all: '全部',
      q: '关键字',
      qPlaceholder: '路径 / 动作 / 操作者邮箱',
      actorEmail: '操作者邮箱',
      action: '动作',
      clientIp: '客户端 IP',
      method: '请求方法',
      authMethod: '认证方式',
      result: '结果',
      resultSuccess: '成功',
      resultFailure: '失败',
      startTime: '开始时间',
      endTime: '结束时间'
    },
    columns: {
      time: '时间',
      actor: '操作者',
      action: '动作',
      method: '方法',
      result: '结果',
      clientIp: '客户端 IP',
      detail: '详情'
    },
    detail: {
      title: '操作日志详情',
      actorRole: '角色',
      methodPath: '方法 / 路径',
      latency: '耗时',
      requestId: '请求 ID',
      credential: '凭证（掩码）',
      userAgent: 'User-Agent',
      requestBody: '请求体（已脱敏）',
      extra: '附加信息'
    },
    clearConfirm: {
      title: '清理全部操作日志',
      message: '此操作将永久删除所有操作日志，且不可恢复。清理动作本身会被留痕记录。确定继续吗？',
      totpTitle: '输入二次验证码',
      totpHint: '清理操作日志需要现场验证 TOTP 验证码。',
      success: '已清理 {count} 条操作日志',
      failed: '清理操作日志失败'
    }
  }
}
