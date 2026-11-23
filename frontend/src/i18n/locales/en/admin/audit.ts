export default {
  audit: {
    title: 'Audit Logs',
    description: 'Records management-plane operations by admins and users. Header credentials keep only their first/last characters and request bodies are redacted. Entries cannot be deleted individually; clearing all requires two-factor verification.',
    clearAll: 'Clear All',
    empty: 'No audit logs yet',
    loadFailed: 'Failed to load audit logs',
    otherAction: 'Other operation',
    actionPattern: '{action} {resource}',
    resourceOperation: '{resource} operation',
    actionNames: {
      navigationRead: 'Mark navigation message as read', apiKeyUsage: 'View API key usage',
      login: 'Sign in', login2fa: 'Sign in with two-factor authentication', register: 'Register', tokenRefresh: 'Refresh session', sessionMismatch: 'Session binding mismatch',
      stepUp: 'Verify identity', clearLogs: 'Clear audit logs', mcpAuthorize: 'Authorize MCP', mcpRevoke: 'Revoke MCP authorization'
    },
    verbs: {
      create: 'Create', read: 'View', update: 'Update', delete: 'Delete', import: 'Import', export: 'Export',
      refresh: 'Refresh', restore: 'Restore', download: 'Download', regenerate: 'Regenerate', probe: 'Probe',
      batch_delete: 'Bulk delete', delete_preview: 'Preview deletion of', filter_delete: 'Delete filtered'
    },
    resources: {
      paymentOrders: 'payment order', paymentPlans: 'subscription plan', paymentProviders: 'payment provider',
      accountCredentials: 'account credentials', userApiKeys: 'user API keys', groupApiKeys: 'group API keys', backupStorage: 'backup storage settings', adminApiKey: 'admin API key',
      users: 'user', accounts: 'account', groups: 'group', apiKeys: 'API key', proxies: 'proxy',
      subscriptions: 'subscription', redeemCodes: 'redeem code', backups: 'backup', settings: 'system settings',
      payment: 'payment configuration', channels: 'channel', auditLogs: 'audit logs', monitoring: 'monitoring configuration',
      usage: 'usage records', announcements: 'announcement', navigation: 'navigation message', relogin: 'relogin settings',
      tickets: 'ticket', downstreams: 'downstream settings', receivables: 'receivable entry', accountVault: 'account details',
      upstreamBalances: 'upstream balance settings', workspace: 'workspace', promptAudit: 'prompt audit',
      codexTicket: 'Codex ticket', dataManagement: 'data management settings', authentication: 'authentication settings', profile: 'profile settings'
    },
    filters: {
      all: 'All',
      q: 'Keyword',
      qPlaceholder: 'Path / action / actor email',
      actorEmail: 'Actor Email',
      action: 'Action',
      clientIp: 'Client IP',
      method: 'Method',
      authMethod: 'Auth Method',
      result: 'Result',
      resultSuccess: 'Success',
      resultFailure: 'Failure',
      startTime: 'Start Time',
      endTime: 'End Time'
    },
    columns: {
      time: 'Time',
      actor: 'Actor',
      action: 'Action',
      method: 'Method',
      result: 'Result',
      clientIp: 'Client IP',
      detail: 'Detail'
    },
    detail: {
      title: 'Audit Log Detail',
      actorRole: 'Role',
      methodPath: 'Method / Path',
      latency: 'Latency',
      requestId: 'Request ID',
      credential: 'Credential (masked)',
      userAgent: 'User-Agent',
      requestBody: 'Request Body (redacted)',
      extra: 'Extra'
    },
    clearConfirm: {
      title: 'Clear All Audit Logs',
      message: 'This permanently deletes all audit logs and cannot be undone. The clear action itself is recorded. Continue?',
      totpTitle: 'Enter Two-Factor Code',
      totpHint: 'Clearing audit logs requires a fresh TOTP verification.',
      success: 'Cleared {count} audit log(s)',
      failed: 'Failed to clear audit logs'
    }
  }
}
