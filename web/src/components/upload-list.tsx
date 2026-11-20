import { CopyAction } from "@/components/copy-action"
import { FileIcon, fileKind } from "@/components/file-icon"
import { QrAction } from "@/components/qr-action"
import {
  Attachment,
  AttachmentAction,
  AttachmentActions,
  AttachmentContent,
  AttachmentDescription,
  AttachmentGroup,
  AttachmentMedia,
  AttachmentTitle,
} from "@/components/ui/attachment"
import { Spinner } from "@/components/ui/spinner"
import { expiresIn, formatBytes, formatSpeed } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { Item } from "@/store/upload"
import {
  IconAlertCircle,
  IconDownload,
  IconExternalLink,
  IconRefresh,
  IconX,
} from "@tabler/icons-react"
import { useState } from "react"

const STATE = { queued: "idle", uploading: "uploading", done: "done", error: "error" } as const

/** One-line status; `tile` drops the size because image tiles are narrow. */
function describe(it: Item, tile: boolean): string {
  const size = formatBytes(it.file.size)
  switch (it.status) {
    case "queued":
      return `${fileKind(it.file.name, it.file.type)} · ${size}`
    case "uploading": {
      const pct = Math.floor((it.loaded / (it.file.size || 1)) * 100)
      return pct >= 100
        ? `Processing · ${size}`
        : `${pct}% · ${it.speed ? formatSpeed(it.speed) : size}`
    }
    case "error":
      return `Failed · ${it.error ?? "try again"}`
    case "done": {
      const r = it.result
      const left = r?.is_burn_on_read
        ? "Burn on read"
        : expiresIn(r?.expires_at ?? "") || "Uploaded"
      return tile ? left : `${left} · ${size}`
    }
  }
}

function Media({ it }: { it: Item }) {
  const [broken, setBroken] = useState(false) // e.g. HEIC, which most browsers can't decode
  if (it.status === "uploading")
    return (
      <AttachmentMedia>
        <Spinner />
      </AttachmentMedia>
    )
  if (it.status === "error")
    return (
      <AttachmentMedia>
        <IconAlertCircle />
      </AttachmentMedia>
    )
  if (it.preview && !broken)
    return (
      <AttachmentMedia variant="image">
        <img src={it.preview} alt="" onError={() => setBroken(true)} />
      </AttachmentMedia>
    )
  return (
    <AttachmentMedia>
      <FileIcon name={it.file.name} type={it.file.type} />
    </AttachmentMedia>
  )
}

type Handlers = { onRemove: (id: string) => void; onRetry: (id: string) => void }

function Row({ it, tile, onRemove, onRetry }: { it: Item; tile: boolean } & Handlers) {
  const name = it.file.name
  const r = it.result
  return (
    <Attachment
      state={STATE[it.status]}
      orientation={tile ? "vertical" : "horizontal"}
      className={tile ? "has-data-[slot=attachment-content]:w-40" : "w-full"}
      title={name}
    >
      <Media it={it} />
      <AttachmentContent>
        <AttachmentTitle>{name}</AttachmentTitle>
        <AttachmentDescription className="font-mono tabular-nums">
          {describe(it, tile)}
        </AttachmentDescription>
      </AttachmentContent>
      {/* on image tiles the actions float over the picture */}
      <AttachmentActions className={cn(tile && "rounded-lg bg-background/80 backdrop-blur-sm")}>
        {it.status === "error" && (
          <AttachmentAction aria-label={`Retry ${name}`} onClick={() => onRetry(it.id)}>
            <IconRefresh />
          </AttachmentAction>
        )}
        {r && (
          <>
            <CopyAction
              value={r.url}
              label={r.is_burn_on_read ? "Copy one-time link" : "Copy link"}
            />
            <QrAction value={r.url} label={`QR code for ${name}`} />
            {!r.is_burn_on_read && !tile && (
              <AttachmentAction
                nativeButton={false}
                render={<a href={r.url} download={name} />}
                aria-label={`Download ${name}`}
              >
                <IconDownload />
              </AttachmentAction>
            )}
            {!r.is_burn_on_read && (
              <AttachmentAction
                nativeButton={false}
                render={<a href={`${r.url}?inline=1`} target="_blank" rel="noreferrer" />}
                aria-label={`Preview ${name} in a new tab`}
              >
                <IconExternalLink />
              </AttachmentAction>
            )}
          </>
        )}
        <AttachmentAction
          aria-label={it.status === "uploading" ? `Cancel ${name}` : `Remove ${name}`}
          onClick={() => onRemove(it.id)}
        >
          <IconX />
        </AttachmentAction>
      </AttachmentActions>
    </Attachment>
  )
}

/** Images as a scrolling row of tiles, everything else as rows, like warpdrop's FileList. */
export function UploadList({ items, ...handlers }: { items: Item[] } & Handlers) {
  const images = items.filter((i) => i.preview)
  const docs = items.filter((i) => !i.preview)
  return (
    <div className="flex flex-col gap-3">
      {images.length > 0 && (
        <AttachmentGroup>
          {images.map((it) => (
            <Row key={it.id} it={it} tile {...handlers} />
          ))}
        </AttachmentGroup>
      )}
      {docs.length > 0 && (
        <div className="flex flex-col gap-2">
          {docs.map((it) => (
            <Row key={it.id} it={it} tile={false} {...handlers} />
          ))}
        </div>
      )}
    </div>
  )
}
