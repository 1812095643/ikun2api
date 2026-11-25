import { createApp } from 'vue'
import './standalone.css'
import StandaloneMonitor from './StandaloneMonitor.vue'

async function start() {
  const u = new URL(location.href)
  const id = u.searchParams.get('monitor_incident') || ''
  const keep = /^inc-[a-f0-9]{64}$/.test(id) ? `?monitor_incident=${encodeURIComponent(id)}` : ''
  history.replaceState(null, '', u.pathname + keep)
  try {
    const theme = localStorage.getItem('theme')
    document.documentElement.classList.toggle('dark', theme === 'dark' || (!theme && matchMedia('(prefers-color-scheme: dark)').matches))
  } catch { /* Storage may be disabled. */ }
  const app = createApp(StandaloneMonitor)
  app.mount('#app')
}
void start()
