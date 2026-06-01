import { useQuery } from "@tanstack/react-query";
import { getAdminHealth, getHealth, getReady } from "../api/client";

export function useServiceHealth() {
  const live = useQuery({ queryKey: ["healthz"], queryFn: getHealth });
  const ready = useQuery({ queryKey: ["readyz"], queryFn: getReady });
  const admin = useQuery({ queryKey: ["admin-health"], queryFn: getAdminHealth });

  return { live, ready, admin };
}
