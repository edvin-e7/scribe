import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// base: './' is mandatory for Wails — the built index.html must reference assets
// with RELATIVE paths, or the embedded asset server 404s and you get a white
// screen in `wails build` while `vite dev` works fine.
export default defineConfig({
  plugins: [react()],
  base: "./",
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});
