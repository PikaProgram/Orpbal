import { fileURLToPath } from "node:url";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// Dev server keeps Astro's old port (4321) and proxies /api to the Go
// server so the browser stays same-origin (cookie auth needs no CORS).
export default defineConfig({
	plugins: [react(), tailwindcss()],
	resolve: {
		alias: {
			"@": fileURLToPath(new URL("./src", import.meta.url)),
		},
	},
	server: {
		host: process.env.HOST ?? "0.0.0.0",
		port: Number(process.env.PORT ?? "4321"),
		proxy: {
			"/api": {
				target: process.env.SERVER_URL ?? "http://localhost:8080",
				changeOrigin: true,
			},
		},
	},
});
