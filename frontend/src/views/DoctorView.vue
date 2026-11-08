<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { AlertTriangle, CheckCircle2, Copy, Loader2, RefreshCw, Wand2, XCircle } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { Button } from '@/components/ui/button'
import PageHeader from '@/components/layout/PageHeader.vue'
import Surface from '@/components/layout/Surface.vue'
import SectionHeader from '@/components/layout/SectionHeader.vue'
import StatCard from '@/components/layout/StatCard.vue'
import type { DoctorSeverity } from '@/lib/api'
import { useDoctorStore } from '@/stores/doctor'

const store = useDoctorStore()

onMounted(() => {
  run()
})

const rank: Record<DoctorSeverity, number> = { fail: 0, warn: 1, ok: 2 }
const sorted = computed(() =>
  store.checks
    .map((c, i) => ({ c, i }))
    .sort((a, b) => rank[a.c.severity] - rank[b.c.severity] || a.i - b.i)
    .map(({ c }) => c),
)

const busy = computed(() => store.loading || store.fixing.size > 0)

async function run() {
  try {
    await store.run()
  } catch (e: any) {
    toast.error('Doctor failed to run', { description: e.message })
  }
}

async function fix(id?: string) {
  try {
    const fixes = await store.fix(id)
    const failed = fixes.filter(f => f.error)
    if (failed.length) {
      toast.error(`${failed.length} fix${failed.length === 1 ? '' : 'es'} failed`, {
        description: failed.map(f => `${f.id}: ${f.error}`).join('\n'),
      })
    } else if (fixes.length) {
      toast.success(`Applied ${fixes.length} fix${fixes.length === 1 ? '' : 'es'}`)
    } else {
      toast.info('Nothing to fix')
    }
  } catch (e: any) {
    toast.error('Fix failed', { description: e.message })
  }
}

function copy(value: string) {
  navigator.clipboard.writeText(value).then(
    () => toast.success('Copied to clipboard'),
    () => toast.error('Failed to copy'),
  )
}

// Remedies that are shell commands get a copy button; prose does not.
function isCommand(remedy: string) {
  return /^(sudo |devctl |launchctl )/.test(remedy)
}
</script>

<template>
  <div class="space-y-6">
    <PageHeader title="Doctor" description="Checks that Caddy, DNS, TLS, PHP, and your sites are healthy. Safe fixes run without root.">
      <template #actions>
        <Button variant="outline" size="sm" :disabled="busy" @click="run()">
          <Loader2 v-if="store.loading" class="size-3.5 animate-spin" />
          <RefreshCw v-else class="size-3.5" />
          Run again
        </Button>
        <Button v-if="store.fixable.length" size="sm" :disabled="busy" @click="fix()">
          <Loader2 v-if="store.fixing.has('*')" class="size-3.5 animate-spin" />
          <Wand2 v-else class="size-3.5" />
          Fix all ({{ store.fixable.length }})
        </Button>
      </template>
    </PageHeader>

    <div class="grid grid-cols-3 gap-3">
      <StatCard label="Failed" :value="store.failed" />
      <StatCard label="Warnings" :value="store.warnings" />
      <StatCard label="Passed" :value="store.passed" />
    </div>

    <Surface class="overflow-hidden">
      <SectionHeader title="Checks" description="Failures first. A command marked sudo needs root; run it in a terminal." />
      <div v-if="store.loading && !store.checks.length" class="flex items-center gap-2 border-t border-border px-5 py-6 text-sm text-muted-foreground">
        <Loader2 class="size-4 animate-spin" />
        Running checks…
      </div>
      <ul class="divide-y divide-border border-t border-border">
        <li
          v-for="c in sorted"
          :key="c.id"
          class="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-start sm:justify-between sm:px-5"
          :data-check="c.id"
          :data-severity="c.severity"
        >
          <div class="flex min-w-0 gap-3">
            <XCircle v-if="c.severity === 'fail'" class="mt-0.5 size-4 shrink-0 text-destructive" aria-label="failed" />
            <AlertTriangle v-else-if="c.severity === 'warn'" class="mt-0.5 size-4 shrink-0 text-warning" aria-label="warning" />
            <CheckCircle2 v-else class="mt-0.5 size-4 shrink-0 text-success" aria-label="ok" />
            <div class="min-w-0 space-y-1">
              <p class="text-sm font-medium">{{ c.title }}</p>
              <p class="break-words text-sm text-muted-foreground">{{ c.message }}</p>
              <ul v-if="c.details?.length" class="space-y-0.5 font-mono text-xs text-muted-foreground">
                <li v-for="d in c.details" :key="d" class="break-all">{{ d }}</li>
              </ul>
              <div v-if="c.severity !== 'ok' && c.remedy" class="flex items-center gap-1.5 pt-1">
                <code v-if="isCommand(c.remedy)" class="rounded bg-muted px-1.5 py-0.5 font-mono text-xs break-all">{{ c.remedy }}</code>
                <span v-else class="text-xs text-muted-foreground">{{ c.remedy }}</span>
                <Button
                  v-if="isCommand(c.remedy)"
                  variant="ghost"
                  size="icon-sm"
                  :aria-label="`Copy command for ${c.title}`"
                  @click="copy(c.remedy)"
                >
                  <Copy class="size-3.5" />
                </Button>
              </div>
            </div>
          </div>
          <Button
            v-if="c.fixable"
            variant="outline"
            size="sm"
            class="shrink-0 self-start"
            :disabled="busy"
            @click="fix(c.id)"
          >
            <Loader2 v-if="store.fixing.has(c.id)" class="size-3.5 animate-spin" />
            <Wand2 v-else class="size-3.5" />
            Fix
          </Button>
        </li>
      </ul>
    </Surface>
  </div>
</template>
