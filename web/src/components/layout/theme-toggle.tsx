import { MonitorCog, MoonStar, SunMedium } from "lucide-react"
import { useTheme } from "next-themes"

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Button } from "@/components/ui/button"

type ThemeValue = "light" | "dark" | "system"

type ThemeToggleProps = {
  onThemeChange?: (theme: ThemeValue) => void
}

export function ThemeToggle({ onThemeChange }: ThemeToggleProps) {
  const { setTheme } = useTheme()
  const apply = (nextTheme: ThemeValue) => {
    setTheme(nextTheme)
    onThemeChange?.(nextTheme)
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="icon" aria-label="Theme switcher">
          <SunMedium className="size-4 rotate-0 scale-100 transition-all dark:-rotate-90 dark:scale-0" />
          <MoonStar className="absolute size-4 rotate-90 scale-0 transition-all dark:rotate-0 dark:scale-100" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-44">
        <DropdownMenuItem onClick={() => apply("light")}>
          <SunMedium className="mr-2 size-4" />
          Light
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => apply("dark")}>
          <MoonStar className="mr-2 size-4" />
          Dark
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => apply("system")}>
          <MonitorCog className="mr-2 size-4" />
          System
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
