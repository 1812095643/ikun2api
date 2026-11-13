export function businessMoney(
  value: string | number | null | undefined
): string {
  if (value === null || value === undefined || !Number.isFinite(Number(value)))
    return '—'
  return (
    '$' +
    Number(value).toLocaleString('en-US', {
      minimumFractionDigits: 2,
      maximumFractionDigits: 8
    })
  )
}
export function positiveDecimal(value: string, places: number): boolean {
  return (
    new RegExp(`^\\d+(?:\\.\\d{1,${places}})?$`).test(value) &&
    Number.isFinite(Number(value)) &&
    Number(value) > 0 &&
    Number(value) <= 999999999999
  )
}

export function newBusinessOperationKey(): string {
  if (typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  bytes[6] = (bytes[6] & 15) | 64
  bytes[8] = (bytes[8] & 63) | 128
  const hex = Array.from(bytes, (value) =>
    value.toString(16).padStart(2, '0')
  ).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}
