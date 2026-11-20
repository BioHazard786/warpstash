import { HistorySheet } from "@/components/history-sheet"
import { ThemeToggle } from "@/components/theme-toggle"

export function Header() {
  return (
    <div className="absolute top-4 right-4 z-40 flex items-center gap-2">
      <HistorySheet />
      <ThemeToggle />
    </div>
  )
}
