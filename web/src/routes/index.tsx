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
import { useUpload } from "@/store/upload"
import { IconCloudUpload, IconFlame } from "@tabler/icons-react"
import { createFileRoute } from "@tanstack/react-router"
import { useState } from "react"

export const Route = createFileRoute("/")({ loader: fetchInfo, component: UploadPage })

const TOKEN_KEY = "warpstash_token"

const cli = (o: string) => ({
  curl: {
    cmd: `curl -F file=@document.pdf ${o}/api/upload`,
    hint: "Returns a direct link. Add -F time=1h or -F burn=true to control expiry.",
  },
  wget: {
    cmd: `wget --post-file=document.pdf --header='X-Filename: document.pdf' ${o}/api/upload -qO -`,
    hint: "Uploads the raw file body. Add --header='X-Expiry: 1h' to control expiry.",
  },
})

function UploadPage() {
  const info = Route.useLoaderData()
  const { items, add, remove, clear, uploadAll, retry } = useUpload()
  const expiries = info.allowed_expiries
  const [expiry, setExpiry] = useState(
    expiries.includes(info.default_expiry) ? info.default_expiry : (expiries[0] ?? ""),
  )
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
            value={[expiry]}
            onValueChange={(v) => v[0] && setExpiry(v[0])}
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
          </ToggleGroup>
        }
      >
        <Dropzone
          onFiles={onFiles}
          hints={[
            Number.isFinite(maxFiles) ? `up to ${maxFiles} files` : "many at once",
            `${info.max_file_size_human} each`,
          ]}
        />

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
              onRetry={(id) => void retry(id, expiry, token)}
            />
            <div className="flex items-center justify-between gap-2">
              <Button variant="outline" size="sm" onClick={clear}>
                {allDone ? "Clear completed" : "Remove all"}
              </Button>
              {!allDone && (
                <Button
                  size="sm"
                  disabled={busy || pending === 0}
                  onClick={() => void uploadAll(expiry, token)}
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
