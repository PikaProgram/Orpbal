/// <reference types="vitest/config" />
import { getViteConfig } from "astro/config";

// Astro's loader gives tests the same vite pipeline the app uses: the `@/`
// tsconfig alias and virtual modules such as `astro:actions`.
export default getViteConfig({
	test: {
		environment: "node",
		include: ["src/**/*.test.ts"],
	},
});