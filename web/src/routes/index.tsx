import { AsciiLogo } from "@/components/ascii-logo"
import { CliBox } from "@/components/cli-box"
import { Dropzone } from "@/components/dropzone"
import { Section } from "@/components/section"
import { UploadList } from "@/components/upload-list"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { toast } from "@/components/ui/toast"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { fetchInfo } from "@/lib/api"
import { expiryError, formatDuration } from "@/lib/expiry"
import { useUpload } from "@/store/upload"
import { IconCloudUpload, IconFlame, IconLock } from "@tabler/icons-react"
import { createFileRoute } from "@tanstack/react-router"
import { useEffect, useState } from "react"

export const Route = createFileRoute("/")({ loader: fetchInfo, component: UploadPage })

const TOKEN_KEY = "warpstash_token"

const cli = (o: string) => ({
  curl: {
    cmd: `curl -F file=@document.pdf ${o}/`,
    hint: "Returns a direct link. Add -F time=5d (or 90m, 2w), -F burn=true or -F password=… to control it.",
  },
  wget: {
    cmd: `wget --post-file=document.pdf --header='X-Filename: document.pdf' ${o}/ -qO -`,
    hint: "Uploads the raw file body. Add --header='X-Expiry: 5d' or --header='X-Password: …' to control it.",
  },
})

function UploadPage() {
  const info = Route.useLoaderData()
  const { items, add, remove, clear, uploadAll, retry } = useUpload()
  const expiries = info.allowed_expiries
  const [preset, setPreset] = useState(
    expiries.includes(info.default_expiry) ? info.default_expiry : (expiries[0] ?? "custom"),
  )
  const [customExpiry, setCustomExpiry] = useState("")
  const [password, setPassword] = useState("")
  const isCustom = preset === "custom"
  const expiry = isCustom ? customExpiry.trim() : preset
  const expiryErr = isCustom ? expiryError(customExpiry, info.max_expiry_seconds) : ""
  const [token, setToken] = useState(() => localStorage.getItem(TOKEN_KEY) ?? "")

  const maxFiles = info.max_files || Infinity
  const onFiles = (files: File[]) => {
    const err = add(files, {
      maxFiles,
      maxBytes: info.max_file_size_mb * 1024 * 1024,
      maxHuman: info.max_file_size_human,
    })
    if (err) toast.add({ type: "error", title: "Some files weren't added", description: err })
  }

  // Paste a screenshot (or copied files) anywhere on the page to queue it.
  useEffect(() => {
    const onPaste = (e: ClipboardEvent) => {
      const files = [...(e.clipboardData?.files ?? [])]
      if (!files.length) return // plain text, e.g. into the token field
      e.preventDefault()
      onFiles(files)
    }
    addEventListener("paste", onPaste)
    return () => removeEventListener("paste", onPaste)
  })

  const busy = items.some((i) => i.status === "uploading")
  const pending = items.filter((i) => i.status === "queued" || i.status === "error").length
  const allDone = items.length > 0 && pending === 0 && !busy

  return (
    <>
      <section className="flex flex-col gap-5">
        <AsciiLogo />
        <div className="flex flex-col gap-2">
          <h1 className="font-serif text-2xl tracking-tight text-balance sm:text-3xl">
            Share a file. Let it disappear.
          </h1>
          <p className="text-base leading-relaxed text-pretty text-muted-foreground">
            Fast, minimalist temporary file hosting. Automatic expiry or single-use
            burn-after-reading, direct URLs, zero tracking.
          </p>
        </div>
      </section>

      <Section
        title="Upload"
        aside={
          <ToggleGroup
            variant="outline"
            size="sm"
            spacing={0}
            value={[preset]}
            onValueChange={(v) => v[0] && setPreset(v[0])}
            aria-label="Expiry"
          >
            {expiries.map((e) =>
              e === "burn" ? (
                <ToggleGroupItem
                  key={e}
                  value={e}
                  title="Burn after reading (destroyed on first download)"
                  className="font-mono text-xs text-destructive/80 hover:text-destructive data-pressed:border-destructive/40 data-pressed:bg-destructive/15 data-pressed:text-destructive"
                >
                  <IconFlame /> Burn
                </ToggleGroupItem>
              ) : (
                <ToggleGroupItem key={e} value={e} className="font-mono text-xs">
                  {e}
                </ToggleGroupItem>
              ),
            )}
            <ToggleGroupItem value="custom" className="font-mono text-xs">
              Custom
            </ToggleGroupItem>
          </ToggleGroup>
        }
      >
        <Dropzone
          onFiles={onFiles}
          hints={[
            Number.isFinite(maxFiles) ? `up to ${maxFiles} files` : "many at once",
            `${info.max_file_size_human} each`,
            "or paste",
          ]}
        />

        <div className="flex flex-col gap-2 sm:flex-row">
          {isCustom && (
            <div className="flex flex-1 flex-col gap-1">
              <Input
                autoFocus
                placeholder="Expires after… e.g. 90m, 5d, 2w"
                aria-label="Custom expiry"
                aria-invalid={customExpiry !== "" && !!expiryErr}
                value={customExpiry}
                onChange={(e) => setCustomExpiry(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">
                {(customExpiry && expiryErr) ||
                  (info.max_expiry_seconds
                    ? `Minutes, hours, days or weeks, up to ${formatDuration(info.max_expiry_seconds)}.`
                    : "Minutes, hours, days or weeks.")}
              </p>
            </div>
          )}
          <div className="relative flex-1">
            <IconLock className="pointer-events-none absolute top-2 left-2.5 size-4 text-muted-foreground" />
            <Input
              type="password"
              className="pl-8"
              placeholder="Password (optional)"
              aria-label="Download password"
              autoComplete="new-password"
              maxLength={72}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </div>
        </div>

        {info.auth_required && (
          <Input
            type="password"
            placeholder="Upload token (this server requires one)"
            aria-label="Upload token"
            value={token}
            onChange={(e) => {
              setToken(e.target.value)
              localStorage.setItem(TOKEN_KEY, e.target.value)
            }}
          />
        )}

        {items.length > 0 && (
          <>
            <UploadList
              items={items}
              onRemove={remove}
              onRetry={(id) => void retry(id, { expiry, token, password })}
            />
            <div className="flex items-center justify-between gap-2">
              <Button variant="outline" size="sm" onClick={clear}>
                {allDone ? "Clear completed" : "Remove all"}
              </Button>
              {!allDone && (
                <Button
                  size="sm"
                  disabled={busy || pending === 0 || !!expiryErr}
                  onClick={() => void uploadAll({ expiry, token, password })}
                >
                  <IconCloudUpload data-icon="inline-start" />
                  {busy ? "Uploading…" : pending > 1 ? `Upload ${pending} files` : "Upload"}
                </Button>
              )}
            </div>
          </>
        )}
      </Section>

      <Terminal />
    </>
  )
}

function Terminal() {
  const CLI = cli(location.origin)
  const [tab, setTab] = useState<keyof typeof CLI>("curl")
  return (
    <Section
      title="Terminal"
      aside={
        <ToggleGroup
          variant="outline"
          size="sm"
          spacing={0}
          value={[tab]}
          onValueChange={(v) => v[0] && setTab(v[0] as keyof typeof CLI)}
          aria-label="Command"
        >
          {Object.keys(CLI).map((k) => (
            <ToggleGroupItem key={k} value={k} className="font-mono text-xs">
              {k}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      }
    >
      <CliBox command={CLI[tab].cmd} />
      <p className="text-xs text-muted-foreground">{CLI[tab].hint}</p>
    </Section>
  )
}
