import { describe, expect, it } from 'vitest'
import { buildPoolShareUrl, isPoolSharePagePath, resolvePoolShareToken } from '../poolShareLink'
import { buildEmbeddedUrl } from '../embedded-url'

const token = 'pool_ORIGINAL_READ_ONLY_CREDENTIAL'
const foreignJWT = 'eyJhbGciOiJIUzI1NiJ9.foreign.signature'

describe('portable pool share credentials', () => {
  it.each(['accounts', 'usage'] as const)('keeps the %s share valid when unmodified Sub2API overwrites token', tab => {
    const url = new URL(buildPoolShareUrl('https://share.example.test', token, tab))
    // Official buildEmbeddedUrl always replaces this parameter with its own JWT.
    url.searchParams.set('token', foreignJWT)
    url.searchParams.set('user_id', '999')
    url.searchParams.set('ui_mode', 'embedded')
    expect(url.hash).toBe('')
    expect(url.searchParams.get('tab')).toBe(tab)
    expect(resolvePoolShareToken(Object.fromEntries(url.searchParams), url.hash)).toBe(token)
  })

  it('does not treat a foreign login token as authorization', () => {
    expect(resolvePoolShareToken({ token: foreignJWT, ui_mode: 'embedded' }, '')).toBe('')
    expect(resolvePoolShareToken({ share_token: foreignJWT }, '')).toBe('')
  })

  it.each([null, '', ['pool_ONE', 'pool_TWO']])('fails closed for an explicit invalid share_token: %s', share_token => {
    expect(resolvePoolShareToken({ share_token, token }, '#token=pool_STALE')).toBe('')
  })

  it('keeps legacy fragment links working after a host injects its login token', () => {
    expect(resolvePoolShareToken({ token: foreignJWT, ui_mode: 'embedded' }, `#token=${token}`)).toBe(token)
    expect(resolvePoolShareToken({ token: foreignJWT }, `#token=${token}`)).toBe('')
    expect(resolvePoolShareToken({}, '#token=pool_ONE&token=pool_TWO')).toBe('')
  })

  it.each([
    `https://share.example.test/share/pool?token=${token}&tab=usage`,
    `https://share.example.test/share/pool#token=${token}&tab=usage`,
    `https://share.example.test/share/pool?share_token=${token}&tab=usage`,
  ])('preserves the original credential in our custom page without forwarding the host login: %s', original => {
    const url = new URL(buildEmbeddedUrl(original, 42, foreignJWT, 'dark', 'zh'))
    expect(url.searchParams.get('share_token')).toBe(token)
    expect(url.searchParams.has('token')).toBe(false)
    expect(url.searchParams.has('user_id')).toBe(false)
    expect(resolvePoolShareToken(Object.fromEntries(url.searchParams), url.hash)).toBe(token)
  })

  it('never repairs ambiguous or deliberately invalid links with another credential', () => {
    const url = new URL(buildEmbeddedUrl(`https://share.example.test/share/pool?share_token=&token=${token}`, 42, foreignJWT))
    expect(url.searchParams.get('share_token')).toBe('')
    expect(resolvePoolShareToken(Object.fromEntries(url.searchParams), url.hash)).toBe('')
  })

  it.each(['/admin', '/share/pool/extra', '/share%2fpool', '/share/p%6fol', '/share/pool%2f'])('does not use the share bootstrap for %s', path => {
    expect(isPoolSharePagePath(path)).toBe(false)
  })
})
