<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Loader2 } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import {
  getServiceSettings, putServiceSettings,
  detectDNSIP, setupSystemDNS, teardownSystemDNS,
} from '@/lib/api'
import type {
  MailpitServiceSettings,
  MySQLServiceSettings,
  MeilisearchServiceSettings,
  PHPSettings,
  DNSServiceSettings,
  PostgresServiceSettings,
  PostgresExtensionStatus,
} from '@/lib/api'
import { useServicesStore } from '@/stores/services'
import PageHeader from '@/components/layout/PageHeader.vue'
import Surface from '@/components/layout/Surface.vue'
import SectionHeader from '@/components/layout/SectionHeader.vue'
import SettingRow from '@/components/layout/SettingRow.vue'
import ServiceMark from '@/components/layout/ServiceMark.vue'
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
const phpMemoryLimit = ref('')
const phpUploadMaxFilesize = ref('')
const phpPostMaxSize = ref('')
const phpMaxExecutionTime = ref('')
const meilisearchEnv = ref('')
const meilisearchArgs = ref('')
const dnsPort = ref('')
const dnsTargetIP = ref('')
const dnsTLD = ref('')
const dnsSystemConfigured = ref(false)
const dnsDetecting = ref(false)
const dnsSetupLoading = ref(false)
const postgresExtensions = ref<PostgresExtensionStatus[]>([])

async function loadSettings() {
  loading.value = true
  try {
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
    } else if (isPHPFPM(serviceId.value)) {
      const php = data as PHPSettings
      phpMemoryLimit.value = php.memory_limit
      phpUploadMaxFilesize.value = php.upload_max_filesize
      phpPostMaxSize.value = php.post_max_size
      phpMaxExecutionTime.value = php.max_execution_time
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
      await putServiceSettings(id, {
        memory_limit: phpMemoryLimit.value,
        upload_max_filesize: phpUploadMaxFilesize.value,
        post_max_size: phpPostMaxSize.value,
        max_execution_time: phpMaxExecutionTime.value,
      })
      toast.success('PHP settings saved — restarting FPM…')
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
    >
      <template #actions>
        <Button variant="outline" @click="router.push('/services')">Cancel</Button>
        <Button v-if="known && !isPostgres(serviceId)" @click="saveSettings" :disabled="saving || loading">
          <Loader2 v-if="saving" class="w-4 h-4 animate-spin" />
          {{ saving ? 'Saving…' : 'Save & Restart' }}
        </Button>
      </template>
    </PageHeader>

    <div class="flex items-center gap-2.5 text-sm text-muted-foreground">
      <ServiceMark :id="serviceId" size="sm" />
      <span>{{ serviceLabel }}</span>
    </div>

    <div v-if="loading" class="text-sm text-muted-foreground py-8 text-center">
      <Loader2 class="w-4 h-4 animate-spin inline-block mr-2" />Loading…
    </div>

    <EmptyState v-else-if="!known" title="No settings">
      This service has no dedicated settings page.
    </EmptyState>

    <Surface v-else-if="isMailpit(serviceId)">
      <SectionHeader title="Ports" description="Mailpit restarts when saved." />
      <div class="px-5 pb-2">
        <SettingRow label="HTTP port" for="svc_mailpit_http">
          <Input id="svc_mailpit_http" v-model="mailpitHttpPort" class="font-mono" />
        </SettingRow>
        <SettingRow label="SMTP port" for="svc_mailpit_smtp">
          <Input id="svc_mailpit_smtp" v-model="mailpitSmtpPort" class="font-mono" />
        </SettingRow>
      </div>
    </Surface>

    <Surface v-else-if="isMeilisearch(serviceId)">
      <SectionHeader title="Runtime" description="Meilisearch restarts when saved." />
      <div class="px-5 pb-2">
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
      <SectionHeader title="php.ini" description="PHP-FPM restarts when saved." />
      <div class="px-5 pb-2">
        <SettingRow label="memory_limit" for="php_memory_limit">
          <Input id="php_memory_limit" v-model="phpMemoryLimit" class="font-mono" placeholder="256M" />
        </SettingRow>
        <SettingRow label="upload_max_filesize" for="php_upload_max">
          <Input id="php_upload_max" v-model="phpUploadMaxFilesize" class="font-mono" placeholder="128M" />
        </SettingRow>
        <SettingRow label="post_max_size" for="php_post_max">
          <Input id="php_post_max" v-model="phpPostMaxSize" class="font-mono" placeholder="128M" />
        </SettingRow>
        <SettingRow label="max_execution_time" for="php_max_exec">
          <Input id="php_max_exec" v-model="phpMaxExecutionTime" class="font-mono" placeholder="120" />
        </SettingRow>
      </div>
    </Surface>

    <Surface v-else-if="isMySQL(serviceId)">
      <SectionHeader title="Listen" description="MySQL restarts when saved." />
      <div class="px-5 pb-2">
        <SettingRow label="Port" for="mysql_port">
          <Input id="mysql_port" v-model="mysqlPort" class="font-mono" placeholder="3306" />
        </SettingRow>
        <SettingRow label="Bind address" for="mysql_bind">
          <Input id="mysql_bind" v-model="mysqlBindAddress" class="font-mono" placeholder="127.0.0.1" />
        </SettingRow>
      </div>
    </Surface>

    <Surface v-else-if="isDNS(serviceId)">
      <SectionHeader title="Resolver" description="The DNS server restarts when saved." />
      <div class="px-5 pb-2">
        <SettingRow label="Port" for="dns_port">
          <Input id="dns_port" v-model="dnsPort" class="font-mono" placeholder="5354" />
        </SettingRow>
        <SettingRow label="TLDs" for="dns_tld">
          <Input id="dns_tld" v-model="dnsTLD" class="font-mono" placeholder=".test" />
        </SettingRow>
        <SettingRow label="Target IP" for="dns_target_ip">
          <div class="flex gap-2">
            <Input id="dns_target_ip" v-model="dnsTargetIP" class="font-mono flex-1" placeholder="192.168.1.x" />
            <Button variant="outline" :disabled="dnsDetecting" @click="autoDetectDNSIP">
              <Loader2 v-if="dnsDetecting" class="w-3.5 h-3.5 animate-spin" />
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
            <Loader2 v-if="dnsSetupLoading" class="w-3.5 h-3.5 animate-spin" />
            Configure
          </Button>
          <Button
            v-else
            variant="destructive"
            :disabled="dnsSetupLoading"
            @click="removeSystemDNS"
          >
            <Loader2 v-if="dnsSetupLoading" class="w-3.5 h-3.5 animate-spin" />
            Remove
          </Button>
        </SettingRow>
      </div>
    </Surface>

    <Surface v-else-if="isPostgres(serviceId)">
      <SectionHeader
        title="Managed extensions"
        description="Installed with PostgreSQL. pg_clickhouse wires into template1 when ClickHouse is also installed."
      />
      <div class="px-5 pb-5">
        <div v-if="postgresExtensions.length === 0" class="text-sm text-muted-foreground py-4">
          No managed extensions.
        </div>
        <ul v-else class="divide-y divide-border rounded-xl border">
          <li
            v-for="ext in postgresExtensions"
            :key="ext.id"
            class="flex items-start justify-between gap-3 px-4 py-3"
          >
            <div class="min-w-0">
              <div class="flex items-center gap-2">
                <span class="font-medium text-sm">{{ ext.label }}</span>
                <span v-if="ext.version" class="text-xs font-mono text-muted-foreground">{{ ext.version }}</span>
              </div>
              <p class="text-xs text-muted-foreground mt-0.5">{{ ext.note }}</p>
            </div>
            <span
              class="shrink-0 text-[10px] font-medium uppercase tracking-wide rounded px-1.5 py-0.5"
              :class="ext.ready
                ? 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-400'
                : ext.files_installed
                  ? 'bg-amber-500/15 text-amber-700 dark:text-amber-400'
                  : 'bg-muted text-muted-foreground'"
            >
              {{ ext.ready ? 'Ready' : ext.files_installed ? 'Partial' : 'Missing' }}
            </span>
          </li>
        </ul>
      </div>
    </Surface>
  </div>
</template>
