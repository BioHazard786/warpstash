import { CopyAction } from "@/components/copy-action"
import { FileIcon, fileKind } from "@/components/file-icon"
import { QrAction } from "@/components/qr-action"
import {
  Attachment,
  AttachmentAction,
  AttachmentActions,
  AttachmentContent,
  AttachmentDescription,
  AttachmentMedia,
  AttachmentTitle,
} from "@/components/ui/attachment"
import { Button } from "@/components/ui/button"
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet"
import { deleteFile } from "@/lib/api"
import { expiresIn, formatBytes } from "@/lib/format"
import { clearHistory, removeHistory, useHistory, type HistoryRecord } from "@/lib/history"
import { IconDownload, IconHistory, IconTrash } from "@tabler/icons-react"
import { useState } from "react"

function Row({ r }: { r: HistoryRecord }) {
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  return (
    <Attachment state={error ? "error" : "done"} className="w-full" title={r.name}>
      <AttachmentMedia>
        <FileIcon name={r.name} />
      </AttachmentMedia>
      <AttachmentContent>
        <AttachmentTitle>{r.name}</AttachmentTitle>
        <AttachmentDescription className="font-mono tabular-nums">
          {error
            ? `Delete failed · ${error}`
            : [fileKind(r.name), formatBytes(r.size), expiresIn(r.expiresAt)]
                .filter(Boolean)
                .join(" · ")}
        </AttachmentDescription>
      </AttachmentContent>
      <AttachmentActions>
        <AttachmentAction
          nativeButton={false}
          render={<a href={r.url} download={r.name} />}
          aria-label={`Download ${r.name}`}
        >
          <IconDownload />
        </AttachmentAction>
        <CopyAction value={r.url} label={`Copy link for ${r.name}`} />
        <QrAction value={r.url} label={`QR code for ${r.name}`} />
        <AttachmentAction
          aria-label={`Delete ${r.name} from the server`}
          title="Delete file now"
          disabled={busy}
          onClick={async () => {
            setBusy(true)
            try {
              if (r.deleteToken) await deleteFile(r.id, r.deleteToken)
              removeHistory(r.id)
            } catch (e) {
              setError((e as Error).message)
              setBusy(false)
            }
          }}
        >
          <IconTrash />
        </AttachmentAction>
      </AttachmentActions>
    </Attachment>
  )
}

export function HistorySheet() {
  const history = useHistory()
  return (
    <Sheet>
      <SheetTrigger render={<Button variant="outline" size="icon" aria-label="Upload history" />}>
        <IconHistory />
      </SheetTrigger>
      <SheetContent side="right">
        <SheetHeader>
          <SheetTitle>Upload history</SheetTitle>
          <SheetDescription>
            Files uploaded from this browser. Clearing history doesn't delete them from the server.
          </SheetDescription>
        </SheetHeader>
        <div className="flex flex-1 flex-col gap-2.5 overflow-y-auto px-4">
          {history.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <IconHistory />
                </EmptyMedia>
                <EmptyTitle>No uploads in this browser yet</EmptyTitle>
                <EmptyDescription>Files you upload will appear here.</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            history.map((r) => <Row key={r.id} r={r} />)
          )}
        </div>
        {history.length > 0 && (
          <SheetFooter>
            <Button variant="destructive" onClick={clearHistory}>
              <IconTrash data-icon="inline-start" /> Clear history
            </Button>
          </SheetFooter>
        )}
      </SheetContent>
    </Sheet>
  )
}
