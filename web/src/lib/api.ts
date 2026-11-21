/** Server limits from GET /api/info; read at runtime so the UI always matches the server's config. */
export type Info = {
  version: string
  max_file_size_mb: number
  max_file_size_human: string
  max_files: number
  /** Longest custom expiry the server accepts, in seconds; 0 means no cap. */
  max_expiry_seconds: number
  allowed_expiries: string[]
  default_expiry: string
  auth_required: boolean
}

export type Uploaded = {
  id: string
  name: string
  size: number
  url: string
  delete_token: string
  created_at: string
  expires_at: string
  is_burn_on_read: boolean
  password_protected: boolean
}

/** Public details of a file, from GET /api/files/{id}. Reading them never burns the file. */
export type FileMeta = {
  id: string
  burn: boolean
  password: boolean
  expires_at: string
  /** Withheld while the file is password protected: they come back from verifyPassword. */
  name?: string
  size?: number
}

/** Check a password without downloading (or burning) anything; returns the file's name and size. */
export async function verifyPassword(
  file: string,
  password: string,
): Promise<{ name: string; size: number }> {
  const res = await fetch(`/api/files/${encodeURIComponent(file)}/verify`, {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify({ password }),
  })
  if (!res.ok) throw new Error(await errorText(res))
  return res.json()
}

/** The file's details, or the HTTP status when it's gone (410) or unknown (404). */
export async function fetchMeta(file: string): Promise<FileMeta | number> {
  const res = await fetch(`/api/files/${encodeURIComponent(file)}`, {
    headers: { Accept: "application/json" },
  })
  if (res.status === 404 || res.status === 410) return res.status
  if (!res.ok) throw new Error(await errorText(res))
  return res.json()
}

export async function fetchInfo(): Promise<Info> {
  const res = await fetch("/api/info", { headers: { Accept: "application/json" } })
  if (!res.ok) throw new Error(`Server returned HTTP ${res.status}`)
  return res.json()
}

/** Error text from a JSON `{error}` body, else plain text, else the status. */
async function errorText(res: Response): Promise<string> {
  const text = (await res.text().catch(() => "")).trim()
  try {
    return JSON.parse(text).error || `HTTP ${res.status}`
  } catch {
    return text || `HTTP ${res.status}`
  }
}

export type UploadOpts = { expiry: string; token: string; password: string }

/** POST one file. XHR rather than fetch because fetch has no upload progress. */
export function upload(
  file: File,
  { expiry, token, password }: UploadOpts,
  onProgress: (loaded: number) => void,
): { promise: Promise<Uploaded>; abort: () => void } {
  const xhr = new XMLHttpRequest()
  const promise = new Promise<Uploaded>((resolve, reject) => {
    xhr.upload.onprogress = (e) => onProgress(e.loaded)
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          return resolve(JSON.parse(xhr.responseText))
        } catch {
          return reject(new Error("Unexpected server response"))
        }
      }
      if (xhr.status === 401) return reject(new Error("Wrong or missing upload token"))
      let msg = `HTTP ${xhr.status}`
      try {
        msg = JSON.parse(xhr.responseText).error || msg
      } catch {}
      reject(new Error(msg))
    }
    xhr.onerror = () => reject(new Error("Network failure"))
    xhr.onabort = () => reject(new DOMException("Upload cancelled", "AbortError"))
  })
  const form = new FormData()
  form.append("time", expiry) // fields before the file: the server reads the multipart stream in order
  if (password) form.append("password", password)
  form.append("file", file)
  xhr.open("POST", "/api/upload")
  xhr.setRequestHeader("Accept", "application/json")
  if (token) xhr.setRequestHeader("X-Auth-Token", token)
  xhr.send(form)
  return { promise, abort: () => xhr.abort() }
}

/** Delete early with the token from the upload response. Already gone (404/410) counts as success. */
export async function deleteFile(id: string, token: string): Promise<void> {
  const res = await fetch(`/api/files/${encodeURIComponent(id)}`, {
    method: "DELETE",
    headers: { "X-Delete-Token": token },
  })
  if (!res.ok && res.status !== 404 && res.status !== 410) throw new Error(await errorText(res))
}
