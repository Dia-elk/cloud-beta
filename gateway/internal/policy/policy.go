// Package policy loads a tenant policy file and answers two questions:
// (1) is this role allowed to do this action on this table (gateway layer),
// (2) what Postgres RLS SQL enforces tenant isolation (database layer).
// Default is deny: anything not listed is not allowed.
package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type TableRule struct {
	TenantField string   `json:"tenant_field"`
	Read        []string `json:"read"`
	Write       []string `json:"write"`
	Delete      []string `json:"delete"`
}

type Policy struct {
	TenantField string               `json:"tenant_field"`
	Source      string               `json:"source"`
	Default     string               `json:"default"`
	Roles       map[string][]string  `json:"roles"`
	Tables      map[string]TableRule `json:"tables"`
}

func Load(path string) (*Policy, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Policy
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	if p.Default != "deny" {
		return nil, fmt.Errorf("policy default must be deny, got %q", p.Default)
	}
	return &p, nil
}

func has(list []string, role string) bool {
	for _, r := range list {
		if r == role {
			return true
		}
	}
	return false
}

// Allowed is the gateway-layer check. action is read, write or delete.
func (p *Policy) Allowed(table, action, role string) bool {
	t, ok := p.Tables[table]
	if !ok {
		return false // unknown table: default deny
	}
	switch action {
	case "read":
		return has(t.Read, role)
	case "write":
		return has(t.Write, role)
	case "delete":
		return has(t.Delete, role)
	default:
		return false
	}
}

// RLS generates Postgres Row Level Security for every tenant table.
// Fix 3: policies are now role-aware, not company-only. Every policy
// compares the row's tenant field to the session variable the gateway
// sets on every request (app.current_company_id) AND the session role
// (app.current_role) to that operation's allowed list in policy.json.
// No row is visible without a tenant match, and no write/delete is
// possible for a role not listed for that operation — even if Go code
// is bypassed and a caller talks to Postgres directly as cloud_app.
// An empty allowed list denies that operation outright (false).
func roleExpr(list []string) string {
	if len(list) == 0 {
		return "false"
	}
	quoted := make([]string, len(list))
	for i, r := range list {
		quoted[i] = "'" + r + "'"
	}
	return fmt.Sprintf("current_setting('app.current_role', true) = ANY(ARRAY[%s])", strings.Join(quoted, ","))
}

func (p *Policy) RLS() string {
	var sb strings.Builder
	for table, t := range p.Tables {
		field := t.TenantField
		if field == "" {
			field = p.TenantField
		}
		var tenant string
		if field == "id" {
			tenant = "id = current_setting('app.current_company_id', true)"
		} else {
			tenant = fmt.Sprintf("%s = current_setting('app.current_company_id', true)", field)
		}
		expr := func(list []string) string {
			if len(list) == 0 {
				return "false"
			}
			return fmt.Sprintf("(%s AND %s)", tenant, roleExpr(list))
		}
		fmt.Fprintf(&sb, "ALTER TABLE %s ENABLE ROW LEVEL SECURITY;\n", table)
		for _, pol := range []string{"tenant_select", "tenant_insert", "tenant_update", "tenant_delete"} {
			fmt.Fprintf(&sb, "DROP POLICY IF EXISTS %s ON %s;\n", pol, table)
		}
		fmt.Fprintf(&sb, "CREATE POLICY tenant_select ON %s FOR SELECT USING (%s);\n", table, expr(t.Read))
		fmt.Fprintf(&sb, "CREATE POLICY tenant_insert ON %s FOR INSERT WITH CHECK (%s);\n", table, expr(t.Write))
		fmt.Fprintf(&sb, "CREATE POLICY tenant_update ON %s FOR UPDATE USING (%s) WITH CHECK (%s);\n", table, expr(t.Write), expr(t.Write))
		fmt.Fprintf(&sb, "CREATE POLICY tenant_delete ON %s FOR DELETE USING (%s);\n", table, expr(t.Delete))
	}
	// Memberships: a user may see their own row, or rows of their company.
	sb.WriteString(`ALTER TABLE memberships ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_select ON memberships;
CREATE POLICY tenant_select ON memberships FOR SELECT USING (user_id = current_setting('app.current_user_id', true) OR company_id = current_setting('app.current_company_id', true));
`)
	// Users: only your own row.
	sb.WriteString(`ALTER TABLE users ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_select ON users;
CREATE POLICY tenant_select ON users FOR SELECT USING (id = current_setting('app.current_user_id', true));
`)
	return sb.String()
}
