<script setup lang="ts">
import { computed } from 'vue'
import {
  Play, CircleStop, RotateCcw, Loader2, Trash2, Settings2, FileText,
  ArrowUpCircle, MoreHorizontal,
} from 'lucide-vue-next'
import type { ServiceState } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { ButtonGroup } from '@/components/ui/button-group'
import {
  Tooltip, TooltipContent, TooltipProvider, TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem,
  DropdownMenuSeparator, DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

// Row actions for one service. `compact` renders icon-only buttons (mobile cards).
const props = defineProps<{
  svc: ServiceState
  busy?: string
  installing?: boolean
  updating?: boolean
  compact?: boolean
  hasSettings: boolean
  hasConfig: boolean
  canUninstall: boolean
}>()

const emit = defineEmits<{
  start: []
  stop: []
  restart: []
  update: []
  settings: []
  config: []
  uninstall: []
}>()

const size = computed(() => props.compact ? 'icon-sm' : 'sm')
const locked = computed(() => !!props.busy || !!props.installing)
const running = computed(() => props.svc.status === 'running')
</script>

<template>
  <ButtonGroup>
    <Button
      v-if="!running"
      variant="outline" :size="size"
      :disabled="locked"
      :title="`Start ${svc.label}`"
      :aria-label="`Start ${svc.label}`"
      @click="emit('start')"
    >
      <Loader2 v-if="busy === 'start'" class="size-3.5 animate-spin" />
      <Play v-else class="size-3.5" />
      <template v-if="!compact">Start</template>
    </Button>
    <Button
      v-if="running && !svc.required"
      variant="outline" :size="size"
      :disabled="locked"
      :title="`Stop ${svc.label}`"
      :aria-label="`Stop ${svc.label}`"
      @click="emit('stop')"
    >
      <Loader2 v-if="busy === 'stop'" class="size-3.5 animate-spin" />
      <CircleStop v-else class="size-3.5" />
      <template v-if="!compact">Stop</template>
    </Button>
    <Button
      v-if="running"
      variant="outline" :size="size"
      :disabled="locked"
      :title="`Restart ${svc.label}`"
      :aria-label="`Restart ${svc.label}`"
      @click="emit('restart')"
    >
      <Loader2 v-if="busy === 'restart'" class="size-3.5 animate-spin" />
      <RotateCcw v-else class="size-3.5" />
      <template v-if="!compact">Restart</template>
    </Button>
    <TooltipProvider v-if="svc.update_available">
      <Tooltip>
        <TooltipTrigger as-child>
          <Button
            variant="outline" :size="size"
            class="text-warning-soft-foreground hover:bg-warning-soft hover:text-warning-soft-foreground"
            :disabled="!!busy || updating"
            :aria-label="`Update ${svc.label}`"
            @click="emit('update')"
          >
            <Loader2 v-if="busy === 'update' || updating" class="size-3.5 animate-spin" />
            <ArrowUpCircle v-else class="size-3.5" />
            <template v-if="!compact">Update</template>
          </Button>
        </TooltipTrigger>
        <TooltipContent>
          Update from {{ svc.version || svc.install_version }} to {{ svc.latest_version }}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
    <DropdownMenu>
      <DropdownMenuTrigger as-child>
        <Button variant="outline" size="icon-sm" :aria-label="`More actions for ${svc.label}`">
          <MoreHorizontal class="size-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem v-if="hasSettings" @click="emit('settings')">
          <Settings2 class="size-4" />
          Settings
        </DropdownMenuItem>
        <DropdownMenuItem v-if="hasConfig" @click="emit('config')">
          <FileText class="size-4" />
          Edit config
        </DropdownMenuItem>
        <template v-if="canUninstall">
          <DropdownMenuSeparator v-if="hasSettings || hasConfig" />
          <DropdownMenuItem
            class="text-destructive focus:text-destructive"
            :disabled="locked"
            @click="emit('uninstall')"
          >
            <Loader2 v-if="busy === 'uninstall'" class="size-4 animate-spin" />
            <Trash2 v-else class="size-4" />
            Uninstall
          </DropdownMenuItem>
        </template>
      </DropdownMenuContent>
    </DropdownMenu>
  </ButtonGroup>
</template>
