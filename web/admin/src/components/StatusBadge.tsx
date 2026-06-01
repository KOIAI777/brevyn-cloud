type StatusBadgeProps = {
  children: string;
  tone?: "ok" | "warn" | "danger" | "neutral";
};

export function StatusBadge({ children, tone = "neutral" }: StatusBadgeProps) {
  return <span className={`badge ${tone}`}>{children}</span>;
}
