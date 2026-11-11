import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ModelIcon from '@/components/common/ModelIcon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import ProviderIcon from '@/components/user/monitor/ProviderIcon.vue'
import TypeSafeIcon from '@/components/common/TypeSafeIcon.vue'

describe('TypeSafe / Jev official icon', () => {
  it('uses the source-backed mark for the Jev model and provider surfaces', () => {
    for (const wrapper of [
      mount(TypeSafeIcon),
      mount(ModelIcon, { props: { model: 'jev-latest' } }),
      mount(PlatformIcon, { props: { platform: 'typesafe' } }),
      mount(ProviderIcon, { props: { provider: 'typesafe' } }),
    ]) {
      expect(wrapper.find('[data-brand="typesafe"] path').exists()).toBe(true)
      wrapper.unmount()
    }
  })
})
