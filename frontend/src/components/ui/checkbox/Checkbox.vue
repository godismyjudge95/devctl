<script setup lang="ts">
import type { CheckboxRootEmits, CheckboxRootProps } from "reka-ui"
import type { HTMLAttributes } from "vue"
import { computed } from "vue"
import { reactiveOmit } from "@vueuse/core"
import { Check } from "lucide-vue-next"
import { CheckboxIndicator, CheckboxRoot, useForwardPropsEmits } from "reka-ui"
import { cn } from "@/lib/utils"

type Checked = boolean | "indeterminate"

const props = defineProps<
  CheckboxRootProps & {
    class?: HTMLAttributes["class"]
    /** Used by v-model:checked throughout the app. */
    checked?: Checked
  }
>()
const emits = defineEmits<
  CheckboxRootEmits & {
    "update:checked": [value: Checked]
  }
>()

// reka-ui CheckboxRoot uses modelValue; bridge checked ↔ modelValue here.
const delegatedProps = reactiveOmit(props, "class", "modelValue", "checked")

const forwarded = useForwardPropsEmits(delegatedProps, emits)

const rootModel = computed<Checked>({
  get: () => props.checked ?? props.modelValue ?? false,
  set: (value) => {
    emits("update:checked", value)
    emits("update:modelValue", value)
  },
})
</script>

<template>
  <CheckboxRoot
    v-slot="slotProps"
    data-slot="checkbox"
    v-bind="forwarded"
    v-model="rootModel"
    :class="
      cn('peer border-input data-[state=checked]:bg-primary data-[state=checked]:text-primary-foreground data-[state=checked]:border-primary focus-visible:border-ring focus-visible:ring-ring/50 aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive size-4 shrink-0 rounded-[4px] border shadow-xs transition-shadow outline-none focus-visible:ring-[3px] disabled:cursor-not-allowed disabled:opacity-50',
         props.class)"
  >
    <CheckboxIndicator
      data-slot="checkbox-indicator"
      class="grid place-content-center text-current transition-none"
    >
      <slot v-bind="slotProps">
        <Check class="size-3.5" />
      </slot>
    </CheckboxIndicator>
  </CheckboxRoot>
</template>