<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Plus, Trash2 } from 'lucide-vue-next'
import { api, type Service } from '@/api'
import ServiceForm from '@/components/ServiceForm.vue'
import ApiErrorAlert from '@/components/ui/ApiErrorAlert.vue'
import ServiceIcon from '@/components/ui/ServiceIcon.vue'
import StatusBadge from '@/components/ui/StatusBadge.vue'
import { t } from '@/i18n'
import { ensureCatalog, loadChecks, loadServices, store } from '@/stores/app'

const editing = ref<Service | 'new' | null>(null)
const error = ref<unknown>(null)

onMounted(() => ensureCatalog().catch((e) => (error.value = e)))

async function reload() {
  await Promise.all([loadServices(), loadChecks()])
}

async function onSaved() {
  editing.value = null
  await reload().catch((e) => (error.value = e))
}

async function remove(s: Service) {
  if (!confirm(t('app.confirmDelete', { name: s.name }))) return
  try {
    await api.services.remove(s.id)
    await reload()
  } catch (e) {
    error.value = e
  }
}
</script>

<template>
  <section class="card">
    <div class="toolbar">
      <h2>{{ t('settings.services') }}</h2>
      <span class="spacer" />
      <button type="button" class="btn primary small" @click="editing = 'new'">
        <Plus :size="14" aria-hidden="true" /> {{ t('settings.newService') }}
      </button>
    </div>
    <ApiErrorAlert :error="error" />
    <div class="table-wrap">
      <table class="table">
        <thead>
          <tr>
            <th>{{ t('settings.serviceName') }}</th>
            <th>{{ t('settings.url') }}</th>
            <th>{{ t('settings.check') }}</th>
            <th><span class="sr-only">{{ t('app.edit') }}</span></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="s in store.services" :key="s.id">
            <td class="name">
              <ServiceIcon :icon="s.icon" :size="20" />
              <span>{{ s.name }}</span>
              <span v-for="tag in s.tags" :key="tag" class="tag">{{ tag }}</span>
            </td>
            <td class="url">{{ s.url }}</td>
            <td>
              <StatusBadge v-if="s.check_id" :status="s.status" />
              <span v-else class="muted small">{{ t('settings.checkNone') }}</span>
            </td>
            <td class="actions">
              <button type="button" class="btn small" @click="editing = s">{{ t('app.edit') }}</button>
              <button type="button" class="btn small icon danger" :aria-label="t('app.delete')" @click="remove(s)">
                <Trash2 :size="14" aria-hidden="true" />
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <ServiceForm v-if="editing" :service="editing === 'new' ? null : editing" @saved="onSaved" @close="editing = null" />
  </section>
</template>

<style scoped>
.name {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
}

.url {
  overflow-wrap: anywhere;
  max-width: 280px;
}

.actions {
  white-space: nowrap;
  text-align: right;
}
</style>
