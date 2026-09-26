import { defineConfig } from "vite";

export default defineConfig({
  root: ".",
  build: {
    outDir: "../master/ui",
    emptyOutDir: true,
    assetsDir: ".",
  },
});
