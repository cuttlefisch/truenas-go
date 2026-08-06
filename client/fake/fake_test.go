package fake

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	truenas "github.com/deevus/truenas-go"
	"github.com/deevus/truenas-go/client"
)

// dial builds a real WebSocketClient pointed at the fake, so every test below
// exercises the actual client code path rather than the fake in isolation.
func dial(t *testing.T, s *Server) client.Client {
	t.Helper()
	host, port := s.HostPort()
	c, err := client.NewWebSocketClient(client.WebSocketConfig{
		Host:     host,
		Port:     port,
		Username: "root",
		APIKey:   "test-key",
		// The fake serves TLS with a self-signed certificate, so this is the
		// real config field doing its real job rather than a test-only hook.
		InsecureSkipVerify: true,
		ConnectTimeout:     5 * time.Second,
		MaxRetries:         1,
		Fallback: &client.MockClient{
			VersionVal:  truenas.Version{Major: 25, Minor: 10},
			ConnectFunc: func(ctx context.Context) error { return nil },
		},
	})
	if err != nil {
		t.Fatalf("NewWebSocketClient: %v", err)
	}
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// TestFake_DetectsNonIdempotentCreate is the reason this package exists.
//
// It registers a normalization hook that upper-cases a field — the server
// storing something other than what the client sent — and asserts the drift is
// observable on read-back. That is the shape of every "terraform plan is never
// empty" bug this tier is meant to catch.
//
// The second half is what makes it a real gate rather than a demonstration:
// with the hook removed, the same assertions must NOT hold. A test that passes
// both with and without the defect present is not detecting anything.
func TestFake_DetectsNonIdempotentCreate(t *testing.T) {
	const sent = "MyShare"

	t.Run("with the normalization hook, drift is visible", func(t *testing.T) {
		s := New()
		defer s.Close()
		s.Normalize("sharing.smb", func(p map[string]any) map[string]any {
			out := copyMap(p)
			if name, ok := out["name"].(string); ok {
				out["name"] = strings.ToUpper(name) // what the real middleware does
			}
			return out
		})

		c := dial(t, s)
		raw, err := c.Call(context.Background(), "sharing.smb.create",
			[]any{map[string]any{"name": sent, "path": "/mnt/tank/x"}})
		if err != nil {
			t.Fatalf("create: %v", err)
		}

		var created map[string]any
		if err := json.Unmarshal(raw, &created); err != nil {
			t.Fatalf("decoding create response: %v", err)
		}
		if created["name"] == sent {
			t.Fatal("server echoed the name back unchanged — the hook did not fire")
		}
		if created["name"] != strings.ToUpper(sent) {
			t.Errorf("stored name = %v, want %q", created["name"], strings.ToUpper(sent))
		}

		// And the drift survives a fresh read, which is where a provider's
		// second plan would see it.
		got := readBackName(t, c, toInt64(created["id"]))
		if got == sent {
			t.Error("read-back matched what was sent; an idempotency defect would be invisible")
		}
	})

	t.Run("without the hook, there is no drift to detect", func(t *testing.T) {
		s := New() // deliberately no Normalize call
		defer s.Close()

		c := dial(t, s)
		raw, err := c.Call(context.Background(), "sharing.smb.create",
			[]any{map[string]any{"name": sent, "path": "/mnt/tank/x"}})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		var created map[string]any
		if err := json.Unmarshal(raw, &created); err != nil {
			t.Fatalf("decoding create response: %v", err)
		}

		// This is the control. If the fake mutated payloads on its own, the
		// test above would pass for the wrong reason and every namespace would
		// inherit a phantom defect.
		if created["name"] != sent {
			t.Errorf("server mutated %q to %v with no hook registered", sent, created["name"])
		}
		if got := readBackName(t, c, toInt64(created["id"])); got != sent {
			t.Errorf("read-back = %q, want %q", got, sent)
		}
	})
}

func readBackName(t *testing.T, c client.Client, id int64) string {
	t.Helper()
	raw, err := c.Call(context.Background(), "sharing.smb.get_instance", []any{id})
	if err != nil {
		t.Fatalf("get_instance: %v", err)
	}
	var rec map[string]any
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("decoding get_instance: %v", err)
	}
	name, _ := rec["name"].(string)
	return name
}

func TestFake_CRUDRoundTrip(t *testing.T) {
	s := New()
	defer s.Close()
	c := dial(t, s)
	ctx := context.Background()

	raw, err := c.Call(ctx, "sharing.nfs.create", []any{map[string]any{"path": "/mnt/tank/a"}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var created map[string]any
	_ = json.Unmarshal(raw, &created)
	id := toInt64(created["id"])
	if id == 0 {
		t.Fatal("create returned no id")
	}

	// query returns it
	raw, err = c.Call(ctx, "sharing.nfs.query", []any{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	var all []map[string]any
	_ = json.Unmarshal(raw, &all)
	if len(all) != 1 {
		t.Fatalf("query returned %d records, want 1", len(all))
	}

	// update mutates only what was patched
	if _, err := c.Call(ctx, "sharing.nfs.update", []any{id, map[string]any{"comment": "hi"}}); err != nil {
		t.Fatalf("update: %v", err)
	}
	raw, _ = c.Call(ctx, "sharing.nfs.get_instance", []any{id})
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	if got["comment"] != "hi" {
		t.Errorf("comment = %v, want hi", got["comment"])
	}
	if got["path"] != "/mnt/tank/a" {
		t.Errorf("update clobbered path: %v", got["path"])
	}

	// delete removes it, and a second get reports not-found
	if _, err := c.Call(ctx, "sharing.nfs.delete", []any{id}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := c.Call(ctx, "sharing.nfs.get_instance", []any{id}); err == nil {
		t.Error("expected an error reading a deleted record")
	}
	if n := len(s.Records("sharing.nfs")); n != 0 {
		t.Errorf("%d records remain server-side after delete", n)
	}
}

// Namespaces must not bleed into one another — a bug that would make every
// cross-resource test meaningless.
func TestFake_NamespacesAreIsolated(t *testing.T) {
	s := New()
	defer s.Close()
	c := dial(t, s)
	ctx := context.Background()

	_, _ = c.Call(ctx, "sharing.nfs.create", []any{map[string]any{"path": "/a"}})
	_, _ = c.Call(ctx, "sharing.smb.create", []any{map[string]any{"path": "/b"}})

	if n := len(s.Records("sharing.nfs")); n != 1 {
		t.Errorf("sharing.nfs has %d records, want 1", n)
	}
	if n := len(s.Records("sharing.smb")); n != 1 {
		t.Errorf("sharing.smb has %d records, want 1", n)
	}
	if n := len(s.Records("user")); n != 0 {
		t.Errorf("untouched namespace has %d records, want 0", n)
	}
}

func TestFake_JobMethodCompletes(t *testing.T) {
	s := New(WithJobMethods("filesystem.setacl"))
	defer s.Close()
	c := dial(t, s)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// CallAndWait must resolve the job rather than returning the bare job ID.
	if _, err := c.CallAndWait(ctx, "filesystem.setacl", []any{map[string]any{"path": "/mnt/tank/a"}}); err != nil {
		t.Fatalf("CallAndWait on a job method: %v", err)
	}
}

func TestFake_UnknownMethodIsAnError(t *testing.T) {
	s := New()
	defer s.Close()
	c := dial(t, s)
	if _, err := c.Call(context.Background(), "no.such.thing", nil); err == nil {
		t.Error("expected an error for an unimplemented method")
	}
}

func TestFake_RecordsCalls(t *testing.T) {
	s := New()
	defer s.Close()
	c := dial(t, s)
	_, _ = c.Call(context.Background(), "sharing.nfs.query", []any{})

	var sawQuery bool
	for _, m := range s.Calls() {
		if m == "sharing.nfs.query" {
			sawQuery = true
		}
	}
	if !sawQuery {
		t.Errorf("sharing.nfs.query missing from recorded calls: %v", s.Calls())
	}
}

func TestFake_SeedBypassesNormalization(t *testing.T) {
	s := New()
	defer s.Close()
	s.Normalize("user", func(p map[string]any) map[string]any {
		out := copyMap(p)
		out["username"] = "MUTATED"
		return out
	})
	id := s.Seed("user", map[string]any{"username": "asis"})

	recs := s.Records("user")
	if len(recs) != 1 || recs[0]["username"] != "asis" {
		t.Errorf("Seed applied normalization; got %v", recs)
	}
	if id == 0 {
		t.Error("Seed returned no id")
	}
}

// The remaining surface: accessors, option plumbing, the query filter, and the
// error paths of each verb. These were the gap that pushed the package below
// the repo's coverage bar — worth closing on merit anyway, since the filter and
// the error paths are exactly where a fake quietly returning the wrong thing
// would send someone hunting a phantom bug in the code under test.

func TestServer_AccessorsAndOptions(t *testing.T) {
	s := New(WithVersion("TrueNAS-25.04.1"))
	defer s.Close()

	if s.URL() == "" {
		t.Error("URL is empty")
	}
	if !strings.HasPrefix(s.URL(), "https://") {
		t.Errorf("URL = %q, want an https:// base (the fake serves TLS)", s.URL())
	}
	if s.Host() == "" || strings.Contains(s.Host(), "/") {
		t.Errorf("Host = %q, want a bare host:port", s.Host())
	}
	host, port := s.HostPort()
	if host == "" || port == 0 {
		t.Errorf("HostPort = %q, %d", host, port)
	}

	// WithVersion must actually reach system.version.
	c := dial(t, s)
	raw, err := c.Call(context.Background(), "system.version", nil)
	if err != nil {
		t.Fatalf("system.version: %v", err)
	}
	var got string
	_ = json.Unmarshal(raw, &got)
	if got != "TrueNAS-25.04.1" {
		t.Errorf("system.version = %q, want the value passed to WithVersion", got)
	}
}

func TestQuery_IDFilter(t *testing.T) {
	s := New()
	defer s.Close()
	a := s.Seed("user", map[string]any{"username": "alice"})
	_ = s.Seed("user", map[string]any{"username": "bob"})

	c := dial(t, s)
	ctx := context.Background()

	// Filtered: exactly the requested record.
	raw, err := c.Call(ctx, "user.query", []any{[]any{[]any{"id", "=", a}}})
	if err != nil {
		t.Fatalf("filtered query: %v", err)
	}
	var got []map[string]any
	_ = json.Unmarshal(raw, &got)
	if len(got) != 1 || got[0]["username"] != "alice" {
		t.Errorf("filtered query returned %v, want just alice", got)
	}

	// An unrecognised filter returns everything rather than nothing. Returning
	// an empty set would look identical to "the record does not exist" and
	// send someone debugging the wrong layer.
	raw, err = c.Call(ctx, "user.query", []any{[]any{[]any{"username", "=", "alice"}}})
	if err != nil {
		t.Fatalf("unsupported-filter query: %v", err)
	}
	_ = json.Unmarshal(raw, &got)
	if len(got) != 2 {
		t.Errorf("unsupported filter returned %d records, want all 2", len(got))
	}
}

func TestQuery_IsSortedByID(t *testing.T) {
	s := New()
	defer s.Close()
	for _, n := range []string{"a", "b", "c", "d"} {
		s.Seed("user", map[string]any{"username": n})
	}
	c := dial(t, s)
	raw, err := c.Call(context.Background(), "user.query", []any{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	var got []map[string]any
	_ = json.Unmarshal(raw, &got)
	for i := 1; i < len(got); i++ {
		if toInt64(got[i-1]["id"]) >= toInt64(got[i]["id"]) {
			t.Fatalf("query results not sorted by id: %v", got)
		}
	}
}

func TestVerbs_ErrorPaths(t *testing.T) {
	s := New()
	defer s.Close()
	c := dial(t, s)
	ctx := context.Background()

	tests := []struct {
		name   string
		method string
		params any
	}{
		{"get_instance on a missing record", "user.get_instance", []any{9999}},
		{"update on a missing record", "user.update", []any{9999, map[string]any{"x": 1}}},
		{"delete on a missing record", "user.delete", []any{9999}},
		{"update with no payload", "user.update", []any{1}},
		{"create with a non-object payload", "user.create", []any{"not-an-object"}},
		{"get_instance with a non-numeric id", "user.get_instance", []any{"abc"}},
		{"delete with a non-numeric id", "user.delete", []any{"abc"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := c.Call(ctx, tt.method, tt.params); err == nil {
				t.Errorf("%s: expected an error", tt.method)
			}
		})
	}
}

// Ids are server-owned: a client must not be able to move a record by patching
// its id, which would silently corrupt the store.
func TestUpdate_CannotReassignID(t *testing.T) {
	s := New()
	defer s.Close()
	id := s.Seed("user", map[string]any{"username": "alice"})
	c := dial(t, s)

	if _, err := c.Call(context.Background(), "user.update",
		[]any{id, map[string]any{"id": 4242, "username": "alice2"}}); err != nil {
		t.Fatalf("update: %v", err)
	}

	recs := s.Records("user")
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	if toInt64(recs[0]["id"]) != id {
		t.Errorf("id was reassigned to %v", recs[0]["id"])
	}
	if recs[0]["username"] != "alice2" {
		t.Errorf("the rest of the patch did not apply: %v", recs[0])
	}
}

func TestToInt64(t *testing.T) {
	cases := map[string]struct {
		in   any
		want int64
	}{
		"int64":         {int64(7), 7},
		"int":           {9, 9},
		"float64":       {float64(11), 11}, // how JSON numbers decode into any
		"string":        {"13", 0},         // unconvertible → 0, not a panic
		"nil":           {nil, 0},
		"bool":          {true, 0},
		"float w/ frac": {float64(3.9), 3},
	}
	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			if got := toInt64(tt.in); got != tt.want {
				t.Errorf("toInt64(%v) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestSplitMethod(t *testing.T) {
	cases := []struct{ in, ns, verb string }{
		{"sharing.nfs.create", "sharing.nfs", "create"},
		{"user.query", "user", "query"},
		{"pool.dataset.get_instance", "pool.dataset", "get_instance"},
		{"bare", "", "bare"},
	}
	for _, tt := range cases {
		ns, verb := splitMethod(tt.in)
		if ns != tt.ns || verb != tt.verb {
			t.Errorf("splitMethod(%q) = (%q, %q), want (%q, %q)", tt.in, ns, verb, tt.ns, tt.verb)
		}
	}
}
