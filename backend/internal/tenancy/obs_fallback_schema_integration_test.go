package tenancy

import (
	"testing"
)

func TestOBSFallbackMigrationContract(t *testing.T) {
	s, a, _, _, _, f := tenantPair(t)
	rows, err := f.Admin.Query(`SELECT c.conname,t.relname,pg_get_constraintdef(c.oid) FROM pg_constraint c JOIN pg_class t ON t.oid=c.conrelid WHERE c.conrelid IN('obs_fallback_rules'::regclass,'obs_fallback_proposals'::regclass)`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Error(err)
		}
	}()
	definitions := map[string]struct{ Table, Definition string }{}
	for rows.Next() {
		var name, table, definition string
		if err := rows.Scan(&name, &table, &definition); err != nil {
			t.Fatal(err)
		}
		definitions[name] = struct{ Table, Definition string }{table, definition}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for name, want := range obsFallbackConstraints {
		got, ok := definitions[name]
		if !ok || got.Table != want.table || got.Definition != want.definition {
			t.Fatalf("constraint %s: %#v", name, got)
		}
	}
	scope, err := s.Product.Scoped(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewOBSFallbackStore(t.Context(), scope); err != nil {
		t.Fatal(err)
	}
}
