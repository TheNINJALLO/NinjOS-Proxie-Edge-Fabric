package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateRejectsPublicHTTP(t *testing.T) {
	cfg := config{ServerID: "creative", DashboardURL: "http://203.0.113.10:25571", SharedSecret: "1234567890123456"}
	if validate(cfg) == nil {
		t.Fatal("public plain HTTP should require explicit insecureHttp")
	}
}

func TestUpdatePermissionsPreservesMembersAndReplacesOperators(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "permissions.json")
	initial := []permissionRow{
		{Permission: "member", XUID: "100"},
		{Permission: "operator", XUID: "200"},
	}
	data, _ := json.Marshal(initial)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	operators := []operatorEntry{{XUID: "300", Gamertag: "Admin", Permission: "operator", Role: "administrator"}}
	if err := updatePermissions(path, operators); err != nil {
		t.Fatal(err)
	}
	var rows []permissionRow
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(saved, &rows); err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	for _, row := range rows {
		found[row.XUID] = row.Permission
	}
	if found["100"] != "member" {
		t.Fatalf("member row was not preserved: %#v", found)
	}
	if _, exists := found["200"]; exists {
		t.Fatalf("old synchronized operator remained: %#v", found)
	}
	if found["300"] != "operator" {
		t.Fatalf("new operator missing: %#v", found)
	}
}
