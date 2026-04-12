import { create } from "zustand"

export type DashboardNotification = {
  id: string
  type: string
  message: string
  at: string
}

type DashboardUIState = {
  notifications: DashboardNotification[]
  tokenSecret: string | null
  pushNotification: (notification: Omit<DashboardNotification, "id">) => void
  clearNotifications: () => void
  setTokenSecret: (tokenSecret: string | null) => void
}

function buildNotificationID() {
  return `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
}

export const useDashboardUIStore = create<DashboardUIState>((set) => ({
  notifications: [],
  tokenSecret: null,
  pushNotification: (notification) =>
    set((state) => ({
      notifications: [{ id: buildNotificationID(), ...notification }, ...state.notifications].slice(0, 40),
    })),
  clearNotifications: () => set({ notifications: [] }),
  setTokenSecret: (tokenSecret) => set({ tokenSecret }),
}))
