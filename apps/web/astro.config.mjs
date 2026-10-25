import node from "@astrojs/node";
import react from "@astrojs/react";
import solidJs from "@astrojs/solid-js";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "astro/config";

export default defineConfig({
	output: "server",
	adapter: node({ mode: "standalone" }),
	security: {
		checkOrigin: true,
	},
	integrations: [
		react({ include: ["src/components/SchemaGraph.tsx"] }),
		solidJs({ exclude: ["src/components/SchemaGraph.tsx"] }),
	],
	vite: {
		plugins: [tailwindcss()],
	},
});
