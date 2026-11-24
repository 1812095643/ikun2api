import { beforeEach, describe, expect, it, vi } from 'vitest'

const { post, get } = vi.hoisted(() => ({ post: vi.fn(), get: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { post, get } }))

import { userRequestAuditAPI } from '@/api/admin/userRequestAudit'

describe('user request audit export API', () => {
  beforeEach(() => {
    post.mockReset()
    get.mockReset()
    post.mockResolvedValue({ data: { id: 'job-1', status: 'queued' } })
  })

  it('keeps active filters when a selection is exported', async () => {
    await userRequestAuditAPI.export(
      { page: 1, page_size: 20, requested_model: 'claude-opus-4-1', group_id: 4, start_time: '2026-10-14T00:00:00.000Z' },
      'jsonl',
      [11, 12]
    )

    expect(post).toHaveBeenCalledWith('/admin/user-request-audit/export', {
      format: 'jsonl',
      requested_model: 'claude-opus-4-1',
      group_id: 4,
      start_time: '2026-10-14T00:00:00.000Z',
      conversation_ids: [11, 12]
    })
  })

  it('exports the filtered set without ids when nothing is selected', async () => {
    await userRequestAuditAPI.export({ page: 2, page_size: 50, requested_model: 'gpt-5.1-codex' }, 'json')

    expect(post).toHaveBeenCalledWith('/admin/user-request-audit/export', {
      format: 'json',
      requested_model: 'gpt-5.1-codex'
    })
  })
})

describe('user request audit export download filenames', () => {
  const job = { id: 'job-1', status: 'completed', format: 'jsonl', created_at: '', rows: 1 } as const

  async function downloadWithDisposition(disposition?: string) {
    const blob = new Blob(['export data'])
    get.mockResolvedValue({ status: 200, data: blob, headers: { 'content-disposition': disposition } })
    const result = await userRequestAuditAPI.downloadExport(job)
    expect(result.blob).toBe(blob)
    return result.filename
  }

  it('sanitizes every C0 control character in decoded download filenames', async () => {
    const controls = Array.from({ length: 32 }, (_, code) => String.fromCharCode(code)).join('')
    const filename = `audit${controls}.jsonl.gz`
    expect(await downloadWithDisposition(`attachment; filename*=UTF-8''${encodeURIComponent(filename)}`))
      .toBe(`audit${'_'.repeat(32)}.jsonl.gz`)
  })

  it('removes decoded filesystem separators and reserved filename characters', async () => {
    const filename = String.raw`audit\/:*?"<>|.jsonl.gz`
    expect(await downloadWithDisposition(`attachment; filename*=UTF-8''${encodeURIComponent(filename)}`))
      .toBe('audit_________.jsonl.gz')
  })

  it('preserves valid UTF-8 names and the archive extension', async () => {
    expect(await downloadWithDisposition(`attachment; filename*=UTF-8''${encodeURIComponent('请求 审计.jsonl.gz')}`))
      .toBe('请求 审计.jsonl.gz')
    expect(await downloadWithDisposition('attachment; filename="audit.jsonl.gz"')).toBe('audit.jsonl.gz')
  })

  it.each([undefined, 'attachment', 'attachment; filename=""', "attachment; filename*=UTF-8''%ZZ"])(
    'falls back for a missing or invalid filename: %s', async disposition => {
      expect(await downloadWithDisposition(disposition)).toBe('user-request-audit.jsonl.gz')
    }
  )
})
