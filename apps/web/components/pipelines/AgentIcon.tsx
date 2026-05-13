// Phase 4 Stage 7 — AgentIcon.
//
// Maps a domain.AgentRole string to a lucide icon. Returns a small (h-4 w-4)
// icon by default; consumers can pass `className` to resize. Unknown roles
// fall back to a generic Workflow icon so we never crash on a future role
// the UI doesn't know about yet.

import {
  Activity,
  Brain,
  CheckCircle2,
  Code2,
  Compass,
  Database,
  FlaskConical,
  Radar,
  Ruler,
  ShieldCheck,
  Workflow,
  Wrench,
  type LucideIcon,
} from "lucide-react";

import { cn } from "@/lib/utils";

const ICONS: Record<string, LucideIcon> = {
  sentinel: Radar,
  pathfinder: Compass,
  synthesiser: Brain,
  architect: Ruler,
  backend: Code2,
  qa: FlaskConical,
  devops: Wrench,
  data_engineer: Database,
  approval_gate: ShieldCheck,
  pipeline: CheckCircle2,
};

export function AgentIcon({
  role,
  className,
}: {
  role: string;
  className?: string;
}) {
  const Icon = ICONS[role] ?? Activity ?? Workflow;
  return <Icon className={cn("h-4 w-4", className)} aria-hidden />;
}
