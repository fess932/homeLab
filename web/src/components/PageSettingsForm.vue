<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, type Asset, type PageInput } from '@/api'
import ModalDialog from '@/components/ui/ModalDialog.vue'
import { t } from '@/i18n'
import { slugPattern } from '@/lib/validate'

const props = defineProps<{ page: PageInput }>()
const emit = defineEmits<{ save: [p: Pick<PageInput, 'title' | 'slug' | 'theme' | 'public'>]; close: [] }>()

const title = ref(props.page.title)
const slug = ref(props.page.slug)
const theme = ref({ ...props.page.theme })
const assets = ref<Asset[]>([])
const slugError = ref('')
const isPublic = ref(props.page.public ?? false)
const publicUrl = computed(() => `${location.origin}/public/${slug.value}`)
const copied = ref(false)

async function copyLink() {
  try {
    await navigator.clipboard.writeText(publicUrl.value)
    copied.value = true
  } catch {
    copied.value = false
  }
}

onMounted(async () => {
  try {
    assets.value = await api.assets.list()
  } catch {
    assets.value = []
  }
})

function submit() {
  if (!slugPattern.test(slug.value)) {
    slugError.value = t('validation.slug')
    return
  }
  emit('save', { title: title.value.trim(), slug: slug.value, theme: theme.value, public: isPublic.value })
}
</script>

<template>
  <ModalDialog :title="t('editor.pageSettings')" @close="emit('close')">
    <form id="page-form" @submit.prevent="submit">
      <label class="field">
        <span>{{ t('editor.pageTitle') }}</span>
        <input v-model="title" class="input" required maxlength="100" />
      </label>
      <label class="field">
        <span>{{ t('editor.slug') }}</span>
        <input v-model="slug" class="input" required maxlength="40" :aria-invalid="!!slugError" aria-describedby="slug-hint" />
        <span id="slug-hint" class="hint">{{ t('editor.slugHint') }}</span>
        <span v-if="slugError" class="error">{{ slugError }}</span>
      </label>
      <label class="field check">
        <input v-model="isPublic" type="checkbox" /> {{ t('editor.public') }}
      </label>
      <div v-if="isPublic" class="field">
        <div class="link-row">
          <input class="input" :value="publicUrl" readonly @focus="($event.target as HTMLInputElement).select()" />
          <button type="button" class="btn" @click="copyLink">{{ copied ? t('editor.copied') : t('editor.copy') }}</button>
          <a class="btn" :href="publicUrl" target="_blank" rel="noopener">{{ t('editor.openLink') }}</a>
        </div>
      </div>
      <fieldset class="theme">
        <legend>{{ t('editor.theme') }}</legend>
        <div class="row">
          <label class="field">
            <span>{{ t('editor.mode') }}</span>
            <select v-model="theme.mode" class="input">
              <option value="system">{{ t('editor.modes.system') }}</option>
              <option value="light">{{ t('editor.modes.light') }}</option>
              <option value="dark">{{ t('editor.modes.dark') }}</option>
            </select>
          </label>
          <label class="field">
            <span>{{ t('editor.accent') }}</span>
            <input v-model="theme.accent" type="color" class="input color" />
          </label>
        </div>
        <div class="row">
          <label class="field">
            <span>{{ t('editor.density') }}</span>
            <select v-model="theme.density" class="input">
              <option value="comfortable">{{ t('editor.densities.comfortable') }}</option>
              <option value="compact">{{ t('editor.densities.compact') }}</option>
            </select>
          </label>
          <label class="field">
            <span>{{ t('editor.columns') }}</span>
            <select v-model.number="theme.columns" class="input">
              <option v-for="c in [4, 6, 8, 12]" :key="c" :value="c">{{ c }}</option>
            </select>
          </label>
        </div>
        <label class="field">
          <span>{{ t('editor.background') }}</span>
          <select v-model="theme.background_asset_id" class="input">
            <option :value="null">{{ t('editor.noBackground') }}</option>
            <option v-for="a in assets" :key="a.id" :value="a.id">{{ a.id }} · {{ a.width }}×{{ a.height }}</option>
          </select>
        </label>
      </fieldset>
    </form>
    <template #footer>
      <button type="button" class="btn" @click="emit('close')">{{ t('app.cancel') }}</button>
      <button type="submit" form="page-form" class="btn primary">{{ t('app.save') }}</button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.theme {
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 8px 12px 0;
}

.link-row {
  display: flex;
  gap: 8px;
}

.link-row .input {
  flex: 1;
  min-width: 0;
}

.color {
  padding: 2px;
  height: 36px;
}
</style>
