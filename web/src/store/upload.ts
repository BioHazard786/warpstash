import { isImage } from "@/components/file-icon"
import { upload, type UploadOpts, type Uploaded } from "@/lib/api"
import { newFiles } from "@/lib/files"
import { SpeedMeter } from "@/lib/format"
import { addHistory } from "@/lib/history"
import { create } from "zustand"

export type Status = "queued" | "uploading" | "done" | "error"

export type Item = {
  id: string
  file: File
  /** blob URL thumbnail for images */
  preview?: string
  status: Status
  loaded: number
  speed: number
  error?: string
  result?: Uploaded
}

type Limits = { maxFiles: number; maxBytes: number; maxHuman: string }

type UploadState = {
  items: Item[]
  /** Queue files; returns why some were refused, if any. */
  add: (files: File[], limits: Limits) => string | undefined
  remove: (id: string) => void
  clear: () => void
  /** Upload every queued or failed item, one at a time. */
  uploadAll: (opts: UploadOpts) => Promise<void>
  retry: (id: string, opts: UploadOpts) => Promise<void>
}

const aborts = new Map<string, () => void>()
let seq = 0

export const useUpload = create<UploadState>()((set, get) => {
  const patch = (id: string, p: Partial<Item>) =>
    set((s) => ({ items: s.items.map((i) => (i.id === id ? { ...i, ...p } : i)) }))

  const drop = (item: Item) => {
    aborts.get(item.id)?.()
    if (item.preview) URL.revokeObjectURL(item.preview)
  }

  async function uploadOne(id: string, opts: UploadOpts) {
    const item = get().items.find((i) => i.id === id)
    if (!item) return
    patch(id, { status: "uploading", loaded: 0, speed: 0, error: undefined })
    const meter = new SpeedMeter()
    const { promise, abort } = upload(item.file, opts, (loaded) =>
      patch(id, { loaded, speed: meter.sample(loaded) }),
    )
    aborts.set(id, abort)
    try {
      const result = await promise
      patch(id, { status: "done", loaded: item.file.size, result })
      if (!result.is_burn_on_read)
        addHistory({
          id: result.id,
          name: result.name,
          size: result.size,
          url: result.url,
          deleteToken: result.delete_token,
          expiresAt: result.expires_at,
          createdAt: result.created_at,
        })
    } catch (e) {
      // aborted means the item was removed; patch is a no-op then anyway
      if (!(e instanceof DOMException && e.name === "AbortError"))
        patch(id, { status: "error", error: (e as Error).message })
    } finally {
      aborts.delete(id)
    }
  }

  return {
    items: [],

    add(files, { maxFiles, maxBytes, maxHuman }) {
      const fresh = newFiles(
        get().items.map((i) => i.file),
        files,
      )
      const fits = fresh.filter((f) => f.size <= maxBytes)
      const room = Math.max(0, maxFiles - get().items.length)
      const accepted = fits.slice(0, room)
      set((s) => ({
        items: [
          ...s.items,
          ...accepted.map((file) => ({
            id: String(++seq),
            file,
            preview: isImage(file.name, file.type) ? URL.createObjectURL(file) : undefined,
            status: "queued" as const,
            loaded: 0,
            speed: 0,
          })),
        ],
      }))
      if (fits.length < fresh.length)
        return `${fresh.length - fits.length} file(s) exceed the ${maxHuman} limit.`
      if (accepted.length < fits.length) return `You can upload up to ${maxFiles} files at once.`
    },

    remove(id) {
      const item = get().items.find((i) => i.id === id)
      if (item) drop(item)
      set((s) => ({ items: s.items.filter((i) => i.id !== id) }))
    },

    clear() {
      get().items.forEach(drop)
      set({ items: [] })
    },

    async uploadAll(opts) {
      for (const { id } of get().items) {
        // re-read: the user may have removed it (or retried it) while an earlier file uploaded
        const cur = get().items.find((i) => i.id === id)
        if (cur && (cur.status === "queued" || cur.status === "error")) await uploadOne(id, opts)
      }
    },

    retry: uploadOne,
  }
})
