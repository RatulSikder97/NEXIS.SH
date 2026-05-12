// Phase 3 Stage 6 — /dashboard collapses into the console shell.
// Kept as a redirect so any Phase 2 links and old bookmarks continue to work.

import { redirect } from "next/navigation";

export default function Dashboard() {
  redirect("/console");
}
