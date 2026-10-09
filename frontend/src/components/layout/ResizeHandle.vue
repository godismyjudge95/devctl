<script setup lang="ts">
const props = defineProps<{
  storageKey: string
  defaultWidth: number
  min: number
  max: number
  reverse?: boolean
}>()

const emit = defineEmits<{
  'update:width': [value: number]
}>()

function onPointerDown(e: PointerEvent) {
  const startX = e.clientX
  const startWidth = props.defaultWidth
  const el = e.currentTarget as HTMLElement
  el.setPointerCapture(e.pointerId)

  function move(ev: PointerEvent) {
    const delta = props.reverse ? startX - ev.clientX : ev.clientX - startX
    const next = Math.min(props.max, Math.max(props.min, startWidth + delta))
    emit('update:width', next)
  }
  function up() {
    window.removeEventListener('pointermove', move)
    window.removeEventListener('pointerup', up)
  }
  window.addEventListener('pointermove', move)
  window.addEventListener('pointerup', up)
}
</script>

<template>
  <!-- 1px divider with a wider invisible hit area. -->
  <div
    class="group relative hidden w-px shrink-0 cursor-col-resize bg-border md:block"
    role="separator"
    aria-orientation="vertical"
    @pointerdown.prevent="onPointerDown"
  >
    <span class="absolute inset-y-0 -left-1.5 -right-1.5 z-10" />
    <span class="absolute inset-y-0 -left-px -right-px bg-ring/0 transition-colors group-hover:bg-ring/60 group-active:bg-ring" />
  </div>
</template>
