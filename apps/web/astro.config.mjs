import node from "@astrojs/node";
import solidJs from "@astrojs/solid-js";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "astro/config";

export default defineConfig({
	output: "server",
	adapter: node({ mode: "standalone" }),
	security: {
		checkOrigin: true,
	},
	integrations: [solidJs()],
	vite: {
		plugins: [tailwindcss()],
	},
});
