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
  <div
    class="hidden md:block w-1.5 shrink-0 cursor-col-resize bg-border hover:bg-primary/40 active:bg-primary/60 transition-colors"
    role="separator"
    aria-orientation="vertical"
    @pointerdown.prevent="onPointerDown"
  />
</template>
