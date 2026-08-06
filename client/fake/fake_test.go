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
