import type { AstroGlobal } from "astro";

type Action = Parameters<AstroGlobal["getActionResult"]>[0];

// Astro stores the most recent result of each action for the POST re-render.
export function actionResults(Astro: AstroGlobal, actions: Action[]) {
	const results = actions.map((action) => Astro.getActionResult(action));
	return {
		succeeded: results.some((result) => result?.data?.ok === true),
		error:
			results
				.map((result) => {
					if (result?.data && result.data.ok === false) return result.data.error;
					if (result?.error) return "Check the form values and try again.";
					return null;
				})
				.find(Boolean) ?? null,
	};
}
