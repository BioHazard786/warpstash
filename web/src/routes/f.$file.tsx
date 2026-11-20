import { StatusView } from "@/components/status-view"
import { buttonVariants } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { formatBytes } from "@/lib/format"
import { fetchMeta } from "@/lib/api"
import { cn } from "@/lib/utils"
import { IconAlertTriangle, IconFlame } from "@tabler/icons-react"
import { createFileRoute } from "@tanstack/react-router"
import { useState } from "react"

/** The Go server only sends browsers here for burn files (the interstitial) and burned/missing ones (410/404);
 *  regular files are served raw. */
export const Route = createFileRoute("/f/$file")({
  loader: ({ params }) => fetchMeta(params.file),
  component: FilePage,
})

function FilePage() {
  const meta = Route.useLoaderData()
  if (typeof meta === "number") return <StatusView code={meta} />
  if (!meta.burn) return <StatusView code={404} /> // only reachable via client-side navigation
  return <BurnCard name={meta.name} size={meta.size} />
}

function BurnCard({ name, size }: { name: string; size: number }) {
  const [taken, setTaken] = useState(false)
  return (
    <Empty className="my-auto flex-none border-2 border-dashed py-12">
      <EmptyHeader>
        <EmptyMedia
          variant="icon"
          className="bg-destructive/10 text-destructive dark:bg-destructive/20"
        >
          <IconFlame />
        </EmptyMedia>
        <EmptyTitle className="max-w-xs truncate text-base" title={name}>
          {name}
        </EmptyTitle>
        <EmptyDescription className="font-mono text-xs">
          {formatBytes(size)} · Single-use delivery
        </EmptyDescription>
        <EmptyDescription className="text-destructive">
          {taken ? (
            "Download started. This link no longer works."
          ) : (
            <>
              <IconAlertTriangle className="mr-1 inline size-4 align-text-bottom" />
              <strong>Warning:</strong> this file can be downloaded once. Downloading it destroys it
              on the server.
            </>
          )}
        </EmptyDescription>
      </EmptyHeader>
      <EmptyContent className="flex-row justify-center gap-2">
        {/* `download` keeps this a download, not a navigation, so nothing can cancel it after the server burns the file.
            The anchor stays mounted after the click: removing it mid-click would cancel the download. */}
        <a
          href={`${location.pathname}?raw=1`}
          download={name}
          onClick={() => setTaken(true)}
          aria-disabled={taken || undefined}
          className={cn(
            buttonVariants({ variant: "destructive" }),
            taken && "pointer-events-none opacity-50",
          )}
        >
          {taken ? "Downloaded" : "Download & destroy"}
        </a>
        <a href="/" className={buttonVariants({ variant: "outline" })}>
          Back to home
        </a>
      </EmptyContent>
    </Empty>
  )
}
