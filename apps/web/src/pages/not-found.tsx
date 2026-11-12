import { Link } from "react-router-dom";

export function NotFoundPage() {
	return (
		<div className="mx-auto max-w-md py-16 text-center">
			<h1 className="mb-2 text-2xl font-bold tracking-tight">404</h1>
			<p className="mb-6 text-sm text-dim">There is nothing at this address.</p>
			<Link to="/" className="btn-brass inline-block px-4 py-2 text-sm">
				Back to Orpbal
			</Link>
		</div>
	);
}
