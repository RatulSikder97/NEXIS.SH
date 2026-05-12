// Phase 3 Stage 8 — Settings/Preferences.
//
// Currently exposes one preference: theme (light/dark/system). The UI calls
// both next-themes' setTheme() (immediate visual update) and the
// control-plane's PATCH /v1/me/preferences endpoint (persist across devices).
//
// We keep this as a thin client component — the persisted value is hydrated
// on first paint via useEffect → preferences.get(); next-themes is the source
// of truth for the live class on <html>.

import { PreferencesClient } from "./client";

export default function PreferencesPage() {
  return <PreferencesClient />;
}
