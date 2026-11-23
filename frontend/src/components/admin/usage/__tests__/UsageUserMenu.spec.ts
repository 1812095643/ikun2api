import { afterEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import UsageUserMenu from '../UsageUserMenu.vue'
import Select from '@/components/common/Select.vue'
import UserEmailMenu from '@/components/admin/user/UserEmailMenu.vue'
import { createI18n } from 'vue-i18n'

enableAutoUnmount(afterEach)
describe('使用记录用户菜单', () => {
  it('复用选择菜单并准确区分三种操作', async () => {
    const wrapper = mount(UsageUserMenu, { props: { email: 'owner@example.test' }, attachTo: document.body,
      global: { plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh: {} } })] } })
    expect(wrapper.get('button').text()).toContain('owner@example.test')
    await wrapper.get('button').trigger('click')
    expect(document.querySelector('[role="listbox"]')?.textContent).toContain('查看使用记录')
    expect(document.querySelector('[role="listbox"]')?.textContent).toContain('查看充值记录')
    expect(document.querySelector('[role="listbox"]')?.textContent).toContain('通知用户')
    for (const action of ['usage', 'balance', 'notify']) wrapper.getComponent(Select).vm.$emit('update:modelValue', action)
    expect(wrapper.emitted('select')).toEqual([['usage'], ['balance'], ['notify']])
  })
  it('用户列表复用同一个菜单，只显示两种日志入口', async () => {
    const wrapper = mount(UserEmailMenu, {
      props: { email: 'second@example.test', options: [
        { value: 'usage', label: '查看使用记录', icon: 'chart' },
        { value: 'errors', label: '查看错误日志', icon: 'exclamationTriangle' }
      ] },
      attachTo: document.body,
      global: { plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh: {} } })] }
    })
    await wrapper.get('button').trigger('click')
    const options = document.querySelectorAll('[role="option"]')
    expect(Array.from(options, option => option.textContent?.trim())).toEqual(['查看使用记录', '查看错误日志'])
    ;(options[1] as HTMLElement).click()
    expect(wrapper.emitted('select')).toEqual([['errors']])
    wrapper.getComponent(Select).vm.$emit('update:modelValue', 'balance')
    expect(wrapper.emitted('select')).toHaveLength(1)
  })
})
