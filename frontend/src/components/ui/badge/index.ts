import type { VariantProps } from "class-variance-authority"
import { cva } from "class-variance-authority"

export { default as Badge } from "./Badge.vue"

export const badgeVariants = cva(
  "inline-flex items-center justify-center rounded-md border px-1.5 py-0.5 text-[11px] font-medium w-fit whitespace-nowrap shrink-0 [&>svg]:size-3 gap-1 [&>svg]:pointer-events-none focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px] aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive transition-[color,box-shadow] overflow-hidden",
  {
    variants: {
      variant: {
        default:
          "border-transparent bg-foreground text-background [a&]:hover:bg-foreground/90",
        secondary:
          "border-transparent bg-muted text-muted-foreground [a&]:hover:bg-muted/90",
        destructive:
         "border-transparent bg-destructive/10 text-destructive [a&]:hover:bg-destructive/15",
        outline:
          "text-muted-foreground border-border [a&]:hover:bg-muted [a&]:hover:text-foreground",
        success:
          "border-transparent bg-[oklch(0.94_0.04_150)] text-[oklch(0.38_0.11_150)] dark:bg-[oklch(0.28_0.05_150)] dark:text-[oklch(0.82_0.08_150)]",
        warning:
          "border-transparent bg-[oklch(0.95_0.04_75)] text-[oklch(0.42_0.10_75)] dark:bg-[oklch(0.30_0.05_75)] dark:text-[oklch(0.86_0.08_75)]",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  },
)
export type BadgeVariants = VariantProps<typeof badgeVariants>
