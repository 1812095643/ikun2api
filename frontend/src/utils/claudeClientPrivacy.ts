// Claude's nonessential-traffic switch does not disable an explicitly enabled
// OpenTelemetry exporter. Keep both settings in every generated Claude config.
export const CLAUDE_PRIVACY_ENV = Object.freeze({
  CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: '1',
  CLAUDE_CODE_ENABLE_TELEMETRY: '0'
})

export function claudePrivacyShellSettings(shell: 'unix' | 'cmd' | 'powershell'): string {
  const prefix = { unix: 'export ', cmd: 'set ', powershell: '$env:' }[shell]
  return Object.entries(CLAUDE_PRIVACY_ENV)
    .map(([name, value]) => `${prefix}${name}=${value}`)
    .join('\n')
}
