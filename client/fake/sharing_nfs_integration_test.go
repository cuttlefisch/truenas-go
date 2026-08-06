package fake_test

// This file lives in fake_test (an external test package) rather than in the
// root package, because it needs both truenas and client/fake and the root
// package cannot import a package that imports it.
//
// It is the first end-to-end exercise of the whole stack with no appliance:
// a real SharingNFSService, over a real WebSocketClient, over a real
// websocket, against the stateful fake. Everything above the network is the
// code that will run in production.

import (
	"context"
	"strings"
	"testing"
	"time"

	truenas "github.com/deevus/truenas-go"
	"github.com/deevus/truenas-go/client"
	"github.com/deevus/truenas-go/client/fake"
)

func newNFSService(t *testing.T, s *fake.Server) *truenas.SharingNFSService {
	t.Helper()
	host, port := s.HostPort()
	c, err := client.NewWebSocketClient(client.WebSocketConfig{
		Host:               host,
		Port:               port,
		Username:           "root",
		APIKey:             "test-key",
		InsecureSkipVerify: true, // the fake serves a self-signed cert
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
	return truenas.NewSharingNFSService(c, truenas.Version{Major: 25, Minor: 10})
}

func TestSharingNFS_FullLifecycleAgainstFake(t *testing.T) {
	s := fake.New()
	defer s.Close()
	svc := newNFSService(t, s)
	ctx := context.Background()

	created, err := svc.Create(ctx, truenas.CreateSharingNFSOpts{
		Path:     "/mnt/tank/export",
		Comment:  "docs",
		Networks: []string{"10.0.0.0/24"},
		Security: []string{"SYS"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("Create returned no id")
	}

	// Read back through a separate call, which is where a create/read
	// asymmetry would show.
	got, err := svc.Get(ctx, created.ID)
	if err != nil || got == nil {
		t.Fatalf("Get: %v, %v", got, err)
	}
	if got.Path != "/mnt/tank/export" || got.Comment != "docs" {
		t.Errorf("read back %+v", got)
	}

	list, err := svc.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("List: %v, %v", list, err)
	}

	updated, err := svc.Update(ctx, created.ID, truenas.UpdateSharingNFSOpts{
		Path:     "/mnt/tank/export",
		Comment:  "updated",
		Networks: []string{"10.0.0.0/24", "10.0.1.0/24"},
		Security: []string{"SYS"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Comment != "updated" || len(updated.Networks) != 2 {
		t.Errorf("update did not apply: %+v", updated)
	}

	if err := svc.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// Not-found is (nil, nil) by this library's convention, not an error.
	gone, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after delete returned an error: %v", err)
	}
	if gone != nil {
		t.Errorf("Get after delete returned %+v, want nil", gone)
	}
}

// Round-tripping the same options must be stable. This is the client-side
// shadow of "the second terraform plan is empty" — if Create and Get disagree
// about a field's representation, it shows here, before any provider exists.
func TestSharingNFS_CreateThenReadIsStable(t *testing.T) {
	s := fake.New()
	defer s.Close()
	svc := newNFSService(t, s)
	ctx := context.Background()

	root := "root"
	opts := truenas.CreateSharingNFSOpts{
		Path:            "/mnt/tank/stable",
		Comment:         "c",
		Aliases:         []string{},
		Networks:        []string{"192.168.1.0/24"},
		Hosts:           []string{"host-a"},
		Security:        []string{"SYS"},
		ReadOnly:        true,
		MaprootUser:     &root,
		ExposeSnapshots: true,
	}

	created, err := svc.Create(ctx, opts)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := svc.Get(ctx, created.ID)
	if err != nil || got == nil {
		t.Fatalf("Get: %v, %v", got, err)
	}

	if got.Path != opts.Path || got.Comment != opts.Comment || got.ReadOnly != opts.ReadOnly ||
		got.ExposeSnapshots != opts.ExposeSnapshots {
		t.Errorf("scalar drift between create and read: %+v", got)
	}
	if got.MaprootUser == nil || *got.MaprootUser != root {
		t.Errorf("MaprootUser = %v, want %q", got.MaprootUser, root)
	}
	// Unset pointers must still be nil after a round trip.
	if got.MapallUser != nil || got.MapallGroup != nil || got.MaprootGroup != nil {
		t.Errorf("an unset map* field came back non-nil: %+v", got)
	}
	if len(got.Networks) != 1 || got.Networks[0] != "192.168.1.0/24" {
		t.Errorf("Networks = %v", got.Networks)
	}
}

// The payoff for the whole Tier-1 design: a server that normalizes a value the
// client does not expect produces observable drift, caught in CI with no
// appliance. TrueNAS canonicalizes paths, so a trailing slash is a realistic
// instance of this.
func TestSharingNFS_ServerNormalizationIsDetectable(t *testing.T) {
	s := fake.New()
	defer s.Close()
	s.Normalize("sharing.nfs", func(p map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range p {
			out[k] = v
		}
		if path, ok := out["path"].(string); ok {
			out["path"] = strings.TrimSuffix(path, "/")
		}
		return out
	})

	svc := newNFSService(t, s)
	created, err := svc.Create(context.Background(), truenas.CreateSharingNFSOpts{
		Path: "/mnt/tank/trailing/",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if created.Path == "/mnt/tank/trailing/" {
		t.Fatal("the hook did not fire; this test would not detect drift")
	}
	if created.Path != "/mnt/tank/trailing" {
		t.Errorf("Path = %q, want the canonicalized form", created.Path)
	}
	// A provider storing the configured value rather than this one is exactly
	// the bug that yields a permanently non-empty plan.
}

func TestSharingNFS_DeleteOfMissingExportErrors(t *testing.T) {
	s := fake.New()
	defer s.Close()
	svc := newNFSService(t, s)

	if err := svc.Delete(context.Background(), 9999); err == nil {
		t.Error("deleting a nonexistent export should error")
	}
}
