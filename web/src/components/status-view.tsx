import { buttonVariants } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty"

const COPY: Record<number, { title: string; description: string; action: string }> = {
  404: {
    title: "Page not found",
    description: "This link is wrong, or the file expired and was deleted.",
    action: "Back to home",
  },
  410: {
    title: "File burned & shredded",
    description:
      "This file was set to burn after reading and has already been downloaded or destroyed.",
    action: "Upload a new file",
  },
  500: {
    title: "Something broke",
    description: "The server hit an unexpected error. Try again in a moment.",
    action: "Back to home",
  },
}

/** Full-page 404 / 410 / 500. */
export function StatusView({ code, description }: { code: number; description?: string }) {
  const c = COPY[code] ?? COPY[500]!
  return (
    <Empty className="my-auto flex-none border-2 border-dashed py-12">
      <EmptyHeader>
        <div className="font-pixel text-6xl leading-none" aria-hidden>
          {code}
        </div>
        <EmptyTitle className="text-base">{c.title}</EmptyTitle>
        <EmptyDescription>{description ?? c.description}</EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <a href="/" className={buttonVariants({ variant: "outline" })}>
          {c.action}
        </a>
      </EmptyContent>
    </Empty>
  )
}
