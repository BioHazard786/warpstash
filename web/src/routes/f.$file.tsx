import { FileIcon, fileKind } from "@/components/file-icon"
import { Section } from "@/components/section"
import {
  Attachment,
  AttachmentContent,
  AttachmentDescription,
  AttachmentMedia,
  AttachmentTitle,
} from "@/components/ui/attachment"
import { Button, buttonVariants } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import { StatusView } from "@/components/status-view"
import { fetchMeta, verifyPassword } from "@/lib/api"
import { formatBytes } from "@/lib/format"
import { cn } from "@/lib/utils"
import { IconArrowLeft, IconDownload, IconFlame, IconLock, IconLockOpen } from "@tabler/icons-react"
import { createFileRoute } from "@tanstack/react-router"
import { useState } from "react"

/** The Go server only sends browsers here for burn files (the interstitial), password-protected files and
 *  burned/missing ones (410/404); regular files are served raw. */
export const Route = createFileRoute("/f/$file")({
  loader: async ({ params }) => {
    const meta = await fetchMeta(params.file)
    // a regular file (only reachable from the dev server or client-side navigation): show the file itself
    if (typeof meta !== "number" && !meta.burn && !meta.password)
      location.replace(`/f/${params.file}?inline=1`)
    return meta
  },
  component: FilePage,
})

function FilePage() {
  const meta = Route.useLoaderData()
  if (typeof meta === "number") return <StatusView code={meta} />
  if (meta.password) return <Locked id={meta.id} burn={meta.burn} />
  if (!meta.burn) return null // redirecting
  return <FileCard name={meta.name ?? ""} size={meta.size ?? 0} burn />
}

function Heading({ title, children }: { title: string; children: string }) {
  return (
    <section className="flex flex-col gap-2">
      <h1 className="font-serif text-2xl tracking-tight text-balance sm:text-3xl">{title}</h1>
      <p className="text-base leading-relaxed text-pretty text-muted-foreground">{children}</p>
    </section>
  )
}

/** Asks for the password; the file's name and size stay hidden until the server accepts it. */
function Locked({ id, burn }: { id: string; burn: boolean }) {
  const [password, setPassword] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const [file, setFile] = useState<{ name: string; size: number }>()

  if (file) return <FileCard {...file} burn={burn} password={password} />

  const unlock = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError("")
    try {
      setFile(await verifyPassword(id, password))
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <Heading title="This file is password protected.">
        {burn
          ? "Enter the password to unlock it. It self-destructs after one download."
          : "Enter the password to unlock it."}
      </Heading>
      <Section
        title="Locked"
        aside={
          <span className="flex items-center gap-1">
            <IconLock className="size-3.5" /> protected
          </span>
        }
      >
        <form onSubmit={unlock} className="flex flex-col gap-2 sm:flex-row">
          <div className="flex flex-1 flex-col gap-1">
            <Input
              type="password"
              autoFocus
              placeholder="Password"
              aria-label="Password"
              aria-invalid={!!error}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
            {error && <p className="text-xs text-destructive">{error}</p>}
          </div>
          <Button type="submit" disabled={busy || !password}>
            {busy ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <IconLockOpen data-icon="inline-start" />
            )}
            Unlock
          </Button>
        </form>
      </Section>
    </>
  )
}

function FileCard({
  name,
  size,
  burn,
  password,
}: {
  name: string
  size: number
  burn: boolean
  /** set for protected files: the download is then a form POST, so the password never lands in a URL */
  password?: string
}) {
  const [taken, setTaken] = useState(false)
  const label = taken ? "Downloaded" : burn ? "Download & destroy" : "Download"
  const buttonClass = cn(
    buttonVariants({ variant: burn ? "destructive" : "default" }),
    taken && "pointer-events-none opacity-50",
  )
  const icon = <IconDownload data-icon="inline-start" />

  return (
    <>
      {burn ? (
        <Heading
          title={taken ? "It's yours. It's gone." : "Someone sent you a file that self-destructs."}
        >
          {taken
            ? "Your download has started and this link no longer works."
            : "One download, then it's deleted from the server. There is no second chance."}
        </Heading>
      ) : (
        <Heading title="Unlocked.">Your file is ready to download.</Heading>
      )}

      <Section
        title={burn ? "Burn after reading" : "Protected file"}
        aside={
          burn ? (
            <span className="flex items-center gap-1 text-destructive">
              <IconFlame className="size-3.5" /> single use
            </span>
          ) : undefined
        }
      >
        <Attachment
          size="default"
          state={taken ? "done" : "idle"}
          className={cn("w-full", burn && "border-destructive/30")}
        >
          <AttachmentMedia
            className={cn(
              burn && "bg-destructive/10 text-destructive dark:bg-destructive/20",
              "[&_svg:not([class*='size-'])]:size-5",
            )}
          >
            <FileIcon name={name} />
          </AttachmentMedia>
          <AttachmentContent>
            <AttachmentTitle title={name}>{name}</AttachmentTitle>
            <AttachmentDescription className="font-mono">
              {fileKind(name)} · {formatBytes(size)}
            </AttachmentDescription>
          </AttachmentContent>
        </Attachment>

        <div className="flex flex-wrap items-center gap-2">
          {password === undefined ? (
            // `download` keeps this a download, not a navigation, so nothing can cancel it after the server burns the file.
            // The anchor stays mounted after the click: removing it mid-click would cancel the download.
            <a
              href={`${location.pathname}?raw=1`}
              download={name}
              onClick={() => setTaken(true)}
              aria-disabled={taken || undefined}
              className={buttonClass}
            >
              {icon}
              {label}
            </a>
          ) : (
            <form
              method="post"
              action={`${location.pathname}?raw=1`}
              onSubmit={() => setTaken(true)}
            >
              <input type="hidden" name="password" value={password} />
              <button type="submit" aria-disabled={taken || undefined} className={buttonClass}>
                {icon}
                {label}
              </button>
            </form>
          )}
          <a href="/" className={buttonVariants({ variant: "outline" })}>
            <IconArrowLeft data-icon="inline-start" />
            Back to home
          </a>
        </div>
      </Section>
    </>
  )
}
