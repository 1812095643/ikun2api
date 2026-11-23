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

export function auditActionLabel(action: string, t: (key: string, params?: Record<string, string>) => string): string {
  if (exactActions[action]) return t(`admin.audit.actionNames.${exactActions[action]}`)
  const parts = action.split('.')
  const verb = parts.pop() || ''
  if (parts[0] === 'admin') parts.shift()
  let resource = ''
  for (let length = parts.length; length > 0 && !resource; length--) {
    resource = resources[parts.slice(0, length).join('.')] || ''
  }
  if (!resource || !verbs.has(verb)) return t('admin.audit.otherAction')
  return t('admin.audit.actionPattern', {
    resource: t(`admin.audit.resources.${resource}`),
    action: t(`admin.audit.verbs.${verb}`)
  })
}
