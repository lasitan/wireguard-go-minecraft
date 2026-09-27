import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  root: ".",
  plugins: [react()],
  build: {
    outDir: "../master/ui/dist",
    emptyOutDir: true,
    assetsDir: ".",
  },
});
