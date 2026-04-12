import { MonitorCog, MoonStar, SunMedium } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { useTheme } from "@/hooks/use-theme"

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
    <TooltipProvider>
      <Tooltip>
        <DropdownMenu>
          <TooltipTrigger asChild>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="icon" aria-label="Switch theme">
                <SunMedium className="size-4 rotate-0 scale-100 transition-all dark:-rotate-90 dark:scale-0" />
                <MoonStar className="absolute size-4 rotate-90 scale-0 transition-all dark:rotate-0 dark:scale-100" />
              </Button>
            </DropdownMenuTrigger>
          </TooltipTrigger>
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
        <TooltipContent>Switch theme</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
