const PLACEHOLDERS = [
  "Acme Co.",
  "Sandwell",
  "Northpoint",
  "Veritas",
  "Halcyon",
  "Brightline",
];

export default function TrustedStrip() {
  return (
    <section
      aria-label="Trusted by teams"
      className="bg-[var(--color-muted)] py-6"
    >
      <div className="mx-auto max-w-[1200px] px-6">
        <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)] mb-4 text-center">
          Trusted by engineering teams that hate alert fatigue
        </p>
        <ul className="flex flex-wrap items-center justify-center gap-8 grayscale opacity-70">
          {PLACEHOLDERS.map((name) => (
            <li
              key={name}
              className="text-sm font-semibold text-[var(--color-muted-foreground)]"
            >
              {name}
            </li>
          ))}
        </ul>
      </div>
    </section>
  );
}
