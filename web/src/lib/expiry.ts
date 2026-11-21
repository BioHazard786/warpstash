const UNIT: Record<string, number> = { s: 1, m: 60, h: 3600, d: 86400, w: 604800 }
const MIN_SECONDS = 60

/** Seconds in an expiry like "90m", "7d", "2w" or "1d12h" (the same grammar the server accepts), else null. */
export function parseExpiry(text: string): number | null {
  let rest = text.trim().toLowerCase()
  if (!rest) return null
  let total = 0
  while (rest) {
    const m = /^(\d+)([smhdw])/.exec(rest)
    if (!m) return null
    total += Number(m[1]) * UNIT[m[2]!]!
    rest = rest.slice(m[0].length)
  }
  return total
}

/** What's wrong with a custom expiry, or "" when it's fine. `max` is the server's cap in seconds (0 = none). */
export function expiryError(text: string, max: number): string {
  const s = parseExpiry(text)
  if (s === null) return "Use a number and a unit: 90m, 12h, 5d, 2w"
  if (s < MIN_SECONDS) return "At least 1 minute"
  if (max > 0 && s > max) return `At most ${formatDuration(max)}`
  return ""
}

/** "30 days", "12 hours", "1 week": the largest unit that divides evenly, for the cap hint. */
export function formatDuration(seconds: number): string {
  for (const [name, size] of [
    ["week", 604800],
    ["day", 86400],
    ["hour", 3600],
    ["minute", 60],
  ] as const) {
    if (seconds >= size && seconds % size === 0) {
      const n = seconds / size
      return `${n} ${name}${n === 1 ? "" : "s"}`
    }
  }
  return `${seconds} seconds`
}
