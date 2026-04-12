import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { BrowserRouter } from "react-router-dom"
import { Toaster } from "sonner"

import "@fontsource-variable/inter/index.css"
import "@fontsource-variable/jetbrains-mono/index.css"

import { App } from "@/app/app"
import { AppErrorBoundary } from "@/app/error-boundary"
import { ThemeProvider } from "@/app/theme-provider"

import "@/index.css"

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      refetchOnWindowFocus: false,
      retry: 1,
    },
  },
})

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <AppErrorBoundary>
          <BrowserRouter>
            <App />
            <Toaster position="bottom-right" visibleToasts={3} richColors closeButton />
          </BrowserRouter>
        </AppErrorBoundary>
      </ThemeProvider>
    </QueryClientProvider>
  </StrictMode>,
)
