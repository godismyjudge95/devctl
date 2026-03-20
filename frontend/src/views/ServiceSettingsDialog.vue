<script setup lang="ts">
import { ref, watch } from 'vue'
import { Loader2 } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { Button } from '@/components/ui/button'
import { ButtonGroup } from '@/components/ui/button-group'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter,
} from '@/components/ui/dialog'
import {
  getServiceSettings, putServiceSettings,
  detectDNSIP, setupSystemDNS, teardownSystemDNS,
} from '@/lib/api'
import type { MailpitServiceSettings, MySQLServiceSettings, PHPSettings, DNSServiceSettings } from '@/lib/api'

const props = defineProps<{
  open: boolean
  serviceId: string
  serviceLabel: string
}>()

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void
}>()

function isMailpit(id: string) { return id === 'mailpit' }
function isMySQL(id: string) { return id === 'mysql' }
function isPHPFPM(id: string) { return id.startsWith('php-fpm-') }
function isDNS(id: string) { return id === 'dns' }

const loading = ref(false)
const saving = ref(false)

// Mailpit
const mailpitHttpPort = ref('')
const mailpitSmtpPort = ref('')

// MySQL
const mysqlPort = ref('')
const mysqlBindAddress = ref('')

// PHP FPM settings
const phpMemoryLimit = ref('')
const phpUploadMaxFilesize = ref('')
const phpPostMaxSize = ref('')
const phpMaxExecutionTime = ref('')

// DNS
const dnsPort = ref('')
const dnsTargetIP = ref('')
const dnsTLD = ref('')
const dnsSystemConfigured = ref(false)
const dnsDetecting = ref(false)
const dnsSetupLoading = ref(false)

async function loadSettings() {
  loading.value = true
  try {
    const data = await getServiceSettings(props.serviceId)
    if (isMailpit(props.serviceId)) {
      const mp = data as MailpitServiceSettings
      mailpitHttpPort.value = mp.http_port
      mailpitSmtpPort.value = mp.smtp_port
    } else if (isMySQL(props.serviceId)) {
      const my = data as MySQLServiceSettings
      mysqlPort.value = my.port
      mysqlBindAddress.value = my.bind_address
    } else if (isPHPFPM(props.serviceId)) {
      const php = data as PHPSettings
      phpMemoryLimit.value = php.memory_limit
      phpUploadMaxFilesize.value = php.upload_max_filesize
      phpPostMaxSize.value = php.post_max_size
      phpMaxExecutionTime.value = php.max_execution_time
    } else if (isDNS(props.serviceId)) {
      const d = data as DNSServiceSettings
      dnsPort.value = d.port
      dnsTargetIP.value = d.target_ip
      dnsTLD.value = d.tld
      dnsSystemConfigured.value = d.system_dns_configured
    }
  } catch (e: any) {
    toast.error('Failed to load settings', { description: e.message })
    emit('update:open', false)
  } finally {
    loading.value = false
  }
}

async function saveSettings() {
  saving.value = true
  try {
    const id = props.serviceId
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
    emit('update:open', false)
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

watch(() => props.open, (val) => {
  if (val) loadSettings()
})
</script>

<template>
  <Dialog :open="open" @update:open="(v) => emit('update:open', v)">
    <DialogContent class="sm:max-w-lg">
      <DialogHeader>
        <DialogTitle>{{ serviceLabel }} Settings</DialogTitle>
      </DialogHeader>

      <div v-if="loading" class="py-8 text-center text-muted-foreground text-sm">
        <Loader2 class="w-4 h-4 animate-spin inline-block mr-2" />Loading…
      </div>

      <template v-else>
        <!-- Mailpit -->
        <template v-if="isMailpit(serviceId)">
          <div class="grid gap-4 py-2">
            <div class="grid gap-1.5">
              <Label for="svc_mailpit_http">HTTP Port</Label>
              <Input id="svc_mailpit_http" v-model="mailpitHttpPort" class="font-mono" />
            </div>
            <div class="grid gap-1.5">
              <Label for="svc_mailpit_smtp">SMTP Port</Label>
              <Input id="svc_mailpit_smtp" v-model="mailpitSmtpPort" class="font-mono" />
            </div>
            <p class="text-xs text-muted-foreground">Mailpit will be restarted automatically when you save.</p>
          </div>
          <DialogFooter>
            <ButtonGroup>
              <Button variant="outline" @click="emit('update:open', false)">Cancel</Button>
              <Button @click="saveSettings" :disabled="saving">
                <Loader2 v-if="saving" class="w-3.5 h-3.5 animate-spin" />
                Save &amp; Restart
              </Button>
            </ButtonGroup>
          </DialogFooter>
        </template>

        <!-- PHP FPM -->
        <template v-else-if="isPHPFPM(serviceId)">
          <div class="grid gap-4 py-2">
            <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div class="grid gap-1.5">
                <Label for="php_memory_limit">memory_limit</Label>
                <Input id="php_memory_limit" v-model="phpMemoryLimit" class="font-mono" placeholder="256M" />
              </div>
              <div class="grid gap-1.5">
                <Label for="php_upload_max">upload_max_filesize</Label>
                <Input id="php_upload_max" v-model="phpUploadMaxFilesize" class="font-mono" placeholder="128M" />
              </div>
              <div class="grid gap-1.5">
                <Label for="php_post_max">post_max_size</Label>
                <Input id="php_post_max" v-model="phpPostMaxSize" class="font-mono" placeholder="128M" />
              </div>
              <div class="grid gap-1.5">
                <Label for="php_max_exec">max_execution_time</Label>
                <Input id="php_max_exec" v-model="phpMaxExecutionTime" class="font-mono" placeholder="120" />
              </div>
            </div>
            <p class="text-xs text-muted-foreground">PHP-FPM will be restarted automatically when you save.</p>
          </div>
          <DialogFooter>
            <ButtonGroup>
              <Button variant="outline" @click="emit('update:open', false)">Cancel</Button>
              <Button @click="saveSettings" :disabled="saving">
                <Loader2 v-if="saving" class="w-3.5 h-3.5 animate-spin" />
                Save &amp; Restart
              </Button>
            </ButtonGroup>
          </DialogFooter>
        </template>

        <!-- MySQL -->
        <template v-else-if="isMySQL(serviceId)">
          <div class="grid gap-4 py-2">
            <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div class="grid gap-1.5">
                <Label for="mysql_port">Port</Label>
                <Input id="mysql_port" v-model="mysqlPort" class="font-mono" placeholder="3306" />
              </div>
              <div class="grid gap-1.5">
                <Label for="mysql_bind">Bind Address</Label>
                <Input id="mysql_bind" v-model="mysqlBindAddress" class="font-mono" placeholder="127.0.0.1" />
              </div>
            </div>
            <p class="text-xs text-muted-foreground">MySQL will be restarted automatically when you save.</p>
          </div>
          <DialogFooter>
            <ButtonGroup>
              <Button variant="outline" @click="emit('update:open', false)">Cancel</Button>
              <Button @click="saveSettings" :disabled="saving">
                <Loader2 v-if="saving" class="w-3.5 h-3.5 animate-spin" />
                Save &amp; Restart
              </Button>
            </ButtonGroup>
          </DialogFooter>
        </template>

        <!-- DNS -->
        <template v-else-if="isDNS(serviceId)">
          <div class="grid gap-4 py-2">
            <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div class="grid gap-1.5">
                <Label for="dns_port">Port</Label>
                <Input id="dns_port" v-model="dnsPort" class="font-mono" placeholder="5354" />
              </div>
              <div class="grid gap-1.5">
                <Label for="dns_tld">TLD(s)</Label>
                <Input id="dns_tld" v-model="dnsTLD" class="font-mono" placeholder=".test" />
              </div>
            </div>
            <div class="grid gap-1.5">
              <Label for="dns_target_ip">Target IP</Label>
              <div class="flex gap-2">
                <Input id="dns_target_ip" v-model="dnsTargetIP" class="font-mono flex-1" placeholder="192.168.1.x" />
                <Button variant="outline" size="sm" :disabled="dnsDetecting" @click="autoDetectDNSIP">
                  <Loader2 v-if="dnsDetecting" class="w-3.5 h-3.5 animate-spin" />
                  Auto-detect
                </Button>
              </div>
            </div>
            <div class="grid gap-1.5">
              <Label>System DNS</Label>
              <div class="flex items-center gap-3">
                <span class="text-xs text-muted-foreground flex-1">
                  {{ dnsSystemConfigured ? 'systemd-resolved is routing .test queries to this server.' : 'systemd-resolved is not configured to use this DNS server.' }}
                </span>
                <Button
                  v-if="!dnsSystemConfigured"
                  variant="outline" size="sm"
                  :disabled="dnsSetupLoading"
                  @click="configureSystemDNS"
                >
                  <Loader2 v-if="dnsSetupLoading" class="w-3.5 h-3.5 animate-spin" />
                  Configure
                </Button>
                <Button
                  v-else
                  variant="outline" size="sm"
                  :disabled="dnsSetupLoading"
                  class="text-destructive hover:text-destructive"
                  @click="removeSystemDNS"
                >
                  <Loader2 v-if="dnsSetupLoading" class="w-3.5 h-3.5 animate-spin" />
                  Remove
                </Button>
              </div>
            </div>
            <p class="text-xs text-muted-foreground">DNS server will be restarted automatically when you save.</p>
          </div>
          <DialogFooter>
            <ButtonGroup>
              <Button variant="outline" @click="emit('update:open', false)">Cancel</Button>
              <Button @click="saveSettings" :disabled="saving">
                <Loader2 v-if="saving" class="w-3.5 h-3.5 animate-spin" />
                Save &amp; Restart
              </Button>
            </ButtonGroup>
          </DialogFooter>
        </template>
      </template>
    </DialogContent>
  </Dialog>
</template>
