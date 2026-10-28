package policy

import (
	"strings"
	"testing"
)

func testPolicy() *Policy {
	p, err := Load("../../policy.json")
	if err != nil {
		panic(err)
	}
	return p
}

func TestDefaultDeny(t *testing.T) {
	p := testPolicy()
	if p.Allowed("unknown_table", "read", "owner") {
		t.Fatal("unknown table must be denied")
	}
	if p.Allowed("clients", "delete", "accountant") {
		t.Fatal("accountant must not delete")
	}
	if p.Allowed("clients", "write", "accountant") {
		t.Fatal("accountant must not write")
	}
	if p.Allowed("clients", "delete", "employee") {
		t.Fatal("employee must not delete")
	}
	if !p.Allowed("clients", "read", "accountant") || !p.Allowed("clients", "write", "employee") || !p.Allowed("clients", "delete", "owner") {
		t.Fatal("owner/employee/accountant base permissions broken")
	}
	if p.Allowed("audit_logs", "write", "owner") {
		t.Fatal("app must never write audit logs directly; only the platform writes them")
	}
}

func TestRLSContainsTenantCheck(t *testing.T) {
	sql := testPolicy().RLS()
	for _, want := range []string{
		"ALTER TABLE clients ENABLE ROW LEVEL SECURITY",
		"company_id = current_setting('app.current_company_id', true)",
		"current_setting('app.current_role', true)",
		"CREATE POLICY tenant_select ON clients",
		"CREATE POLICY tenant_insert ON clients",
		"CREATE POLICY tenant_delete ON clients",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("RLS missing %q", want)
		}
	}
}

// Fix 3: RLS must be role-aware, not company-only. An accountant must
// not appear in any clients INSERT/DELETE policy expression, and an
// empty allowed list (audit_logs write, audit_logs delete) must deny.
func TestRLSRoleAware(t *testing.T) {
	sql := testPolicy().RLS()
	// Extract the clients policies: INSERT and DELETE lines must not
	// grant accountant. INSERT write list is owner+employee only;
	// DELETE list is owner only.
	for _, line := range strings.Split(sql, "\n") {
		if strings.Contains(line, "CREATE POLICY tenant_insert ON clients") && strings.Contains(line, "'accountant'") {
			t.Fatalf("clients INSERT policy must not include accountant: %s", line)
		}
		if strings.Contains(line, "CREATE POLICY tenant_delete ON clients") {
			if strings.Contains(line, "'accountant'") || strings.Contains(line, "'employee'") {
				t.Fatalf("clients DELETE policy must be owner only: %s", line)
			}
			if !strings.Contains(line, "'owner'") {
				t.Fatalf("clients DELETE policy must include owner: %s", line)
			}
		}
		if strings.Contains(line, "CREATE POLICY tenant_insert ON audit_logs") && !strings.Contains(line, "false") {
			t.Fatalf("audit_logs INSERT policy must deny (empty write list): %s", line)
		}
		if strings.Contains(line, "CREATE POLICY tenant_delete ON audit_logs") && !strings.Contains(line, "false") {
			t.Fatalf("audit_logs DELETE policy must deny (empty delete list): %s", line)
		}
	}
	if !strings.Contains(sql, "CREATE POLICY tenant_select ON clients") {
		t.Fatal("clients SELECT policy missing")
	}
}
