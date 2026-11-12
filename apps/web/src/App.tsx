import { lazy, Suspense, type ReactNode } from "react";
import { Navigate, Route, Routes } from "react-router-dom";

import { GuestOnly, RequireAdmin, RequireAuth } from "@/auth/guards";
import { SessionProvider } from "@/auth/session";
import { AppLayout } from "@/layouts/AppLayout";
import { DatabasesPage } from "@/pages/databases";
import { LoginPage } from "@/pages/login";
import { LogoutPage } from "@/pages/logout";
import { RootRedirect } from "@/pages/root";
import { NotFoundPage } from "@/pages/not-found";

// Heavy routes are code-split so the initial bundle stays lean for mobile.
const DatabaseDetailPage = lazy(() =>
	import("@/pages/databases/[id]").then((m) => ({ default: m.DatabaseDetailPage })),
);
const AdminUsersPage = lazy(() =>
	import("@/pages/admin/users").then((m) => ({ default: m.AdminUsersPage })),
);
const AdminGroupsPage = lazy(() =>
	import("@/pages/admin/groups").then((m) => ({ default: m.AdminGroupsPage })),
);
const AdminConnectionsPage = lazy(() =>
	import("@/pages/admin/databases").then((m) => ({ default: m.AdminConnectionsPage })),
);
const AdminGrantsPage = lazy(() =>
	import("@/pages/admin/grants").then((m) => ({ default: m.AdminGrantsPage })),
);

function ChunkFallback() {
	return (
		<div className="flex h-full min-h-[50vh] items-center justify-center text-sm text-dim">
			Loading…
		</div>
	);
}

function SuspenseRoute({ children }: { children: ReactNode }) {
	return <Suspense fallback={<ChunkFallback />}>{children}</Suspense>;
}

export function App() {
	return (
		<SessionProvider>
			<Routes>
				<Route element={<GuestOnly />}>
					<Route path="/login" element={<LoginPage />} />
				</Route>
				<Route element={<RequireAuth />}>
					<Route element={<AppLayout />}>
						<Route path="/" element={<RootRedirect />} />
						<Route path="/logout" element={<LogoutPage />} />
						<Route path="/databases" element={<DatabasesPage />} />
						<Route
							path="/databases/:id"
							element={<SuspenseRoute><DatabaseDetailPage /></SuspenseRoute>}
						/>
						<Route element={<RequireAdmin />}>
							<Route
								path="/admin/users"
								element={<SuspenseRoute><AdminUsersPage /></SuspenseRoute>}
							/>
							<Route
								path="/admin/groups"
								element={<SuspenseRoute><AdminGroupsPage /></SuspenseRoute>}
							/>
							<Route
								path="/admin/databases"
								element={<SuspenseRoute><AdminConnectionsPage /></SuspenseRoute>}
							/>
							<Route
								path="/admin/grants"
								element={<SuspenseRoute><AdminGrantsPage /></SuspenseRoute>}
							/>
						</Route>
					</Route>
				</Route>
				<Route path="/api/*" element={<Navigate to="/" replace />} />
				<Route path="*" element={<NotFoundPage />} />
			</Routes>
		</SessionProvider>
	);
}
