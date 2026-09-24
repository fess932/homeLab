<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import QRCode from 'qrcode'
import { Link2, RefreshCw } from 'lucide-vue-next'
import { api, type Candidate, type DeviceInput, type DiscoverResponse, type DriverInfo, type DriverLogin } from '@/api'
import ApiErrorAlert from '@/components/ui/ApiErrorAlert.vue'
import ModalDialog from '@/components/ui/ModalDialog.vue'
import { t } from '@/i18n'

const props = defineProps<{ driver: DriverInfo }>()
const emit = defineEmits<{ added: []; configure: [initial: Partial<DeviceInput>]; close: [] }>()

const result = ref<DiscoverResponse | null>(null)
const subnet = ref('')
const searching = ref(false)
const error = ref<unknown>(null)
const adopting = ref<string | null>(null)
const addresses = ref<Record<string, string>>({})

const loginOpen = ref(false)
// Код пользователя запоминается в браузере: при новом QR (старый устарел) вводить его заново не нужно.
const USER_CODE_KEY = `homedeck.${props.driver.kind}.userCode`
const userCode = ref(readUserCode())

function readUserCode(): string {
  try {
    return localStorage.getItem(USER_CODE_KEY) ?? ''
  } catch {
    return ''
  }
}

function rememberUserCode(code: string) {
  try {
    localStorage.setItem(USER_CODE_KEY, code)
  } catch {
    /* хранилище браузера недоступно — код просто не запомнится */
  }
}
const login = ref<DriverLogin | null>(null)
const qr = ref('')
const loginState = ref<'idle' | 'waiting' | 'done' | 'expired'>('idle')
let poll: ReturnType<typeof setInterval> | undefined

async function search() {
  searching.value = true
  error.value = null
  try {
    result.value = await api.drivers.discover(props.driver.kind, subnet.value.trim())
  } catch (e) {
    error.value = e
  } finally {
    searching.value = false
  }
}

async function startLogin() {
  error.value = null
  try {
    login.value = await api.drivers.login(props.driver.kind, { user_code: userCode.value.trim() })
    rememberUserCode(userCode.value.trim())
    qr.value = await QRCode.toDataURL(login.value.qr, { margin: 1, width: 240 })
    loginState.value = 'waiting'
    stopPolling()
    poll = setInterval(checkLogin, 2000)
  } catch (e) {
    error.value = e
  }
}

async function checkLogin() {
  if (!login.value) return
  if (Date.now() > Date.parse(login.value.expires)) {
    loginState.value = 'expired'
    stopPolling()
    return
  }
  try {
    const r = await api.drivers.checkLogin(props.driver.kind, login.value.id)
    if (r.state === 'done') {
      loginState.value = 'done'
      stopPolling()
      loginOpen.value = false
      await search()
    }
  } catch (e) {
    error.value = e
    stopPolling()
  }
}

function stopPolling() {
  if (poll) clearInterval(poll)
  poll = undefined
}

const key = (c: Candidate) => c.ref || c.address
const addedID = (c: Candidate) => (c.ref ? result.value?.added[c.ref] : undefined)

async function adopt(c: Candidate) {
  const address = c.address || addresses.value[key(c)]?.trim()
  if (!address) {
    error.value = new Error(t('discover.needAddress'))
    return
  }
  adopting.value = key(c)
  error.value = null
  try {
    await api.drivers.adopt(props.driver.kind, { account_id: c.account_id!, ref: c.ref!, name: c.name, address })
    emit('added')
    await search()
  } catch (e) {
    error.value = e
  } finally {
    adopting.value = null
  }
}

function configure(c: Candidate) {
  emit('configure', { kind: props.driver.kind, name: c.name, address: c.address, config: c.config })
}

onMounted(search)
onBeforeUnmount(stopPolling)
</script>

<template>
  <ModalDialog :title="t('discover.title', { driver: driver.title })" wide @close="emit('close')">
    <ApiErrorAlert :error="error" />

    <section v-if="driver.accounts" class="accounts" aria-labelledby="acc-title">
      <div class="toolbar">
        <h3 id="acc-title">{{ t('discover.accounts') }}</h3>
        <span v-for="a in result?.accounts ?? []" :key="a.id" class="tag">{{ a.name }}</span>
        <span v-if="result && !result.accounts.length" class="small muted">{{ t('discover.noAccounts') }}</span>
        <span class="spacer" />
        <button type="button" class="btn small" @click="loginOpen = !loginOpen"><Link2 :size="14" aria-hidden="true" /> {{ t('discover.connect') }}</button>
      </div>
      <div v-if="loginOpen" class="login">
        <p class="small muted">{{ t('discover.connectHint') }}</p>
        <form class="row" @submit.prevent="startLogin">
          <label class="field">
            <span>{{ t('discover.userCode') }}</span>
            <input v-model="userCode" class="input code" required autocomplete="off" />
            <span class="hint">{{ t('discover.userCodeHint') }}</span>
          </label>
          <button type="submit" class="btn" :disabled="!userCode.trim()">{{ t('discover.showQR') }}</button>
        </form>
        <div v-if="login && loginState !== 'idle'" class="qr" role="status">
          <img v-if="loginState === 'waiting'" :src="qr" :alt="t('discover.qrAlt')" width="240" height="240" />
          <p class="small">
            {{ loginState === 'waiting' ? login.hint : loginState === 'expired' ? t('discover.qrExpired') : t('discover.connected') }}
          </p>
        </div>
      </div>
    </section>

    <form class="toolbar search" @submit.prevent="search">
      <label class="field subnet">
        <span>{{ t('discover.subnet') }}</span>
        <input v-model="subnet" class="input code" :placeholder="result?.subnets.join(', ') || '192.168.0.0/24'" />
      </label>
      <button type="submit" class="btn" :disabled="searching">
        <RefreshCw :size="14" aria-hidden="true" :class="{ spin: searching }" /> {{ searching ? t('discover.searching') : t('discover.search') }}
      </button>
    </form>
    <p class="small muted">{{ t('discover.hint') }}</p>
    <div v-for="w in result?.warnings ?? []" :key="w" class="alert warn small">{{ w }}</div>

    <p v-if="result && !result.candidates.length && !searching" class="muted">{{ t('discover.nothing') }}</p>
    <div v-if="result?.candidates.length" class="table-wrap">
      <table class="table">
        <thead>
          <tr>
            <th>{{ t('sources.name') }}</th>
            <th>{{ t('devices.address') }}</th>
            <th>{{ t('devices.deviceId') }}</th>
            <th>{{ t('discover.found') }}</th>
            <th><span class="sr-only">{{ t('discover.actions') }}</span></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="c in result.candidates" :key="key(c)">
            <td>
              {{ c.name || c.product_id || '—' }}
              <div v-if="c.note" class="small muted">{{ c.note }}</div>
            </td>
            <td>
              <template v-if="c.address">{{ c.address }}</template>
              <input v-else v-model="addresses[key(c)]" class="input code addr" placeholder="192.168.0.x" :aria-label="t('devices.address')" />
            </td>
            <td><code class="small">{{ c.ref || '—' }}</code></td>
            <td class="flags">
              <span v-if="c.in_network" class="tag">{{ t('discover.inNetwork') }}</span>
              <span v-if="c.account_id" class="tag">{{ t('discover.inAccount') }}</span>
              <span v-if="c.has_key" class="tag">{{ t('discover.hasKey') }}</span>
            </td>
            <td class="actions">
              <span v-if="addedID(c)" class="small muted">{{ t('discover.added') }}</span>
              <button
                v-else-if="c.has_key && c.account_id"
                type="button"
                class="btn small primary"
                :disabled="adopting === key(c)"
                @click="adopt(c)"
              >
                {{ adopting === key(c) ? t('discover.adding') : t('discover.add') }}
              </button>
              <button v-else type="button" class="btn small" @click="configure(c)">{{ t('discover.configure') }}</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <template #footer>
      <button type="button" class="btn" @click="emit('close')">{{ t('app.close') }}</button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.accounts {
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 8px 12px;
  margin-bottom: 14px;
}

.accounts h3 {
  margin: 0;
}

.login {
  margin-top: 10px;
}

.login form {
  align-items: flex-end;
}

.login form .btn {
  margin-bottom: 12px;
}

.qr {
  display: flex;
  gap: 16px;
  align-items: center;
  flex-wrap: wrap;
}

.qr img {
  background: #fff;
  padding: 8px;
  border-radius: var(--radius-sm);
}

.search {
  align-items: flex-end;
}

.search .btn {
  margin-bottom: 12px;
}

.subnet {
  flex: 0 1 280px;
}

.addr {
  min-width: 140px;
}

.flags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.actions {
  text-align: right;
  white-space: nowrap;
}

.spin {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
