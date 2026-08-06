package api

import (
	"testing"
)

func TestVersions(t *testing.T) {
	vs := Versions()
	if len(vs) == 0 {
		t.Fatal("expected at least one embedded version")
	}
	if vs[0] != "25.04" {
		t.Errorf("expected first version 25.04, got %s", vs[0])
	}
}

func TestLatestVersion(t *testing.T) {
	v := LatestVersion()
	if v == "" {
		t.Fatal("expected non-empty latest version")
	}
	// Updated from 25.04 when the 25.10 schema was embedded. LatestVersion is
	// the FEATURES.md denominator, so this moving is exactly what regenerates
	// that file for every service.
	if v != "25.10" {
		t.Errorf("expected latest version 25.10, got %s", v)
	}
}

func TestResolveVersion(t *testing.T) {
	tests := []struct {
		name         string
		major, minor int
		want         string
	}{
		{"exact 25.04", 25, 4, "25.04"},
		{"exact 25.10", 25, 10, "25.10"},
		// The case that matters: a patch release resolves to its own minor,
		// never down to an older one that may still list removed methods.
		{"25.10.5 appliance", 25, 10, "25.10"},
		// Between embedded schemas: falls back to the highest not exceeding.
		{"25.05 between", 25, 5, "25.04"},
		{"25.09 between", 25, 9, "25.04"},
		// Newer than anything embedded: deliberately clamps to the newest,
		// under-reporting new methods rather than over-reporting removed ones.
		{"26.0 future", 26, 0, "25.10"},
		{"27.0 far future", 27, 0, "25.10"},
		// Older than everything embedded: no answer, rather than a wrong one.
		{"24.10 too old", 24, 10, ""},
		{"22.02 far too old", 22, 2, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveVersion(tt.major, tt.minor); got != tt.want {
				t.Errorf("ResolveVersion(%d, %d) = %q, want %q", tt.major, tt.minor, got, tt.want)
			}
		})
	}
}

func TestMethodsForVersion(t *testing.T) {
	m, v, err := MethodsForVersion(25, 10)
	if err != nil {
		t.Fatalf("MethodsForVersion(25, 10) error: %v", err)
	}
	if v != "25.10" {
		t.Errorf("expected schema 25.10, got %s", v)
	}
	// share_precheck is 25.10-only; its presence proves the right schema
	// answered rather than a fallback to 25.04.
	if _, ok := m["sharing.smb.share_precheck"]; !ok {
		t.Error("expected sharing.smb.share_precheck in the 25.10 schema")
	}

	m04, v04, err := MethodsForVersion(25, 4)
	if err != nil {
		t.Fatalf("MethodsForVersion(25, 4) error: %v", err)
	}
	if v04 != "25.04" {
		t.Errorf("expected schema 25.04, got %s", v04)
	}
	if _, ok := m04["sharing.smb.share_precheck"]; ok {
		t.Error("sharing.smb.share_precheck must NOT be in the 25.04 schema")
	}
}

func TestMethodsForVersion_TooOld(t *testing.T) {
	_, _, err := MethodsForVersion(24, 10)
	if err == nil {
		t.Fatal("expected an error for an appliance older than every embedded schema")
	}
}

// Every embedded schema must declare its provenance. Without this, adding a
// directory silently produces a schema nobody can tell is unverified.
func TestProvenanceCoversEveryEmbeddedVersion(t *testing.T) {
	for _, v := range Versions() {
		p, ok := ProvenanceFor(v)
		if !ok {
			t.Errorf("embedded schema %s has no provenance entry", v)
			continue
		}
		if p.Source == "" {
			t.Errorf("provenance for %s has an empty Source", v)
		}
		if !p.Verified && p.Note == "" {
			t.Errorf("provenance for %s is unverified but carries no explanatory Note", v)
		}
	}
}

// The 25.10 schema is knowingly interim. This test is the tripwire: when it is
// re-fetched from a real 25.10.5 appliance and Verified flips to true, this
// test fails and forces the note, the ADR-004 Evidence section, and the
// tracking issue to be closed out together rather than silently drifting.
func TestProvenance_25_10_IsStillInterim(t *testing.T) {
	p, ok := ProvenanceFor("25.10")
	if !ok {
		t.Fatal("no provenance for 25.10")
	}
	if p.Verified {
		t.Fatal("25.10 is now marked verified — update ADR-004's Evidence section, " +
			"close the re-fetch issue, and delete this test")
	}
}

func TestMethods(t *testing.T) {
	methods, err := Methods("25.04")
	if err != nil {
		t.Fatalf("Methods(25.04) error: %v", err)
	}
	if len(methods) == 0 {
		t.Fatal("expected non-empty methods map")
	}

	// Spot-check a known method
	m, ok := methods["system.info"]
	if !ok {
		t.Fatal("expected system.info to be present")
	}
	if m.Description == nil {
		t.Error("expected system.info to have a description")
	}
}

func TestMethods_InvalidVersion(t *testing.T) {
	_, err := Methods("99.99")
	if err == nil {
		t.Fatal("expected error for invalid version")
	}
}

func TestNamespace(t *testing.T) {
	tests := []struct {
		method string
		want   string
	}{
		{"app.create", "app"},
		{"app.registry.create", "app.registry"},
		{"system.info", "system"},
		{"pool.dataset.query", "pool.dataset"},
		{"standalone", "standalone"},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			got := Namespace(tt.method)
			if got != tt.want {
				t.Errorf("Namespace(%q) = %q, want %q", tt.method, got, tt.want)
			}
		})
	}
}

func TestMethods_KnownFields(t *testing.T) {
	methods, err := Methods("25.04")
	if err != nil {
		t.Fatalf("Methods(25.04) error: %v", err)
	}

	// Check a job method
	if m, ok := methods["app.create"]; ok {
		if !m.Job {
			t.Error("expected app.create to be a job method")
		}
	}

	// Check a filterable method
	if m, ok := methods["app.query"]; ok {
		if !m.Filterable {
			t.Error("expected app.query to be filterable")
		}
	}
}
