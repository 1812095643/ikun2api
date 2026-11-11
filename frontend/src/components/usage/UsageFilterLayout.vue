<template>
  <div>
    <div class="space-y-4">
      <div class="usage-filter-fields" data-testid="usage-filter-fields">
        <slot />
      </div>
      <div v-if="$slots.actions" class="usage-filter-actions" data-testid="usage-filter-actions">
        <slot name="actions" />
      </div>
    </div>
  </div>
</template>

<style scoped>
.usage-filter-fields {
  display: flex;
  flex-wrap: wrap;
  align-items: end;
  justify-content: space-between;
  gap: 1rem;
}
.usage-filter-fields > :slotted(.usage-filter-field) {
  min-width: 0;
  flex: 1 1 13rem;
}
.usage-filter-actions { display: flex; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: .5rem; }
.usage-filter-fields:empty {
  display: none;
}
</style>
