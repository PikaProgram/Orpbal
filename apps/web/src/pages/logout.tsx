import { useEffect } from "react";
import { Navigate, useNavigate } from "react-router-dom";

import { useSession } from "@/auth/session";

export function LogoutPage() {
	const { logOut, isAuthed } = useSession();
	const navigate = useNavigate();

	useEffect(() => {
		void logOut().then(() => navigate("/login", { replace: true }));
	}, [logOut, navigate]);

	// The effect drives navigation; the layout header stays visible meanwhile.
	return isAuthed ? <p className="text-sm text-dim">Signing out…</p> : <Navigate to="/login" replace />;
}
