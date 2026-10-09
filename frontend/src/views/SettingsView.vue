<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useSettingsStore } from '@/stores/settings'
import { Download, ShieldCheck, RotateCw, Loader2 } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Card, CardContent, CardFooter, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import PageHeader from '@/components/layout/PageHeader.vue'
import { restartDevctl, trustTLS } from '@/lib/api'
import { toast } from 'vue-sonner'

const store = useSettingsStore()
onMounted(() => {
  store.load()
})

async function save(key: string, value: string) {
  try {
    await store.save({ [key]: value })
    toast.success('Setting saved')
  } catch (e: any) {
    toast.error('Failed to save setting', { description: e.message })
  }
}

const restarting = ref(false)
const restartStatus = ref<'idle' | 'restarting' | 'reconnecting' | 'done' | 'error'>('idle')

async function saveAndRestart() {
  restarting.value = true
  restartStatus.value = 'restarting'
  try {
    await restartDevctl()
  } catch {
    // The process may die before it can send a response — that's fine.
  }
  restartStatus.value = 'reconnecting'
  // Poll /api/settings until the server comes back up.
  const deadline = Date.now() + 15_000
  while (Date.now() < deadline) {
    await new Promise(r => setTimeout(r, 800))
    try {
      const res = await fetch('/api/settings')
      if (res.ok) {
        await store.load()
        restartStatus.value = 'done'
        restarting.value = false
        setTimeout(() => { restartStatus.value = 'idle' }, 3000)
        return
      }
    } catch {
      // server not up yet — keep polling
    }
  }
  restartStatus.value = 'error'
  restarting.value = false
}

const trusting = ref(false)
const trustStatus = ref<'idle' | 'working' | 'done' | 'error'>('idle')
const trustMessage = ref('')

async function downloadCert() {
  const res = await fetch('/api/tls/cert')
  if (!res.ok) { alert('Failed to fetch certificate'); return }
  const blob = await res.blob()
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = 'devctl-root.crt'
  a.click()
}

async function trustCert() {
  trusting.value = true
  trustStatus.value = 'working'
  trustMessage.value = ''
  try {
    const result = await trustTLS()
    trustStatus.value = 'done'
    trustMessage.value = result.output || 'Certificate trusted successfully.'
  } catch (e: unknown) {
    trustStatus.value = 'error'
    trustMessage.value = e instanceof Error ? e.message : 'Failed to trust certificate.'
  } finally {
    trusting.value = false
    setTimeout(() => { trustStatus.value = 'idle'; trustMessage.value = '' }, 8000)
  }
}
</script>

<template>
  <div class="space-y-6">
    <PageHeader title="Settings" description="Dashboard bind address, watch directory, TLS, and integrations." />

    <div v-if="store.loading" class="flex items-center justify-center gap-2 py-12 text-sm text-muted-foreground">
      <Loader2 class="size-4 animate-spin" />
      Loading…
    </div>

    <div v-else class="space-y-6">
      <!-- Dashboard -->
      <Card>
        <CardHeader>
          <CardTitle>Dashboard</CardTitle>
          <CardDescription>Address and port the devctl UI listens on.</CardDescription>
        </CardHeader>
        <CardContent class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div class="grid gap-2">
            <Label for="devctl_host">Bind host</Label>
            <Input
              id="devctl_host"
              v-model="store.settings['devctl_host']"
              class="font-mono"
              @change="save('devctl_host', store.settings['devctl_host'] ?? '')"
            />
          </div>
          <div class="grid gap-2">
            <Label for="devctl_port">Port</Label>
            <Input
              id="devctl_port"
              v-model="store.settings['devctl_port']"
              inputmode="numeric"
              class="font-mono"
              @change="save('devctl_port', store.settings['devctl_port'] ?? '')"
            />
          </div>
        </CardContent>
      </Card>

      <!-- Sites -->
      <Card>
        <CardHeader>
          <CardTitle>Sites</CardTitle>
          <CardDescription>Root directory watched for auto-discovered sites.</CardDescription>
        </CardHeader>
        <CardContent>
          <div class="grid gap-2">
            <Label for="sites_watch_dir">Watch directory</Label>
            <Input
              id="sites_watch_dir"
              v-model="store.settings['sites_watch_dir']"
              placeholder="~/Code/sites"
              class="font-mono"
              @change="save('sites_watch_dir', store.settings['sites_watch_dir'] ?? '')"
            />
            <p class="text-xs text-muted-foreground">
              <code class="font-mono">~</code> and <code class="font-mono">$HOME</code> are expanded.
              Install default: <code class="font-mono">~/ddev/sites</code> on Linux, <code class="font-mono">~/Code/sites</code> on macOS.
            </p>
          </div>
        </CardContent>
      </Card>

      <!-- TLS -->
      <Card>
        <CardHeader>
          <CardTitle>TLS</CardTitle>
          <CardDescription>Caddy internal CA root certificate management.</CardDescription>
        </CardHeader>
        <CardContent class="space-y-3">
          <div class="flex flex-wrap gap-2">
            <Button variant="outline" size="sm" @click="downloadCert">
              <Download class="size-3.5" />
              Download root certificate
            </Button>
            <Button variant="outline" size="sm" :disabled="trusting" @click="trustCert">
              <Loader2 v-if="trusting" class="size-3.5 animate-spin" />
              <ShieldCheck v-else class="size-3.5" />
              {{ trusting ? 'Trusting…' : 'Trust certificate' }}
            </Button>
          </div>
          <p v-if="trustStatus === 'done'" class="whitespace-pre-wrap text-sm text-success">{{ trustMessage }}</p>
          <p v-else-if="trustStatus === 'error'" class="whitespace-pre-wrap text-sm text-destructive">{{ trustMessage }}</p>
          <p v-else-if="trustStatus === 'working'" class="text-sm text-muted-foreground">Installing certificate into system and browser trust stores…</p>
        </CardContent>
      </Card>

      <!-- Dump Server -->
      <Card>
        <CardHeader>
          <CardTitle>PHP Dump Server</CardTitle>
          <CardDescription>TCP listener for dump() / dd() calls.</CardDescription>
        </CardHeader>
        <CardContent>
          <div class="grid gap-2 sm:max-w-xs">
            <Label for="dump_tcp_port">TCP port</Label>
            <Input
              id="dump_tcp_port"
              v-model="store.settings['dump_tcp_port']"
              inputmode="numeric"
              class="font-mono"
              @change="save('dump_tcp_port', store.settings['dump_tcp_port'] ?? '')"
            />
          </div>
        </CardContent>
      </Card>

      <!-- Save & Restart -->
      <Card>
        <CardFooter class="flex flex-col items-start gap-3 px-4 py-4 sm:px-5 sm:flex-row sm:items-center sm:justify-between">
          <p class="text-sm" :class="{
            'text-muted-foreground': restartStatus === 'idle' || restartStatus === 'restarting' || restartStatus === 'reconnecting',
            'text-success': restartStatus === 'done',
            'text-destructive': restartStatus === 'error',
          }">
            <template v-if="restartStatus === 'idle'">Changes take effect after devctl restarts.</template>
            <template v-else-if="restartStatus === 'restarting'">Restarting…</template>
            <template v-else-if="restartStatus === 'reconnecting'">Waiting for server…</template>
            <template v-else-if="restartStatus === 'done'">Restarted successfully.</template>
            <template v-else-if="restartStatus === 'error'">Server did not come back in time. Check journalctl.</template>
          </p>
          <Button size="sm" :disabled="restarting" @click="saveAndRestart">
            <RotateCw class="size-3.5" :class="restarting ? 'animate-spin' : ''" />
            Save &amp; Restart
          </Button>
        </CardFooter>
      </Card>
    </div>
  </div>
</template>
