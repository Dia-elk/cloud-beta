export type Role = "owner" | "employee" | "accountant";
export type Op = "read" | "write" | "delete";

export type Me = { user_id: string; email: string; company_id: string; company_name: string; role: Role };
export type Company = { id: string; name: string; role: Role };
export type Member = { user_id: string; email: string; role: Role };

export type AppStatus = "registered" | "deploying" | "running" | "stopped" | "failed";
export type App = { id: string; name: string; repo: string; url: string; status: AppStatus; container_id: string; port: number };

export type Client = { id: string; name: string; email: string; notes: string; created_at: string };

export type Share = {
  id: string;
  table_name: string;
  row_id: string;
  row_name: string;
  expires_at: string;
  revoked: boolean;
  created_at: string;
};

export type AuditRow = {
  action: string;
  table: string;
  target_id: string;
  allowed: boolean;
  reason: string;
  latency_ms: number;
  created_at: string;
  actor_role: string;
};

export type AuditSummary = {
  total: number;
  allowed: number;
  denied: number;
  avg_latency_ms: number;
  per_day: { date: string; total: number; allowed: number; denied: number }[];
};

export type Proposal = {
  id: string;
  title: string;
  diff: string;
  status: "pending" | "approved" | "applied";
  applied: boolean;
  fix_kind: string;
  fix_column: string;
  applyable: boolean;
  created_at: string;
};

export type TableRule = { tenant_field: string; read: Role[]; write: Role[]; delete: Role[] };
export type Policy = {
  tenant_field: string;
  source: string;
  default: string;
  roles: Record<Role, string[]>;
  tables: Record<string, TableRule>;
};
