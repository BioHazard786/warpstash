import { Header } from "@/components/header"
import { StatusView } from "@/components/status-view"
import { Marker, MarkerContent } from "@/components/ui/marker"
import { Toaster } from "@/components/ui/toast"
import { pageData } from "@/lib/page-data"
import { Outlet, createRootRoute } from "@tanstack/react-router"

export const Route = createRootRoute({
  component: () => (
    <Toaster>
      <div className="flex min-h-svh flex-col">
        <Header />
        <main className="mx-auto flex w-full max-w-3xl flex-1 flex-col gap-10 px-4 pt-16 pb-6 sm:px-6 sm:pt-20">
          <Outlet />
        </main>
        <footer className="mx-auto w-full max-w-3xl px-4 pt-10 pb-8 sm:px-6">
          <Marker variant="separator" className="font-mono text-xs">
            <MarkerContent>
              warpstash ·{" "}
              <a href="https://github.com/BioHazard786/warpstash" target="_blank" rel="noreferrer">
                open source
              </a>
            </MarkerContent>
          </Marker>
        </footer>
      </div>
    </Toaster>
  ),
  // the server sends unknown paths here with status 404, and panics with 500
  notFoundComponent: () => <StatusView code={pageData.status === 500 ? 500 : 404} />,
  errorComponent: ({ error }) => <StatusView code={500} description={(error as Error).message} />,
})
