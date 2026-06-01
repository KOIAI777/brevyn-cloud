import type { LucideIcon } from "lucide-react";

type Stat = {
  label: string;
  value: string;
  delta: string;
  tone: "green" | "amber" | "cyan" | "red";
  icon: LucideIcon;
};

type StatGridProps = {
  stats: Stat[];
};

export function StatGrid({ stats }: StatGridProps) {
  return (
    <section className="stat-grid">
      {stats.map((stat) => (
        <article className={`metric-card ${stat.tone}`} key={stat.label}>
          <div className="metric-top">
            <span>{stat.label}</span>
            <stat.icon size={18} />
          </div>
          <strong>{stat.value}</strong>
          <small>{stat.delta}</small>
        </article>
      ))}
    </section>
  );
}
