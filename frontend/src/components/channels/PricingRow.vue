<template>
  <div class="flex justify-between gap-2">
    <span class="text-gray-500 dark:text-gray-400">{{ label }}</span>
    <span class="font-mono">{{ display }}</span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { formatScaled } from '@/utils/pricing'
import { usdToCny } from '@/utils/displayCurrency'

const props = withDefaults(
  defineProps<{
    label: string
    value: number | null
    unit: string
    scale: number
    currency?: 'USD' | 'CNY'
  }>(),
  { value: null, currency: 'CNY' }
)

const display = computed(() => {
  if (props.value == null) return '-'
  const amount = props.currency === 'CNY' ? usdToCny(props.value) : props.value
  const price = formatScaled(amount, props.scale)
  return `${props.currency === 'CNY' ? price.replace(/^\$/, '¥') : price} ${props.unit}`
})
</script>
