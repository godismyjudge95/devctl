import type { ClassValue } from "clsx"
import { clsx } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

const ansiPattern = /[\u001B\u009B][[\]()#;?]*(?:(?:(?:\d{1,4}(?:;\d{0,4})*)?[0-9A-ORZcf-ntqry=><~])|(?:[\dA-PR-TZcf-ntqry=><~]))/g

export function normalizeLogChunk(text: string): string {
  return text.replace(ansiPattern, '').replace(/\r/g, '')
}
