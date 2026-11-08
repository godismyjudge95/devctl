<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Loader2 } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import {
  getServiceSettings, putServiceSettings,
  detectDNSIP, setupSystemDNS, teardownSystemDNS,
  getPHPVersions, getPHPSettings, setPHPSettings, unsetPHPIni,
} from '@/lib/api'
import type {
  MailpitServiceSettings,
  MySQLServiceSettings,
  MeilisearchServiceSettings,
  PHPSettings,
  PHPSettingsView,
  DNSServiceSettings,
  PostgresServiceSettings,
  PostgresExtensionStatus,
} from '@/lib/api'
import { useServicesStore } from '@/stores/services'
import PageHeader from '@/components/layout/PageHeader.vue'
import Surface from '@/components/layout/Surface.vue'
import SectionHeader from '@/components/layout/SectionHeader.vue'
import SettingRow from '@/components/layout/SettingRow.vue'
import { Badge } from '@/components/ui/badge'
import EmptyState from '@/components/layout/EmptyState.vue'

const route = useRoute()
const router = useRouter()
const store = useServicesStore()

const serviceId = computed(() => route.params.id as string)
const service = computed(() => store.states.find(s => s.id === serviceId.value))
const serviceLabel = computed(() => service.value?.label ?? serviceId.value)

function isMailpit(id: string) { return id === 'mailpit' }
function isMySQL(id: string) { return id === 'mysql' }
function isMeilisearch(id: string) { return id === 'meilisearch' }
function isPHPFPM(id: string) { return id.startsWith('php-fpm-') }
function isDNS(id: string) { return id === 'dns' }
function isPostgres(id: string) { return id === 'postgres' }

const known = computed(() =>
  isMailpit(serviceId.value) || isMySQL(serviceId.value) || isMeilisearch(serviceId.value)
  || isPHPFPM(serviceId.value) || isDNS(serviceId.value) || isPostgres(serviceId.value),
)

const loading = ref(false)
const saving = ref(false)

const mailpitHttpPort = ref('')
const mailpitSmtpPort = ref('')
const mysqlPort = ref('')
const mysqlBindAddress = ref('')
type PHPSettingKey = keyof PHPSettings
const phpFields: { key: PHPSettingKey, id: string, placeholder: string }[] = [
  { key: 'memory_limit', id: 'php_memory_limit', placeholder: '256M' },
  { key: 'upload_max_filesize', id: 'php_upload_max', placeholder: '128M' },
  { key: 'post_max_size', id: 'php_post_max', placeholder: '128M' },
  { key: 'max_execution_time', id: 'php_max_exec', placeholder: '120' },
]
const phpScope = ref('all')
const phpVersions = ref<string[]>([])
const phpForm = ref<PHPSettings>({ memory_limit: '', upload_max_filesize: '', post_max_size: '', max_execution_time: '' })
const phpLoaded = ref<PHPSettingsView | null>(null)
const phpGlobal = ref<PHPSettingsView | null>(null)
const meilisearchEnv = ref('')
const meilisearchArgs = ref('')
const dnsPort = ref('')
const dnsTargetIP = ref('')
const dnsTLD = ref('')
const dnsSystemConfigured = ref(false)
const dnsDetecting = ref(false)
const dnsSetupLoading = ref(false)
const postgresExtensions = ref<PostgresExtensionStatus[]>([])

async function loadPHPSettings() {
  const scope = phpScope.value
  const [global, current] = await Promise.all([
    getPHPSettings(),
    scope === 'all' ? Promise.resolve(null) : getPHPSettings(scope),
  ])
  const view = current ?? global
  phpGlobal.value = global
  phpLoaded.value = view
  phpForm.value = Object.fromEntries(phpFields.map(f => [f.key, view[f.key]])) as unknown as PHPSettings
}

function phpOverridden(key: PHPSettingKey) {
  return phpScope.value !== 'all' && (phpLoaded.value?.overrides ?? []).includes(key)
}

function phpHint(key: PHPSettingKey) {
  if (phpScope.value === 'all' || !phpLoaded.value || !phpGlobal.value) return undefined
  if (phpLoaded.value[key] !== phpGlobal.value[key]) return `Differs from all versions (${phpGlobal.value[key]}).`
  if (phpOverridden(key)) return 'Set for this version.'
  return undefined
}

async function resetPHPSetting(key: PHPSettingKey) {
  try {
    await unsetPHPIni(phpScope.value, key)
    await loadPHPSettings()
    toast.success(`${key} now uses the all-versions value`)
  } catch (e: any) {
    toast.error('Failed to reset setting', { description: e.message })
  }
}

watch(phpScope, () => {
  loadPHPSettings().catch((e: any) => toast.error('Failed to load settings', { description: e.message }))
})

async function loadSettings() {
  loading.value = true
  try {
    if (isPHPFPM(serviceId.value)) {
      phpVersions.value = (await getPHPVersions()).map(v => v.version)
      await loadPHPSettings()
      return
    }
    const data = await getServiceSettings(serviceId.value)
    if (isMailpit(serviceId.value)) {
      const mp = data as MailpitServiceSettings
      mailpitHttpPort.value = mp.http_port
      mailpitSmtpPort.value = mp.smtp_port
    } else if (isMySQL(serviceId.value)) {
      const my = data as MySQLServiceSettings
      mysqlPort.value = my.port
      mysqlBindAddress.value = my.bind_address
    } else if (isMeilisearch(serviceId.value)) {
      const meili = data as MeilisearchServiceSettings
      meilisearchEnv.value = meili.env
      meilisearchArgs.value = meili.args
    } else if (isDNS(serviceId.value)) {
      const d = data as DNSServiceSettings
      dnsPort.value = d.port
      dnsTargetIP.value = d.target_ip
      dnsTLD.value = d.tld
      dnsSystemConfigured.value = d.system_dns_configured
    } else if (isPostgres(serviceId.value)) {
      const pg = data as PostgresServiceSettings
      postgresExtensions.value = pg.extensions ?? []
    }
  } catch (e: any) {
    toast.error('Failed to load settings', { description: e.message })
  } finally {
    loading.value = false
  }
}

async function saveSettings() {
  saving.value = true
  try {
    const id = serviceId.value
    if (isMailpit(id)) {
      await putServiceSettings(id, {
        http_port: mailpitHttpPort.value,
        smtp_port: mailpitSmtpPort.value,
      })
      toast.success('Mailpit settings saved — restarting…')
    } else if (isMySQL(id)) {
      await putServiceSettings(id, {
        port: mysqlPort.value,
        bind_address: mysqlBindAddress.value,
      })
      toast.success('MySQL settings saved — restarting…')
    } else if (isMeilisearch(id)) {
      await putServiceSettings(id, {
        env: meilisearchEnv.value,
        args: meilisearchArgs.value,
      })
      toast.success('Meilisearch settings saved — restarting…')
    } else if (isPHPFPM(id)) {
      // Send only edited keys so untouched ones keep following their source.
      const changed = Object.fromEntries(
        phpFields
          .filter(f => phpForm.value[f.key] !== phpLoaded.value?.[f.key])
          .map(f => [f.key, phpForm.value[f.key]]),
      ) as Partial<PHPSettings>
      if (Object.keys(changed).length === 0) {
        toast.info('No changes to save')
        return
      }
      const scope = phpScope.value
      await setPHPSettings(changed, scope === 'all' ? undefined : scope)
      await loadPHPSettings()
      toast.success(scope === 'all' ? 'PHP settings saved for all versions' : `PHP ${scope} settings saved`)
    } else if (isDNS(id)) {
      await putServiceSettings(id, {
        port: dnsPort.value,
        target_ip: dnsTargetIP.value,
        tld: dnsTLD.value,
        system_dns_configured: dnsSystemConfigured.value,
      })
      toast.success('DNS settings saved — restarting…')
    }
  } catch (e: any) {
    toast.error('Failed to save settings', { description: e.message })
  } finally {
    saving.value = false
  }
}

async function autoDetectDNSIP() {
  dnsDetecting.value = true
  try {
    const res = await detectDNSIP()
    dnsTargetIP.value = res.ip
  } catch (e: any) {
    toast.error('Failed to detect IP', { description: e.message })
  } finally {
    dnsDetecting.value = false
  }
}

async function configureSystemDNS() {
  dnsSetupLoading.value = true
  try {
    await setupSystemDNS()
    dnsSystemConfigured.value = true
    toast.success('System DNS configured')
  } catch (e: any) {
    toast.error('Failed to configure system DNS', { description: e.message })
  } finally {
    dnsSetupLoading.value = false
  }
}

async function removeSystemDNS() {
  dnsSetupLoading.value = true
  try {
    await teardownSystemDNS()
    dnsSystemConfigured.value = false
    toast.success('System DNS configuration removed')
  } catch (e: any) {
    toast.error('Failed to remove system DNS configuration', { description: e.message })
  } finally {
    dnsSetupLoading.value = false
  }
}

onMounted(loadSettings)
</script>

<template>
  <div class="space-y-6">
    <PageHeader
      :title="`${serviceLabel} settings`"
      description="Changes restart the service when you save."
      back-to="/services"
      back-label="Services"
    />

    <div v-if="loading" class="flex items-center justify-center gap-2 py-12 text-sm text-muted-foreground">
      <Loader2 class="size-4 animate-spin" />
      Loading…
    </div>

    <EmptyState v-else-if="!known" title="No settings">
      This service has no dedicated settings page.
    </EmptyState>

    <Surface v-else-if="isMailpit(serviceId)">
      <SectionHeader title="Ports" description="Mailpit restarts when saved." />
      <div class="px-4 pb-1 sm:px-5">
        <SettingRow label="HTTP port" for="svc_mailpit_http">
          <Input id="svc_mailpit_http" v-model="mailpitHttpPort" class="font-mono sm:max-w-xs" />
        </SettingRow>
        <SettingRow label="SMTP port" for="svc_mailpit_smtp">
          <Input id="svc_mailpit_smtp" v-model="mailpitSmtpPort" class="font-mono sm:max-w-xs" />
        </SettingRow>
      </div>
    </Surface>

    <Surface v-else-if="isMeilisearch(serviceId)">
      <SectionHeader title="Runtime" description="Meilisearch restarts when saved." />
      <div class="px-4 pb-1 sm:px-5">
        <SettingRow label="Environment variables" hint="One KEY=VALUE entry per line." for="meilisearch_env">
          <Textarea
            id="meilisearch_env"
            v-model="meilisearchEnv"
            class="font-mono min-h-28"
            placeholder="MEILI_EXPERIMENTAL_ALLOWED_IP_NETWORKS=any"
          />
        </SettingRow>
        <SettingRow label="Extra command-line args" hint="Appended after --config-file-path." for="meilisearch_args">
          <Textarea
            id="meilisearch_args"
            v-model="meilisearchArgs"
            class="font-mono min-h-24"
            placeholder="--experimental-allowed-ip-networks any"
          />
        </SettingRow>
      </div>
    </Surface>

    <Surface v-else-if="isPHPFPM(serviceId)">
      <SectionHeader title="php.ini" description="Running PHP-FPM pools restart when saved." />
      <div class="px-4 pb-1 sm:px-5">
        <SettingRow label="Applies to" hint="A version value wins over the all-versions value." for="php_scope">
          <Select v-model="phpScope">
            <SelectTrigger id="php_scope" class="w-full sm:w-48">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All versions</SelectItem>
              <SelectItem v-for="ver in phpVersions" :key="ver" :value="ver">PHP {{ ver }}</SelectItem>
            </SelectContent>
          </Select>
        </SettingRow>
        <SettingRow v-for="f in phpFields" :key="f.key" :label="f.key" :hint="phpHint(f.key)" :for="f.id">
          <div class="flex flex-wrap items-center gap-2">
            <Input :id="f.id" v-model="phpForm[f.key]" class="font-mono sm:max-w-xs" :placeholder="f.placeholder" />
            <Button v-if="phpOverridden(f.key)" variant="outline" size="sm" @click="resetPHPSetting(f.key)">
              Use all-versions value
            </Button>
          </div>
        </SettingRow>
      </div>
    </Surface>

    <Surface v-else-if="isMySQL(serviceId)">
      <SectionHeader title="Listen" description="MySQL restarts when saved." />
      <div class="px-4 pb-1 sm:px-5">
        <SettingRow label="Port" for="mysql_port">
          <Input id="mysql_port" v-model="mysqlPort" class="font-mono sm:max-w-xs" placeholder="3306" />
        </SettingRow>
        <SettingRow label="Bind address" for="mysql_bind">
          <Input id="mysql_bind" v-model="mysqlBindAddress" class="font-mono sm:max-w-xs" placeholder="127.0.0.1" />
        </SettingRow>
      </div>
    </Surface>

    <Surface v-else-if="isDNS(serviceId)">
      <SectionHeader title="Resolver" description="The DNS server restarts when saved." />
      <div class="px-4 pb-1 sm:px-5">
        <SettingRow label="Port" for="dns_port">
          <Input id="dns_port" v-model="dnsPort" class="font-mono sm:max-w-xs" placeholder="5354" />
        </SettingRow>
        <SettingRow label="TLDs" for="dns_tld">
          <Input id="dns_tld" v-model="dnsTLD" class="font-mono sm:max-w-xs" placeholder=".test" />
        </SettingRow>
        <SettingRow label="Target IP" for="dns_target_ip">
          <div class="flex gap-2 sm:max-w-md">
            <Input id="dns_target_ip" v-model="dnsTargetIP" class="flex-1 font-mono" placeholder="192.168.1.x" />
            <Button variant="outline" :disabled="dnsDetecting" @click="autoDetectDNSIP">
              <Loader2 v-if="dnsDetecting" class="size-3.5 animate-spin" />
              Auto-detect
            </Button>
          </div>
        </SettingRow>
        <SettingRow
          label="System DNS"
          :hint="dnsSystemConfigured
            ? 'systemd-resolved is routing .test queries to this server.'
            : 'systemd-resolved is not configured to use this DNS server.'"
        >
          <Button
            v-if="!dnsSystemConfigured"
            variant="outline"
            :disabled="dnsSetupLoading"
            @click="configureSystemDNS"
          >
            <Loader2 v-if="dnsSetupLoading" class="size-3.5 animate-spin" />
            Configure
          </Button>
          <Button
            v-else
            variant="destructive"
            :disabled="dnsSetupLoading"
            @click="removeSystemDNS"
          >
            <Loader2 v-if="dnsSetupLoading" class="size-3.5 animate-spin" />
            Remove
          </Button>
        </SettingRow>
      </div>
    </Surface>

    <Surface v-else-if="isPostgres(serviceId)">
      <SectionHeader
        title="Managed extensions"
        description="Installed with PostgreSQL. pg_clickhouse wires into template1 when ClickHouse is also installed. Contrib search modules are created in every connectable database."
      />
      <div class="px-4 pb-4 sm:px-5 sm:pb-5">
        <div v-if="postgresExtensions.length === 0" class="py-4 text-sm text-muted-foreground">
          No managed extensions.
        </div>
        <ul v-else class="divide-y divide-border rounded-lg border border-border">
          <li
            v-for="ext in postgresExtensions"
            :key="ext.id"
            class="flex items-start justify-between gap-3 px-4 py-3"
          >
            <div class="min-w-0">
              <div class="flex items-center gap-2">
                <span class="text-sm font-medium">{{ ext.label }}</span>
                <span v-if="ext.version" class="font-mono text-xs text-muted-foreground">{{ ext.version }}</span>
              </div>
              <p v-if="ext.note && ext.note.toLowerCase() !== 'ready'" class="mt-0.5 text-xs text-muted-foreground">{{ ext.note }}</p>
            </div>
            <Badge :variant="ext.ready ? 'success' : ext.files_installed ? 'warning' : 'secondary'">
              {{ ext.ready ? 'Ready' : ext.files_installed ? 'Partial' : 'Missing' }}
            </Badge>
          </li>
        </ul>
      </div>
    </Surface>

    <div v-if="!loading" class="flex flex-wrap justify-end gap-2">
      <Button variant="outline" size="sm" @click="router.push('/services')">Cancel</Button>
      <Button v-if="known && !isPostgres(serviceId)" size="sm" :disabled="saving || loading" @click="saveSettings">
        <Loader2 v-if="saving" class="size-3.5 animate-spin" />
        {{ saving ? 'Saving…' : 'Save & Restart' }}
      </Button>
    </div>
  </div>
</template>
