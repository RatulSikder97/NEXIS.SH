export type StepItemProps = {
  number: string;
  title: string;
  description: string;
  active?: boolean;
  className?: string;
};

export function StepItem({
  number,
  title,
  description,
  active = false,
  className,
}: StepItemProps) {
  return (
    <div
      className={[
        "relative min-h-[140px] rounded-[12px] p-6 transition-colors duration-150 ease-out",
        active ? "border border-border-hover" : "border border-transparent",
        className,
      ].join(" ")}
    >
      <div className="flex items-start gap-4">
        <div
          className={[
            "font-mono text-[12px] transition-colors duration-150 ease-out",
            active ? "text-primary" : "text-text-muted",
          ].join(" ")}
        >
          {number}
        </div>
        <div>
          <h3 className="text-[18px] font-medium tracking-[-0.01em] text-accent">
            {title}
          </h3>
          <p className="mt-2 max-w-[760px] text-[15px] leading-[1.75] text-text-secondary">
            {description}
          </p>
        </div>
      </div>

      <div
        className={[
          "absolute inset-x-0 top-0 h-[1px]",
          active ? "bg-primary/70" : "bg-primary/20",
        ].join(" ")}
      />
    </div>
  );
}

