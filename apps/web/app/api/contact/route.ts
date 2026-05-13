// /api/contact — Stub handler for the /contact page form. Validates input,
// logs to stdout (the controller wires this up to a real CRM later) and
// returns { ok: true }. No persistence yet by design.
import { NextResponse } from "next/server";

type ContactPayload = {
  name?: unknown;
  email?: unknown;
  company?: unknown;
  message?: unknown;
};

export async function POST(req: Request) {
  let body: ContactPayload;
  try {
    body = (await req.json()) as ContactPayload;
  } catch {
    return NextResponse.json({ error: "invalid JSON" }, { status: 400 });
  }

  const name = typeof body.name === "string" ? body.name.trim().slice(0, 120) : "";
  const email =
    typeof body.email === "string" ? body.email.trim().toLowerCase().slice(0, 160) : "";
  const company =
    typeof body.company === "string" ? body.company.trim().slice(0, 160) : "";
  const message =
    typeof body.message === "string" ? body.message.trim().slice(0, 4000) : "";

  if (!name || !email || !message) {
    return NextResponse.json(
      { error: "name, email, message required" },
      { status: 400 },
    );
  }
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
    return NextResponse.json({ error: "valid email required" }, { status: 400 });
  }

  // No real CRM hookup yet — log it so the dev server still surfaces traffic.
  console.log("[contact]", { name, email, company, length: message.length });

  return NextResponse.json({ ok: true });
}
