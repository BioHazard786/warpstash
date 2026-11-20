/** Copy text; true on success. Falls back to execCommand because navigator.clipboard is missing on plain-HTTP origins (self-hosted on a LAN). */
export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    const ta = document.createElement("textarea")
    ta.value = text
    ta.style.cssText = "position:fixed;opacity:0"
    document.body.append(ta)
    ta.select()
    try {
      return document.execCommand("copy")
    } catch {
      return false
    } finally {
      ta.remove()
    }
  }
}
