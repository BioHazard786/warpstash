import { CopyAction } from "@/components/copy-action"
import { FileIcon, fileKind } from "@/components/file-icon"
import {
  Attachment,
  AttachmentAction,
  AttachmentActions,
  AttachmentContent,
  AttachmentDescription,
  AttachmentMedia,
  AttachmentTitle,
} from "@/components/ui/attachment"
import { Spinner } from "@/components/ui/spinner"
import { formatBytes, formatSpeed } from "@/lib/format"
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

function describe(it: Item): string {
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
    case "done":
      return `${it.result?.is_burn_on_read ? "Burn on read" : "Uploaded"} · ${size}`
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

export function UploadList({
  items,
  onRemove,
  onRetry,
}: {
  items: Item[]
  onRemove: (id: string) => void
  onRetry: (id: string) => void
}) {
  return (
    <div className="flex flex-col gap-2">
      {items.map((it) => {
        const name = it.file.name
        const r = it.result
        return (
          <Attachment key={it.id} state={STATE[it.status]} className="w-full" title={name}>
            <Media it={it} />
            <AttachmentContent>
              <AttachmentTitle>{name}</AttachmentTitle>
              <AttachmentDescription className="font-mono tabular-nums">
                {describe(it)}
              </AttachmentDescription>
            </AttachmentContent>
            <AttachmentActions>
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
                  {!r.is_burn_on_read && (
                    <>
                      <AttachmentAction
                        nativeButton={false}
                        render={<a href={r.url} download={name} />}
                        aria-label={`Download ${name}`}
                      >
                        <IconDownload />
                      </AttachmentAction>
                      <AttachmentAction
                        nativeButton={false}
                        render={<a href={`${r.url}?inline=1`} target="_blank" rel="noreferrer" />}
                        aria-label={`Preview ${name} in a new tab`}
                      >
                        <IconExternalLink />
                      </AttachmentAction>
                    </>
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
      })}
    </div>
  )
}
