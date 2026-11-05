<template>
  <AppLayout>
    <div class="mx-auto max-w-2xl space-y-5">
      <div class="card space-y-4 p-5">
        <div class="flex items-start justify-between gap-3">
          <div>
            <h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('payment.manual.title') }}</h1>
            <p v-if="details" class="mt-1 text-sm text-gray-500">{{ t('payment.orders.orderId') }} #{{ details.order.id }}</p>
          </div>
          <div class="flex items-center gap-2">
            <OrderStatusBadge v-if="details" :status="details.order.status" />
            <button data-testid="manual-refresh" class="btn btn-secondary btn-sm" :disabled="refreshing || submitting" :aria-label="t('common.refresh')" @click="refreshDetails">
              <Icon name="refresh" size="sm" :class="refreshing ? 'animate-spin' : ''" />
              <span class="sr-only">{{ t('common.refresh') }}</span>
            </button>
          </div>
        </div>

        <p v-if="details?.order.status === 'PENDING_REVIEW'" class="rounded-lg bg-blue-50 p-3 text-sm text-blue-800 dark:bg-blue-900/20 dark:text-blue-200">{{ t('payment.manual.awaitingReview') }}</p>
        <p v-if="details?.order.status === 'REJECTED'" class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ details.review_note || t('payment.manual.rejected') }}</p>
        <p v-if="details?.order.status === 'EXPIRED'" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-200">{{ t('payment.manual.expiredOriginalQuoteWarning') }}</p>

        <div v-if="details" class="grid gap-4 sm:grid-cols-2">
          <div><p class="text-xs text-gray-500">{{ t('payment.orders.payAmount') }}</p><p class="text-lg font-semibold">¥{{ details.order.pay_amount.toFixed(2) }} CNY</p></div>
          <div><p class="text-xs text-gray-500">{{ t('payment.orders.creditedAmount') }}</p><p class="text-lg font-semibold">${{ details.order.amount.toFixed(2) }} USD</p></div>
          <div class="sm:col-span-2"><p class="text-xs text-gray-500">{{ t('payment.manual.payee') }}</p><p class="text-sm font-medium">{{ details.payee_name }}</p></div>
        </div>

        <div v-if="canDisplayQr" class="space-y-3">
          <img :src="qrImageSrc" :alt="t('payment.manual.qrAlt')" data-testid="manual-qr-image" class="mx-auto max-h-[70vh] max-w-full rounded-lg border border-gray-200 object-contain dark:border-dark-600" />
          <div class="flex flex-wrap items-center justify-center gap-3">
            <button type="button" data-testid="manual-qr-enlarge" class="btn btn-secondary btn-sm" aria-haspopup="dialog" @click="qrDialogOpen = true">
              <Icon name="eye" size="sm" class="mr-1.5" />{{ t('payment.manual.enlargeQr') }}
            </button>
            <a :href="qrImageSrc" :download="qrDownloadName" data-testid="manual-qr-save" class="btn btn-secondary btn-sm" :aria-label="t('payment.manual.saveQr')">
              <Icon name="download" size="sm" class="mr-1.5" />{{ t('payment.manual.saveQr') }}
            </a>
          </div>
        </div>
        <p v-if="details?.instructions" class="whitespace-pre-wrap text-sm text-gray-600 dark:text-gray-300">{{ details.instructions }}</p>

        <div v-if="details && canSubmit" class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-600">
          <p class="text-sm text-gray-600 dark:text-gray-300">{{ t('payment.manual.reportInstructions') }}</p>
          <label class="block"><span class="input-label">{{ t('payment.manual.transactionNo') }}</span><input v-model="transactionNo" data-testid="manual-transaction-no" class="input mt-1 w-full" maxlength="128" /></label>
          <label class="block"><span class="input-label">{{ t('payment.manual.note') }}</span><textarea v-model="note" data-testid="manual-note" rows="3" maxlength="500" class="input mt-1 w-full" /></label>
          <button data-testid="manual-submit" class="btn btn-primary w-full" :disabled="submitting || refreshing" @click="submitReport">{{ submitting ? t('common.processing') : t('payment.manual.submit') }}</button>
        </div>
        <p v-if="details?.submission?.transaction_no" class="text-sm text-gray-600 dark:text-gray-300">{{ t('payment.manual.submittedReference') }}: {{ details.submission.transaction_no }}</p>
        <p v-if="details?.order.status === 'COMPLETED'" class="text-sm text-green-700 dark:text-green-300">{{ t('payment.status.completed') }}</p>
      </div>
    </div>
    <BaseDialog :show="qrDialogOpen && canDisplayQr" :title="t('payment.manual.viewOriginalQr')" width="wide" @close="qrDialogOpen = false">
      <div class="flex justify-center">
        <img :src="qrImageSrc" :alt="t('payment.manual.qrAlt')" data-testid="manual-qr-original" class="max-h-[75vh] max-w-full w-auto object-contain" />
      </div>
      <template #footer>
        <div class="flex justify-end gap-2">
          <a :href="qrImageSrc" :download="qrDownloadName" data-testid="manual-qr-original-save" class="btn btn-primary">
            <Icon name="download" size="sm" class="mr-1.5" />{{ t('payment.manual.saveQr') }}
          </a>
          <button type="button" class="btn btn-secondary" @click="qrDialogOpen = false">{{ t('common.close') }}</button>
        </div>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { paymentAPI } from '@/api/payment'
import type { ManualOrderDetails } from '@/types/payment'
import { useAppStore, useAuthStore } from '@/stores'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { isValidManualQrImageValue } from '@/components/payment/providerConfig'
import AppLayout from '@/components/layout/AppLayout.vue'
import OrderStatusBadge from '@/components/payment/OrderStatusBadge.vue'
import Icon from '@/components/icons/Icon.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'

const STATUS_POLL_INTERVAL_MS = 5_000
const MAX_STATUS_POLLS = 120
const POLLABLE_STATUSES = new Set(['PENDING', 'PENDING_REVIEW', 'PAID', 'RECHARGING'])

const { t } = useI18n()
const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()
const details = ref<ManualOrderDetails | null>(null)
const transactionNo = ref('')
const note = ref('')
const submitting = ref(false)
const refreshing = ref(false)
const qrDialogOpen = ref(false)
const id = computed(() => Number(route.params.id))
const qrImageSrc = computed(() => {
  const value = details.value?.qr_code_img || ''
  return isValidManualQrImageValue(value) ? value : ''
})
const canDisplayQr = computed(() => details.value?.order.status === 'PENDING' && !!qrImageSrc.value)
const qrDownloadName = computed(() => {
  if (qrImageSrc.value.startsWith('data:image/jpeg')) return 'manual-payment-qr.jpg'
  if (qrImageSrc.value.startsWith('data:image/png')) return 'manual-payment-qr.png'
  try {
    const pathname = new URL(qrImageSrc.value, window.location.origin).pathname
    const filename = pathname.split('/').pop() || ''
    return /\.(png|jpe?g|webp)$/i.test(filename) ? filename : 'manual-payment-qr'
  } catch {
    return 'manual-payment-qr'
  }
})
const canSubmit = computed(() => ['PENDING', 'EXPIRED'].includes(details.value?.order.status || ''))
let mounted = false
let requestInFlight = false
let pollTimer: ReturnType<typeof setTimeout> | undefined
let pollCount = 0
const completedBalanceRefreshes = new Set<number>()

watch(canDisplayQr, (visible) => {
  if (!visible) qrDialogOpen.value = false
})

function clearPollTimer() {
  if (pollTimer !== undefined) {
    clearTimeout(pollTimer)
    pollTimer = undefined
  }
}

function scheduleStatusPoll() {
  if (!mounted || requestInFlight || submitting.value || pollTimer !== undefined || pollCount >= MAX_STATUS_POLLS) return
  if (!POLLABLE_STATUSES.has(details.value?.order.status || '')) return
  pollTimer = setTimeout(() => {
    pollTimer = undefined
    if (!mounted || requestInFlight || submitting.value) return
    pollCount += 1
    void loadDetails(true)
  }, STATUS_POLL_INTERVAL_MS)
}

async function acceptDetails(next: ManualOrderDetails) {
  details.value = next
  transactionNo.value = next.submission?.transaction_no || transactionNo.value
  note.value = next.submission?.note || note.value

  if (next.order.status === 'COMPLETED') {
    clearPollTimer()
    if (!completedBalanceRefreshes.has(next.order.id)) {
      completedBalanceRefreshes.add(next.order.id)
      try {
        await authStore.refreshUser()
      } catch (err: unknown) {
        appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
      }
    }
  } else if (!POLLABLE_STATUSES.has(next.order.status)) {
    clearPollTimer()
  }
}

async function loadDetails(silent = false): Promise<ManualOrderDetails | null> {
  if (requestInFlight) return null
  requestInFlight = true
  refreshing.value = true
  clearPollTimer()
  const requestedId = id.value
  try {
    const response = await paymentAPI.getManualOrder(requestedId)
    if (!mounted || requestedId !== id.value) return null
    await acceptDetails(response.data)
    return response.data
  } catch (err: unknown) {
    if (!silent) appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
    return null
  } finally {
    requestInFlight = false
    refreshing.value = false
    if (mounted && requestedId !== id.value) {
      void loadDetails()
    } else {
      scheduleStatusPoll()
    }
  }
}

async function refreshDetails() {
  if (requestInFlight || submitting.value) return
  await loadDetails()
}

async function submitReport() {
  if (!canSubmit.value || submitting.value || refreshing.value) return
  clearPollTimer()
  submitting.value = true
  try {
    const submittedOrderId = id.value
    const response = await paymentAPI.submitManualOrder(submittedOrderId, {
      transaction_no: transactionNo.value.trim() || undefined,
      note: note.value.trim() || undefined,
    })
    if (mounted && submittedOrderId === id.value) await acceptDetails(response.data)
  } catch {
    // A timed-out submit can still have reached the server; report ambiguity only if a
    // fresh read does not show that the server accepted or resolved the report.
    const latest = await loadDetails(true)
    if (!latest || ['PENDING', 'EXPIRED'].includes(latest.order.status)) {
      appStore.showError(t('payment.manual.submitAmbiguous'))
    }
  } finally {
    submitting.value = false
    scheduleStatusPoll()
  }
}

onMounted(() => {
  mounted = true
  void loadDetails()
})

watch(id, () => {
  clearPollTimer()
  pollCount = 0
  details.value = null
  transactionNo.value = ''
  note.value = ''
  if (mounted && !requestInFlight) void loadDetails()
})

onUnmounted(() => {
  mounted = false
  clearPollTimer()
})
</script>
