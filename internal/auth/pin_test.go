package auth

import (
	"path/filepath"
	"testing"
)

func TestPINLifecycle(t *testing.T) {
	m := New(filepath.Join(t.TempDir(), "tasks.json"))
	if exists, err := m.Exists(); err != nil || exists {
		t.Fatalf("Exists = %v, %v", exists, err)
	}
	if err := m.Set("1234"); err != nil {
		t.Fatal(err)
	}
	if ok, err := m.Verify("1234"); err != nil || !ok {
		t.Fatalf("correct PIN = %v, %v", ok, err)
	}
	if ok, err := m.Verify("4321"); err != nil || ok {
		t.Fatalf("wrong PIN = %v, %v", ok, err)
	}
	if err := m.Set("12ab"); err == nil {
		t.Fatal("accepted invalid PIN")
	}
}

func TestProfileName(t *testing.T) {
	m := New(filepath.Join(t.TempDir(), "tasks.json"))
	if err := m.SetProfile("2468", "Ada"); err != nil {
		t.Fatal(err)
	}
	name, err := m.Name()
	if err != nil || name != "Ada" {
		t.Fatalf("name = %q, %v", name, err)
	}
	if err := m.SetName("Grace"); err != nil {
		t.Fatal(err)
	}
	name, err = m.Name()
	if err != nil || name != "Grace" {
		t.Fatalf("updated name = %q, %v", name, err)
	}
	if ok, err := m.Verify("2468"); err != nil || !ok {
		t.Fatalf("PIN after rename = %v, %v", ok, err)
	}
}
