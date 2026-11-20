import { useTheme } from "@/components/theme-provider"
import { Button } from "@/components/ui/button"
import { IconBrightness } from "@tabler/icons-react"

/** Light/dark switch. Press D anywhere to toggle too. */
export function ThemeToggle() {
  const { theme, setTheme } = useTheme()
  return (
    <Button
      variant="outline"
      size="icon"
      onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
      aria-label={`Switch to ${theme === "dark" ? "light" : "dark"} theme`}
    >
      <IconBrightness />
    </Button>
  )
}
