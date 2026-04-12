import { createContext, useContext, useEffect, useMemo, type ReactNode } from "react"

import { useLocalStorage } from "@/hooks/use-local-storage"

export type Theme = "light" | "dark" | "system"

const THEME_STORAGE_KEY = "monsoon-theme"

type ThemeContextValue = {
  theme: Theme
  resolvedTheme: "light" | "dark"
  setTheme: (theme: Theme) => void
}

const ThemeContext = createContext<ThemeContextValue | null>(null)

function getSystemTheme() {
  if (typeof window === "undefined") {
    return "dark" as const
  }
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light"
}

function applyTheme(theme: Theme) {
  const resolvedTheme = theme === "system" ? getSystemTheme() : theme
  const root = document.documentElement
  root.classList.remove("light", "dark")
  root.classList.add(resolvedTheme)
  root.dataset.theme = theme
  root.style.colorScheme = resolvedTheme
  return resolvedTheme
}

type ThemeProviderProps = {
  children: ReactNode
}

export function ThemeProvider({ children }: ThemeProviderProps) {
  const [theme, setTheme] = useLocalStorage<Theme>(THEME_STORAGE_KEY, "system")

  useEffect(() => {
    const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)")
    const syncTheme = () => {
      applyTheme(theme)
    }
    syncTheme()
    mediaQuery.addEventListener("change", syncTheme)
    return () => {
      mediaQuery.removeEventListener("change", syncTheme)
    }
  }, [theme])

  const value = useMemo<ThemeContextValue>(
    () => ({
      theme,
      resolvedTheme: theme === "system" ? getSystemTheme() : theme,
      setTheme,
    }),
    [theme, setTheme],
  )

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export function useTheme() {
  const context = useContext(ThemeContext)
  if (!context) {
    throw new Error("useTheme must be used within ThemeProvider")
  }
  return context
}
