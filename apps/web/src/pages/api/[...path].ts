import type { APIRoute } from "astro";

export const prerender = false;

const HOP_BY_HOP: Record<string, true> = {
	connection: true,
	"keep-alive": true,
	"transfer-encoding": true,
	"content-encoding": true,
	"content-length": true,
	host: true,
};

export const ALL: APIRoute = async ({ request, params }) => {
	const base = process.env.SERVER_URL || import.meta.env.SERVER_URL || "http://localhost:8080";
	const incoming = new URL(request.url);
	const target = new URL(`/api/${params.path ?? ""}`, base);
	target.search = incoming.search;

	const headers = new Headers(request.headers);
	for (const header of Object.keys(HOP_BY_HOP)) {
		headers.delete(header);
	}

	const init: RequestInit & { duplex?: "half" } = {
		method: request.method,
		headers,
		redirect: "manual",
	};
	if (request.method !== "GET" && request.method !== "HEAD" && request.body) {
		init.body = request.body;
		init.duplex = "half";
	}

	const upstream = await fetch(target, init);
	const responseHeaders = new Headers(upstream.headers);
	for (const header of Object.keys(HOP_BY_HOP)) {
		responseHeaders.delete(header);
	}
	responseHeaders.delete("set-cookie");

	const getSetCookie = (upstream.headers as Headers & { getSetCookie?: () => string[] }).getSetCookie;
	if (getSetCookie) {
		for (const cookie of getSetCookie.call(upstream.headers)) {
			responseHeaders.append("set-cookie", cookie);
		}
	}

	return new Response(upstream.body, {
		status: upstream.status,
		headers: responseHeaders,
	});
};
