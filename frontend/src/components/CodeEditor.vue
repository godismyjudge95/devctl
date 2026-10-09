<script setup lang="ts">
import { ref, watch, onMounted, onBeforeUnmount } from 'vue'
import { EditorView, basicSetup } from 'codemirror'
import { EditorState } from '@codemirror/state'
import { StreamLanguage, syntaxHighlighting } from '@codemirror/language'
import { properties } from '@codemirror/legacy-modes/mode/properties'
import { toml } from '@codemirror/legacy-modes/mode/toml'
import { standardSQL } from '@codemirror/legacy-modes/mode/sql'
import { oneDarkHighlightStyle } from '@codemirror/theme-one-dark'
import { useDarkMode } from '@/composables/useDarkMode'

const props = defineProps<{
  modelValue: string
  language?: 'ini' | 'toml' | 'text' | 'sql'
  readonly?: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const { isDark } = useDarkMode()
const container = ref<HTMLElement | null>(null)
let view: EditorView | null = null

function buildExtensions() {
  const exts = [basicSetup]

  if (props.language === 'ini') {
    exts.push(StreamLanguage.define(properties))
  } else if (props.language === 'toml') {
    exts.push(StreamLanguage.define(toml))
  } else if (props.language === 'sql') {
    exts.push(StreamLanguage.define(standardSQL))
  }

  // Chrome colours come from the app theme tokens so the editor matches the
  // surrounding surface; only syntax colours differ between light and dark.
  exts.push(
    EditorView.theme({
      '&': { background: 'var(--card)', color: 'var(--foreground)' },
      '.cm-scroller': { fontFamily: "'Geist Mono', ui-monospace, monospace", fontSize: '13px', lineHeight: '1.6' },
      '.cm-gutters': { background: 'var(--card)', color: 'var(--muted-foreground)', borderRight: '1px solid var(--border)' },
      '.cm-activeLineGutter': { background: 'var(--muted)', color: 'var(--foreground)' },
      '.cm-activeLine': { background: 'color-mix(in oklch, var(--muted) 60%, transparent)' },
      '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--foreground)' },
      '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection': {
        background: 'color-mix(in oklch, var(--ring) 30%, transparent)',
      },
      '&.cm-focused': { outline: 'none' },
    }, { dark: isDark.value }),
  )
  if (isDark.value) exts.push(syntaxHighlighting(oneDarkHighlightStyle))

  if (props.readonly) {
    exts.push(EditorState.readOnly.of(true))
  } else {
    exts.push(
      EditorView.updateListener.of((update) => {
        if (update.docChanged) {
          emit('update:modelValue', update.state.doc.toString())
        }
      })
    )
  }

  return exts
}

function createView() {
  if (!container.value) return
  view = new EditorView({
    state: EditorState.create({
      doc: props.modelValue,
      extensions: buildExtensions(),
    }),
    parent: container.value,
  })
}

function destroyView() {
  if (view) {
    view.destroy()
    view = null
  }
}

// Rebuild the editor when dark mode changes (theme affects extensions)
watch(isDark, () => {
  const content = view?.state.doc.toString() ?? props.modelValue
  destroyView()
  // Temporarily override modelValue with current content for rebuild
  const prevModelValue = props.modelValue
  // Recreate with current content
  if (container.value) {
    view = new EditorView({
      state: EditorState.create({
        doc: content,
        extensions: buildExtensions(),
      }),
      parent: container.value,
    })
    // If the doc content differs from the prop (user typed), emit to sync
    if (content !== prevModelValue) {
      emit('update:modelValue', content)
    }
  }
})

// Sync external modelValue changes to the editor (e.g. when loading a new file)
watch(
  () => props.modelValue,
  (newVal) => {
    if (!view) return
    const currentVal = view.state.doc.toString()
    if (newVal === currentVal) return
    view.dispatch({
      changes: { from: 0, to: currentVal.length, insert: newVal },
    })
  }
)

onMounted(() => {
  createView()
})

onBeforeUnmount(() => {
  destroyView()
})
</script>

<template>
  <div ref="container" class="code-editor-container" />
</template>

<style scoped>
.code-editor-container {
  height: 100%;
  overflow: hidden;
}

.code-editor-container :deep(.cm-editor) {
  height: 100%;
}

.code-editor-container :deep(.cm-scroller) {
  overflow: auto;
  height: 100%;
}
</style>
