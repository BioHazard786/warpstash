import { AttachmentAction } from "@/components/ui/attachment"
import { toast } from "@/components/ui/toast"
import { copyText } from "@/lib/clipboard"
import { IconCheck, IconCopy } from "@tabler/icons-react"
import { useEffect, useState } from "react"

/** Attachment action that copies `value`; shows a check only when the copy really happened. */
export function CopyAction({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false)
  useEffect(() => {
    if (!copied) return
    const t = setTimeout(() => setCopied(false), 2000)
    return () => clearTimeout(t)
  }, [copied])
  return (
    <AttachmentAction
      aria-label={copied ? "Copied" : label}
      title={copied ? "Copied" : label}
      onClick={async () => {
        if (await copyText(value)) setCopied(true)
        else toast.add({ type: "error", title: "Couldn't copy", description: value })
      }}
    >
      {copied ? <IconCheck /> : <IconCopy />}
    </AttachmentAction>
  )
}
