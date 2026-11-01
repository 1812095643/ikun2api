import { nativeStorageName } from "@/lib/native-runtime";
import { create } from "zustand";
import { persist } from "zustand/middleware";

export type ThemeName = "light" | "dark";

type ThemeStore = {
    theme: ThemeName;
    setTheme: (theme: ThemeName) => void;
};

export const useThemeStore = create<ThemeStore>()(
    persist(
        (set) => ({
            theme: localStorage.getItem("theme") === "light" ? "light" : "dark",
            setTheme: (theme) => set({ theme }),
        }),
        { name: nativeStorageName("infinite-canvas:theme_store") },
    ),
);
