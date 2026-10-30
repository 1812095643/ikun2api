<script setup lang="ts">
import { ref } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { formatTime, safeExternalURL, type ResetPost } from '@/api/workspace'
import tiboAvatar from '@/assets/workspace/thsottiaux.jpg'
defineProps<{ post: ResetPost }>()
const emit = defineEmits<{ context: [post: ResetPost] }>()
const original = ref(false)
const labels: Record<number, string> = { 10: '额度重置', 20: '重置预告', 30: '其他' }
</script>

<template>
  <article class="original-feed-postCard">
    <span class="original-feed-avatar"><img :src="tiboAvatar" alt="" width="40" height="40" /></span>
    <div class="original-feed-postMain">
      <header class="original-feed-postHeader">
        <div class="original-feed-identityLine">
          <strong class="original-feed-author">Tibo</strong><Icon name="check" size="xs" class="original-feed-verifiedBadge" />
          <span class="original-feed-handle">@thsottiaux</span><span class="original-feed-separator" aria-hidden="true">·</span>
          <time class="original-feed-time" :datetime="post.publishedAt">{{ formatTime(post.publishedAt) }}</time>
        </div>
        <a v-if="safeExternalURL(post.sourceUrl)" class="original-feed-sourceLink" :href="safeExternalURL(post.sourceUrl)" target="_blank" rel="noopener noreferrer" aria-label="在 X 查看原文"><Icon name="externalLink" size="xs" /></a>
      </header>
      <button v-if="post.isReply" class="reset-reply" @click="emit('context', post)">回复 @{{ post.replyingTo?.username || '对话' }}</button>
      <div class="original-feed-postContent">
        <p class="original-feed-postText">{{ original ? post.originalText : post.translatedText || post.originalText }}</p>
        <footer class="original-feed-postTools">
          <button v-if="post.translatedText" class="original-feed-languageToggle" :aria-pressed="original" @click="original = !original">{{ original ? '显示译文' : '显示原文' }}</button>
          <span class="original-feed-categoryLabel" :data-category="post.category"><Icon :name="post.category === 10 ? 'refresh' : post.category === 20 ? 'bell' : 'globe'" size="xs" />{{ labels[post.category] || '其他' }}</span>
          <button v-if="post.isReply" class="reset-context-button" @click="emit('context', post)">对话上下文</button>
        </footer>
      </div>
    </div>
  </article>
</template>

<style scoped>
.reset-reply { margin: 1px 0 5px; padding: 0; color: var(--twitter-blue); font-size: 12px; }
.reset-context-button { margin-left: auto; padding: 0; color: var(--text-tertiary); font-size: 11px; }
.reset-reply:focus-visible, .reset-context-button:focus-visible { outline: 2px solid var(--brand); outline-offset: 3px; }
</style>
