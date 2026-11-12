import { useState } from "react";
import { NavLink, Outlet } from "react-router-dom";

import { useSession } from "@/auth/session";

const navLink = ({ isActive }: { isActive: boolean }) =>
	`text-sm transition-colors hover:text-brass-lit focus-visible:text-brass-lit ${isActive ? "text-brass-lit" : "text-dim"}`;

export function AppLayout() {
	const { session } = useSession();
	const user = session?.user;
	const [menuOpen, setMenuOpen] = useState(false);

	const links = user
		? [
				{ to: "/databases", label: "Databases" },
				...(user.is_platform_admin
					? [
							{ to: "/admin/users", label: "Users" },
							{ to: "/admin/groups", label: "Groups" },
							{ to: "/admin/databases", label: "Connections" },
							{ to: "/admin/grants", label: "Grants" },
						]
					: []),
				{ to: "/logout", label: "Sign out" },
			]
		: [];

	return (
		<div className="flex min-h-screen flex-col">
			<header className="border-b-2 border-seam bg-plate/60">
				<div className="container mx-auto flex items-center justify-between gap-4 px-4 py-3">
					<NavLink to="/" className="text-lg font-bold tracking-tight text-foreground">
						Orpbal
					</NavLink>
					{user && (
						<>
							<nav className="hidden flex-wrap items-center gap-4 md:flex">
								{links.map((link) => (
									<NavLink key={link.to} to={link.to} className={navLink}>
										{link.label}
									</NavLink>
								))}
							</nav>
							<button
								type="button"
								className="btn-steel px-3 py-2 text-xs md:hidden"
								aria-expanded={menuOpen}
								aria-controls="mobile-nav"
								onClick={() => setMenuOpen((open) => !open)}
							>
								Menu
							</button>
						</>
					)}
				</div>
				{user && menuOpen && (
					<nav id="mobile-nav" className="flex flex-col gap-1 border-t border-seam px-4 py-3 md:hidden">
						{links.map((link) => (
							<NavLink key={link.to} to={link.to} className={navLink} onClick={() => setMenuOpen(false)}>
								{link.label}
							</NavLink>
						))}
					</nav>
				)}
			</header>
			<div className="h-1 bg-brass"></div>
			<main className="container mx-auto w-full px-4 py-8">
				<Outlet />
			</main>
		</div>
	);
}
