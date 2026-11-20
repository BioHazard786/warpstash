import { RouterProvider, createRouter } from "@tanstack/react-router"
import { StrictMode } from "react"
import { createRoot } from "react-dom/client"

import "./index.css"
import { ThemeProvider } from "@/components/theme-provider.tsx"
import { Spinner } from "@/components/ui/spinner"
import { routeTree } from "./routeTree.gen"

// shown while a route loader (e.g. /api/info, file details) is slow, instead of a blank page
const router = createRouter({
  routeTree,
  defaultPendingComponent: () => <Spinner className="m-auto size-6 text-muted-foreground" />,
})

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router
  }
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ThemeProvider>
      <RouterProvider router={router} />
    </ThemeProvider>
  </StrictMode>,
)
