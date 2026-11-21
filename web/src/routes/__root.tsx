import { Header } from "@/components/header"
import { StatusView } from "@/components/status-view"
import { Marker, MarkerContent } from "@/components/ui/marker"
import { Toaster } from "@/components/ui/toast"
import { Link, Outlet, createRootRoute } from "@tanstack/react-router"

// The error component replaces the whole root component, so it needs the layout itself to be centered.
function Shell({ children }: { children: React.ReactNode }) {
  return (
    <Toaster>
      <div className="flex min-h-svh flex-col">
        <Header />
        <main className="mx-auto flex w-full max-w-3xl flex-1 flex-col gap-10 px-4 pt-16 pb-6 sm:px-6 sm:pt-20">
          {children}
        </main>
        <footer className="mx-auto w-full max-w-3xl px-4 pt-10 pb-8 sm:px-6">
          <Marker variant="separator" className="font-mono text-xs">
            <MarkerContent>
              warpstash · <Link to="/about">about</Link> ·{" "}
              <a href="https://github.com/BioHazard786/warpstash" target="_blank" rel="noreferrer">
                open source
              </a>
            </MarkerContent>
          </Marker>
        </footer>
      </div>
    </Toaster>
  )
}

export const Route = createRootRoute({
  component: () => (
    <Shell>
      <Outlet />
    </Shell>
  ),
  notFoundComponent: () => <StatusView code={404} />,
  errorComponent: ({ error }) => (
    <Shell>
      <StatusView code={500} description={(error as Error).message} />
    </Shell>
  ),
})
