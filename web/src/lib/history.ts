import { useMemo, useSyncExternalStore } from "react"

/** Non-burn uploads from this browser. Stored as a bare JSON array under the same key the Astro UI used. */
export type HistoryRecord = {
  id: string
  name: string
  size: number
  url: string
  deleteToken: string
  expiresAt: string
  createdAt: string
}

const KEY = "warpstash_history"
const EVENT = "warpstash:history"
const MAX = 50

/** Valid, unexpired records from the stored JSON. */
export function parseHistory(raw: string | null, now = Date.now()): HistoryRecord[] {
  let list: unknown
  try {
    list = JSON.parse(raw || "[]")
  } catch {
    return []
  }
  if (!Array.isArray(list)) return []
  return list.filter((r): r is HistoryRecord => {
    if (!r || typeof r.id !== "string" || typeof r.url !== "string") return false
    const exp = new Date(r.expiresAt).getTime()
    return isNaN(exp) || exp > now
  })
}

const read = () => parseHistory(localStorage.getItem(KEY))

function write(list: HistoryRecord[]) {
  localStorage.setItem(KEY, JSON.stringify(list.slice(0, MAX)))
  dispatchEvent(new Event(EVENT))
}

export const addHistory = (r: HistoryRecord) => write([r, ...read().filter((x) => x.id !== r.id)])
export const removeHistory = (id: string) => write(read().filter((r) => r.id !== id))
export const clearHistory = () => write([])

function subscribe(cb: () => void) {
  addEventListener(EVENT, cb)
  addEventListener("storage", cb) // other tabs
  return () => {
    removeEventListener(EVENT, cb)
    removeEventListener("storage", cb)
  }
}

export function useHistory(): HistoryRecord[] {
  const raw = useSyncExternalStore(subscribe, () => localStorage.getItem(KEY))
  return useMemo(() => parseHistory(raw), [raw])
}
