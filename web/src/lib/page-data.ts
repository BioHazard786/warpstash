/** What the Go server injected into <script id="page-data"> (see backend/internal/api/spa.go). */
export type PageData = {
  status?: number
  burn?: { id: string; name: string; size: number }
}

export const pageData: PageData = (() => {
  try {
    return JSON.parse(document.getElementById("page-data")?.textContent || "null") ?? {}
  } catch {
    return {}
  }
})()
