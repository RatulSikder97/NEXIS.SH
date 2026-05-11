import { Badge } from "@/components/ui/Badge";

export type AgentCardProps = {
  id: string;
  name: string;
  layerLabel: string;
  role: string;
};

export function AgentCard({ id, name, layerLabel, role }: AgentCardProps) {
  return (
    <div className="relative rounded-[12px] border border-border bg-surface p-5 transition-transform duration-150 ease-out hover:-translate-y-[2px] hover:border-border-hover">
      <div className="flex items-start justify-between gap-4">
        <Badge>{layerLabel}</Badge>
        <div className="font-mono text-[11px] text-text-muted">{id}</div>
      </div>

      <div className="mt-4">
        <div className="text-[15px] font-medium text-accent">{name}</div>
        <div className="mt-2 truncate text-[13px] text-text-secondary">
          {role}
        </div>
      </div>
    </div>
  );
}

