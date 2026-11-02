import { marked } from 'marked'
import DOMPurify from 'dompurify'

export function renderAnnouncement(content: string): string {
  return DOMPurify.sanitize(marked.parse(content || '', { breaks: true, gfm: true }) as string)
}
export function announcementPreview(content: string): string {
  const fragment = DOMPurify.sanitize(marked.parse(content || '', { breaks: true, gfm: true }) as string, { RETURN_DOM: true })
  return (fragment.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 180)
}
