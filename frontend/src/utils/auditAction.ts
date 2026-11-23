const exactActions: Record<string, string> = {
  'workspace.navigation.read.create': 'navigationRead',
  'usage.dashboard.api_keys_usage.create': 'apiKeyUsage',
  'auth.login': 'login', 'auth.login_2fa': 'login2fa', 'auth.register': 'register',
  'auth.login.2fa': 'login2fa', 'auth.token.refresh': 'tokenRefresh', 'auth.step_up.verify': 'stepUp',
  'auth.session_binding.mismatch': 'sessionMismatch', 'admin.audit_log.clear': 'clearLogs',
  'auth.token_refresh': 'tokenRefresh', 'auth.step_up_verify': 'stepUp',
  'admin.audit_logs.clear': 'clearLogs',
  'user.mcp.authorize': 'mcpAuthorize', 'user.mcp.revoke': 'mcpRevoke'
}

const resources: Record<string, string> = {
  'payment.orders': 'paymentOrders', 'payment.plans': 'paymentPlans', 'payment.providers': 'paymentProviders',
  'accounts.credentials': 'accountCredentials', 'users.api_keys': 'userApiKeys', 'groups.api_keys': 'groupApiKeys',
  'backups.s3_config': 'backupStorage', 'data_management.s3_config': 'backupStorage', admin_api_key: 'adminApiKey',
  users: 'users', accounts: 'accounts', groups: 'groups', api_keys: 'apiKeys', keys: 'apiKeys',
  proxies: 'proxies', subscriptions: 'subscriptions', redeem_codes: 'redeemCodes',
  backups: 'backups', settings: 'settings', payment: 'payment', channels: 'channels',
  audit_logs: 'auditLogs', ops: 'monitoring', usage: 'usage', announcements: 'announcements',
  'workspace.navigation': 'navigation', 'workspace.relogin': 'relogin',
  'workspace.tickets': 'tickets', 'workspace.downstreams': 'downstreams',
  'workspace.receivables': 'receivables', 'workspace.account_vault': 'accountVault',
  'workspace.upstream_balances': 'upstreamBalances', 'workspace': 'workspace',
  prompt_audit: 'promptAudit', codex_ticket: 'codexTicket', data_management: 'dataManagement',
  auth: 'authentication', user: 'profile'
}
const verbs = new Set(['create', 'read', 'update', 'delete', 'import', 'export', 'refresh', 'restore', 'download', 'regenerate', 'probe', 'batch_delete', 'delete_preview', 'filter_delete'])
const endpointVerbs: Record<string, string> = { apply: 'update', sync: 'refresh', test: 'probe', probe: 'probe', refresh: 'refresh', restore: 'restore', regenerate: 'regenerate', batch_delete: 'batch_delete', batch_update: 'update', bulk_update: 'update', export: 'export', import: 'import' }

export function auditActionLabel(action: string, t: (key: string, params?: Record<string, string>) => string): string {
  if (exactActions[action]) return t(`admin.audit.actionNames.${exactActions[action]}`)
  const parts = action.split('.')
  let verb = parts.pop() || ''
  if (parts[0] === 'admin') parts.shift()
  // POST 还可能执行查询或批量操作，按端点语义翻译，避免将它们误标为创建实体。
  if (verb === 'create' && endpointVerbs[parts[parts.length - 1] || '']) verb = endpointVerbs[parts.pop()!]
  let resource = ''
  let matchedParts = 0
  for (let length = parts.length; length > 0 && !resource; length--) {
    resource = resources[parts.slice(0, length).join('.')] || ''
    if (resource) matchedParts = length
  }
  if (!resource || !verbs.has(verb)) return t('admin.audit.otherAction')
  if (parts.length > matchedParts && verb === 'create') return t('admin.audit.resourceOperation', { resource: t(`admin.audit.resources.${resource}`) })
  return t('admin.audit.actionPattern', {
    resource: t(`admin.audit.resources.${resource}`),
    action: t(`admin.audit.verbs.${verb}`)
  })
}
