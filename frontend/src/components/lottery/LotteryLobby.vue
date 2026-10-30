<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'
import WorkspaceDialog from '@/components/workspace/WorkspaceDialog.vue'
import LotteryActivityCard from './LotteryActivityCard.vue'
import LotteryWinRecords from './LotteryWinRecords.vue'
import LotteryShareDialog from './LotteryShareDialog.vue'
import LotteryDetail from './LotteryDetail.vue'
import LotteryHistory from './LotteryHistory.vue'
import { useLottery } from '@/composables/workspace/useLottery'
import { lotteryMoney } from '@/api/lottery'
const {
  items,
  wins,
  total,
  winTotal,
  winAmount,
  page,
  winPage,
  tab,
  historyRevision,
  filter,
  loading,
  busy,
  error,
  feedback,
  detail,
  share,
  now,
  joinedID,
  pending,
  load,
  join,
  claim,
  openDetail,
  openShare,
  changeFilter
} = useLottery()

function changePage(delta: number, history = false) {
  if (history) winPage.value += delta
  else page.value += delta
  void load()
}
</script>
<template>
  <section class="lottery-area">
    <div class="lottery-topbar">
      <nav class="lottery-tabs" aria-label="抽奖页面">
        <button :class="{ active: tab === 'activities' }" @click="tab = 'activities'">
          抽奖大厅</button
        ><button :class="{ active: tab === 'participations' }" @click="tab = 'participations'">
          我的参与
        </button>
        <button :class="{ active: tab === 'wins' }" @click="tab = 'wins'">
          中奖记录 <span v-if="winTotal">{{ winTotal }}</span
          ><i v-if="pending" aria-label="有待领取奖金" />
        </button>
      </nav>
      <button class="lottery-icon-button" aria-label="刷新活动" :disabled="loading" @click="load()">
        <Icon name="refresh" size="sm" />
      </button>
    </div>
    <div v-if="error" class="lottery-error" role="alert">
      {{ error }}<button @click="load()">重试</button>
    </div>
    <Transition name="lottery-feedback"
      ><div v-if="feedback" class="lottery-floating-feedback" role="status">
        <Icon name="checkCircle" size="sm" />{{ feedback }}
      </div></Transition
    >
    <template v-if="tab === 'activities'">
      <div class="lottery-filters">
        <button
          v-for="option in [
            { value: 'open', label: '正在报名' },
            { value: 'drawn', label: '往期活动' }
          ]"
          :key="option.value"
          :class="{ active: filter === option.value }"
          @click="changeFilter(option.value)"
        >
          {{ option.label }}</button
        ><small>{{ total }} 场活动</small>
      </div>
      <div v-if="loading && !items.length" class="lottery-empty" role="status">
        正在查看最新活动…
      </div>
      <div v-else-if="!items.length" class="lottery-empty">
        <p>{{ filter === 'open' ? '暂无进行中的活动' : '暂无往期活动' }}</p>
      </div>
      <div v-else class="lottery-activity-grid">
        <LotteryActivityCard
          v-for="item in items"
          :key="item.id"
          :item="item"
          :now="now"
          :busy="busy === item.id"
          :confirmed="joinedID === item.id"
          @join="join(item)"
          @detail="openDetail(item.id)"
        />
      </div>
      <div v-if="total > 12" class="lottery-pagination">
        <button
          class="btn btn-secondary lottery-button"
          :disabled="page <= 1"
          @click="changePage(-1)"
        >
          上一页</button
        ><span>{{ page }} / {{ Math.ceil(total / 12) }}</span
        ><button
          class="btn btn-secondary lottery-button"
          :disabled="page * 12 >= total"
          @click="changePage(1)"
        >
          下一页
        </button>
      </div>
    </template>
    <LotteryHistory
      v-else-if="tab === 'participations'"
      :revision="historyRevision"
      :busy="busy"
      @detail="openDetail"
      @claim="claim"
      @share="openShare"
    />
    <template v-else
      ><div class="lottery-my-summary">
        <p>
          累计 <strong>{{ winTotal }}</strong> 次好运
        </p>
        <p>
          中奖总额 <strong>${{ lotteryMoney(winAmount) }}</strong>
        </p>
      </div>
      <LotteryWinRecords
        :items="wins"
        :busy="busy"
        @claim="claim"
        @share="openShare"
        @detail="openDetail"
      />
      <div v-if="winTotal > 20" class="lottery-pagination">
        <button
          class="btn btn-secondary lottery-button"
          :disabled="winPage <= 1"
          @click="changePage(-1, true)"
        >
          上一页</button
        ><span>{{ winPage }} / {{ Math.ceil(winTotal / 20) }}</span
        ><button
          class="btn btn-secondary lottery-button"
          :disabled="winPage * 20 >= winTotal"
          @click="changePage(1, true)"
        >
          下一页
        </button>
      </div></template
    >
    <WorkspaceDialog
      :open="!!detail"
      :title="detail?.spec.title || '活动详情'"
      wide
      @update:open="detail = null"
      ><LotteryDetail
        v-if="detail"
        :item="detail"
        :busy="busy !== null"
        @claim="claim"
        @share="openShare"
    /></WorkspaceDialog>
    <LotteryShareDialog :win="share" @close="share = null" />
  </section>
</template>
