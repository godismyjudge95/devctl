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

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`
}

export const defaultEditorUrl = 'vscode://file/{file}:{line}'

// editorUrl fills an editor URL template (the editor_url setting), e.g.
// vscode://file/{file}:{line} or phpstorm://open?file={file}&line={line}.
export function editorUrl(template: string | undefined, file: string, line: number): string {
  return (template || defaultEditorUrl).split('{file}').join(encodeURI(file)).split('{line}').join(String(line))
}
