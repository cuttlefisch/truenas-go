package truenas

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func nfsSvc(t *testing.T, fn func(ctx context.Context, method string, params any) (json.RawMessage, error)) (*SharingNFSService, *mockCaller) {
	t.Helper()
	mc := &mockCaller{callFunc: fn}
	return NewSharingNFSService(mc, Version{Major: 25, Minor: 10}), mc
}

const nfsExportJSON = `{
	"id": 1,
	"path": "/mnt/tank/export",
	"aliases": [],
	"comment": "docs",
	"networks": ["10.0.0.0/24"],
	"hosts": [],
	"ro": false,
	"maproot_user": "root",
	"maproot_group": null,
	"mapall_user": null,
	"mapall_group": null,
	"security": ["SYS"],
	"enabled": true,
	"expose_snapshots": false,
	"locked": false
}`

func TestSharingNFSService_Create(t *testing.T) {
	var sentParams any
	svc, mc := nfsSvc(t, func(_ context.Context, method string, params any) (json.RawMessage, error) {
		switch method {
		case "sharing.nfs.create":
			sentParams = params
			return json.RawMessage(`{"id": 1}`), nil
		case "sharing.nfs.get_instance":
			return json.RawMessage(nfsExportJSON), nil
		}
		return nil, errors.New("unexpected method " + method)
	})

	got, err := svc.Create(context.Background(), CreateSharingNFSOpts{
		Path:     "/mnt/tank/export",
		Comment:  "docs",
		Networks: []string{"10.0.0.0/24"},
		Security: []string{"SYS"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.ID != 1 || got.Path != "/mnt/tank/export" {
		t.Errorf("got %+v", got)
	}

	// Create re-Gets rather than trusting the create response, matching the
	// convention every other service here follows.
	if len(mc.calls) != 2 || mc.calls[1].Method != "sharing.nfs.get_instance" {
		t.Errorf("expected create then get_instance, got %v", mc.calls)
	}

	// The payload is wrapped in a positional array, and unset map* fields are
	// absent rather than sent as empty strings.
	arr, ok := sentParams.([]any)
	if !ok || len(arr) != 1 {
		t.Fatalf("create params not a 1-element array: %#v", sentParams)
	}
	p := arr[0].(map[string]any)
	for _, absent := range []string{"maproot_user", "maproot_group", "mapall_user", "mapall_group", "enabled"} {
		if _, present := p[absent]; present {
			t.Errorf("unset field %q was sent; nil must mean omitted", absent)
		}
	}
}

// Nil slices must serialise as [] rather than null. On UPDATE the difference is
// not cosmetic: an omitted field keeps its previous value while an empty array
// clears it, so sending null for a field the caller cleared would silently
// preserve it.
func TestSharingNFSService_NilSlicesBecomeEmptyArrays(t *testing.T) {
	var sent map[string]any
	svc, _ := nfsSvc(t, func(_ context.Context, method string, params any) (json.RawMessage, error) {
		if method == "sharing.nfs.update" {
			sent = params.([]any)[1].(map[string]any)
			return json.RawMessage(nfsExportJSON), nil
		}
		return nil, errors.New("unexpected " + method)
	})

	if _, err := svc.Update(context.Background(), 1, UpdateSharingNFSOpts{Path: "/mnt/tank/x"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	for _, field := range []string{"aliases", "networks", "hosts", "security"} {
		v, present := sent[field]
		if !present {
			t.Errorf("%s missing from the update payload", field)
			continue
		}
		s, ok := v.([]string)
		if !ok || s == nil {
			t.Errorf("%s = %#v, want a non-nil []string", field, v)
		}
	}
}

// The four map* fields are string|null on the wire. Decoding null into "" would
// make "not set" indistinguishable from "set to empty", which is a diff that
// never converges.
func TestSharingNFSService_NullMapFieldsStayNil(t *testing.T) {
	svc, _ := nfsSvc(t, func(context.Context, string, any) (json.RawMessage, error) {
		return json.RawMessage(nfsExportJSON), nil
	})

	got, err := svc.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.MaprootUser == nil || *got.MaprootUser != "root" {
		t.Errorf("MaprootUser = %v, want a pointer to \"root\"", got.MaprootUser)
	}
	for name, ptr := range map[string]*string{
		"MaprootGroup": got.MaprootGroup,
		"MapallUser":   got.MapallUser,
		"MapallGroup":  got.MapallGroup,
	} {
		if ptr != nil {
			t.Errorf("%s = %q, want nil for a JSON null", name, *ptr)
		}
	}
}

func TestSharingNFSService_GetNotFoundReturnsNilNil(t *testing.T) {
	svc, _ := nfsSvc(t, func(context.Context, string, any) (json.RawMessage, error) {
		return nil, errors.New("[ENOENT] sharing.nfs 42 does not exist")
	})

	got, err := svc.Get(context.Background(), 42)
	if err != nil {
		t.Fatalf("not-found must not be an error, got %v", err)
	}
	if got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

func TestSharingNFSService_GetPropagatesRealErrors(t *testing.T) {
	svc, _ := nfsSvc(t, func(context.Context, string, any) (json.RawMessage, error) {
		return nil, errors.New("connection reset")
	})
	if _, err := svc.Get(context.Background(), 1); err == nil {
		t.Error("a transport error must not be swallowed as not-found")
	}
}

func TestSharingNFSService_ListAndDelete(t *testing.T) {
	svc, mc := nfsSvc(t, func(_ context.Context, method string, _ any) (json.RawMessage, error) {
		switch method {
		case "sharing.nfs.query":
			return json.RawMessage("[" + nfsExportJSON + "]"), nil
		case "sharing.nfs.delete":
			return json.RawMessage(`true`), nil
		}
		return nil, errors.New("unexpected " + method)
	})

	list, err := svc.List(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("List: %v, %v", list, err)
	}
	if err := svc.Delete(context.Background(), 1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if mc.calls[1].Method != "sharing.nfs.delete" {
		t.Errorf("calls = %v", mc.calls)
	}
}

func TestSharingNFSService_MalformedResponses(t *testing.T) {
	cases := []struct {
		name, method string
		body         string
		call         func(*SharingNFSService) error
	}{
		{"create", "sharing.nfs.create", `not json`, func(s *SharingNFSService) error {
			_, err := s.Create(context.Background(), CreateSharingNFSOpts{Path: "/x"})
			return err
		}},
		{"get", "sharing.nfs.get_instance", `[]`, func(s *SharingNFSService) error {
			_, err := s.Get(context.Background(), 1)
			return err
		}},
		{"list", "sharing.nfs.query", `{}`, func(s *SharingNFSService) error {
			_, err := s.List(context.Background())
			return err
		}},
		{"update", "sharing.nfs.update", `7`, func(s *SharingNFSService) error {
			_, err := s.Update(context.Background(), 1, UpdateSharingNFSOpts{Path: "/x"})
			return err
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := nfsSvc(t, func(context.Context, string, any) (json.RawMessage, error) {
				return json.RawMessage(tt.body), nil
			})
			if err := tt.call(svc); err == nil {
				t.Errorf("%s: expected a parse error for body %q", tt.name, tt.body)
			}
		})
	}
}

// The whole reason NFS goes before SMB: its accepted fields are identical in
// both embedded schemas, so this service needs no version resolution at all.
// If that ever stops being true, this test is where it surfaces.
func TestSharingNFSService_NoVersionDivergenceToHandle(t *testing.T) {
	svc25_04 := NewSharingNFSService(&mockCaller{}, Version{Major: 25, Minor: 4})
	svc25_10 := NewSharingNFSService(&mockCaller{}, Version{Major: 25, Minor: 10})

	for _, svc := range []*SharingNFSService{svc25_04, svc25_10} {
		mc := svc.client.(*mockCaller)
		_, _ = svc.Get(context.Background(), 1)
		if len(mc.calls) != 1 || mc.calls[0].Method != "sharing.nfs.get_instance" {
			t.Errorf("version %v resolved to %v; the namespace must not vary by version",
				svc.version, mc.calls)
		}
	}
}
