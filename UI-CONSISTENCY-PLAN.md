# UI Consistency Plan

Comprehensive plan to make the devctl frontend visually consistent and professional.
**Status: COMPLETE**

---

## Design Decisions

| Decision | Choice |
|---|---|
| Sidebar nav unread badges (Dumps/Mail/SPX) | `variant="secondary"` (not destructive red) |
| Folder icon color | `text-primary` (indigo, replaces `text-yellow-500`) |
| Dump syntax highlight colors | Add `dark:` variants to all hardcoded palette colors |
| Log terminal theme | Replace `bg-neutral-950 text-green-400` with design-system colors |
| Dialog footer standard | `<DialogFooter class="gap-2">` with `size="sm"` buttons (no ButtonGroup) |

---

## Step 1 — Design Token Additions (`frontend/src/style.css`)

Add two new semantic CSS variables:

### `--success`
Used for success messages in SettingsView (trust cert, restart success), replacing `text-green-600`.

```css
/* :root */
--success: oklch(0.55 0.15 145);
--success-foreground: oklch(0.985 0 0);

/* .dark */
--success: oklch(0.70 0.15 145);
--success-foreground: oklch(0.1 0.01 265);
```

Wire into `@theme inline`:
```css
--color-success: var(--success);
--color-success-foreground: var(--success-foreground);
```

---

## Step 2 — Button CVA: Add `icon-xs` size token

**File:** `frontend/src/components/ui/button/index.ts`

Add to the `size` variants:
```ts
"icon-xs": "size-7",
```

This eliminates all `size="icon" class="h-7 w-7"` manual overrides across 10+ files.

---

## Step 3 — Per-file changes

### `App.vue`
- [ ] Dark mode toggle buttons (sidebar + mobile header): `size="icon" class="h-7 w-7"` → `size="icon-xs"`
- [ ] Sidebar nav badges: Dumps, Mail, SPX unread counts → `variant="secondary"`

### `ServicesView.vue`
- [ ] Mobile action buttons: `size="icon" class="h-8 w-8"` → `size="icon-sm"`
- [ ] Credential copy buttons: standardize all to `size="icon-sm"` (currently mix of `h-7 w-7` and `h-6 w-6`)

### `SitesView.vue`
- [ ] Desktop table wrapper: `rounded-md border` → `rounded-lg border border-border overflow-hidden`
- [ ] Mobile cards: raw `<div class="rounded-lg border p-3 ...">` → `<Card><CardContent class="p-4">`
- [ ] Mobile empty state dashed border: add `border-border` class

### `SettingsView.vue`
- [ ] Redis connection type badge: `variant="destructive"` → `variant="secondary"`
- [ ] WhoDB edit/delete buttons: `size="icon" class="h-7 w-7"` → `size="icon-xs"`
- [ ] Trust cert success message: `text-green-600` → `text-success`
- [ ] Restart success message: `text-green-600` → `text-success`
- [ ] Connection dialog footer: wrap buttons in `<DialogFooter class="gap-2">` with `size="sm"`
- [ ] Checkbox label gap: `gap-3` → `gap-2`

### `LogsView.vue`
- [ ] Refresh button: `size="icon" class="h-6 w-6"` → `size="icon-sm"`
- [ ] Mobile back button: icon-only ArrowLeft → `size="sm"` with ArrowLeft icon + "Back" text
- [ ] Active log item: add `border-l-2 border-l-primary` to the active highlight class

### `SpxView.vue`
- [ ] Toolbar delete-all button: `size="icon" class="h-7 w-7"` → `size="icon-xs"`
- [ ] Per-profile delete button: `size="icon" class="h-6 w-6"` → `size="icon-sm"`
- [ ] All `hover:bg-accent/30` → `hover:bg-accent/50` (flat table rows + all 9 metadata tab rows)

### `MailView.vue`
- [ ] Toolbar buttons (4×): `size="icon" class="h-7 w-7"` → `size="icon-xs"`
- [ ] Pagination buttons (2×): `size="icon" class="h-6 w-6"` → `size="icon-sm"`
- [ ] Headers tab table rows: `hover:bg-accent/30` → `hover:bg-accent/50`
- [ ] Detail header toolbar: flatten nested `<ButtonGroup>` inside `<ButtonGroup>` → single `<ButtonGroup>` with two `<Button>` siblings (or plain `<div class="flex items-center gap-1.5">`)

### `RustFSView.vue`
- [ ] Mobile back button: icon-only ArrowLeft → `size="sm"` with ArrowLeft icon + "Back" text
- [ ] Bucket/object row dropdown triggers: `size="icon" class="h-6 w-6"` → `size="icon-sm"`
- [ ] Toolbar buttons: `size="icon" class="h-7 w-7"` → `size="icon-xs"`
- [ ] Table rows: `hover:bg-accent/30` → `hover:bg-accent/50` (folder row + object row)
- [ ] Folder icons: `text-yellow-500` → `text-primary` (TreeNodeRow render function + table row)
- [ ] `AlertDialogAction`: remove redundant manual `bg-destructive text-destructive-foreground hover:bg-destructive/90` classes
- [ ] Search clear: raw `<button>` → `<Button variant="ghost" size="icon-sm">`

### `ServiceInstallModal.vue`
- [ ] Service icon container: `bg-white` → `bg-background`
- [ ] Install/action button: `variant="outline"` → `variant="default"`

### `ServiceSettingsDialog.vue`
- [ ] Footer buttons: remove `<ButtonGroup>` wrappers → `<DialogFooter class="gap-2">` with `size="sm"` on Cancel + Save buttons
- [ ] DNS Remove button: `variant="outline" class="text-destructive hover:text-destructive"` → `variant="destructive"`

### `SiteSettingsDialog.vue`
- [ ] Trigger button: `size="sm" class="h-7 w-7 p-0"` → `size="icon-xs"`

### `ServiceLogSheet.vue`
- [ ] Terminal container: `bg-neutral-950 text-green-400` → `bg-muted text-foreground font-mono text-sm`
- [ ] Log text: keep `font-mono text-sm` on individual lines
- [ ] Empty state placeholder: `text-neutral-500` → `text-muted-foreground`
- [ ] Error lines: `text-red-400` → `text-destructive`

### `ConfigEditorView.vue`
- [ ] Save button icon spacing: `mr-1.5` → `mr-2`
- [ ] Header divider: `border-b` → `border-b border-border`

### `DumpNode.vue`
- [ ] Add `dark:` variants to all hardcoded syntax highlight colors:
  - `text-purple-600` → `text-purple-600 dark:text-purple-400`
  - `text-blue-600` → `text-blue-600 dark:text-blue-400`
  - `text-blue-500` → `text-blue-500 dark:text-blue-300`
  - `text-blue-700` → `text-blue-700 dark:text-blue-500`
  - `text-green-600` → `text-green-600 dark:text-green-400`
  - `text-amber-600` → `text-amber-600 dark:text-amber-400`
  - `text-amber-500` → `text-amber-500 dark:text-amber-300`
  - `text-red-600` → `text-red-600 dark:text-red-400`
  - `text-gray-400` → `text-gray-400 dark:text-gray-500`
  - `text-gray-500` → `text-gray-500 dark:text-gray-400`

---

## Step 4 — Build & Test

- [ ] `make build-ui` — verify no TypeScript or Vite errors
- [ ] `make install` — build Go binary + install
- [ ] `sudo systemctl restart devctl`
- [ ] Browser test all affected views

---

## Completion Checklist

- [ ] Step 1: Design tokens added to `style.css`
- [ ] Step 2: `icon-xs` token added to Button CVA
- [ ] Step 3: All per-file changes applied
- [ ] Step 4: Build passes, browser test complete
