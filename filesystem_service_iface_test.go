package truenas

import (
	"context"
	"testing"
)

func TestMockFilesystemService_ImplementsInterface(t *testing.T) {
	var _ FilesystemServiceAPI = (*FilesystemService)(nil)
	var _ FilesystemServiceAPI = (*MockFilesystemService)(nil)
}

func TestMockFilesystemService_DefaultsToNil(t *testing.T) {
	mock := &MockFilesystemService{}
	ctx := context.Background()

	result, err := mock.Stat(ctx, "/mnt/pool/test")
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result, got: %v", result)
	}

	c := mock.Client()
	if c != nil {
		t.Fatalf("expected nil client, got: %v", c)
	}
}

func TestMockFilesystemService_CallsFunc(t *testing.T) {
	called := false
	mock := &MockFilesystemService{
		StatFunc: func(ctx context.Context, path string) (*StatResult, error) {
			called = true
			return &StatResult{}, nil
		},
	}

	_, err := mock.Stat(context.Background(), "/mnt/pool/test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected StatFunc to be called")
	}
}

func TestMockFilesystemService_ACLDefaultsToNil(t *testing.T) {
	m := &MockFilesystemService{}

	acl, err := m.GetACL(context.Background(), GetACLOpts{Path: "/mnt/x"})
	if acl != nil || err != nil {
		t.Errorf("GetACL = %v, %v; want nil, nil", acl, err)
	}
	if err := m.SetACL(context.Background(), SetACLOpts{Path: "/mnt/x"}); err != nil {
		t.Errorf("SetACL = %v; want nil", err)
	}
}

func TestMockFilesystemService_ACLCallsFunc(t *testing.T) {
	var gotGet GetACLOpts
	var gotSet SetACLOpts
	m := &MockFilesystemService{
		GetACLFunc: func(_ context.Context, opts GetACLOpts) (*ACL, error) {
			gotGet = opts
			return &ACL{Path: opts.Path, ACLType: ACLTypeNFS4}, nil
		},
		SetACLFunc: func(_ context.Context, opts SetACLOpts) error {
			gotSet = opts
			return nil
		},
	}

	acl, err := m.GetACL(context.Background(), GetACLOpts{Path: "/mnt/x", ResolveIDs: true})
	if err != nil {
		t.Fatalf("GetACL: %v", err)
	}
	if acl.ACLType != ACLTypeNFS4 || !gotGet.ResolveIDs {
		t.Errorf("GetACL passthrough failed: %+v / %+v", acl, gotGet)
	}

	if err := m.SetACL(context.Background(), SetACLOpts{Path: "/mnt/y", StripACL: true}); err != nil {
		t.Fatalf("SetACL: %v", err)
	}
	if gotSet.Path != "/mnt/y" || !gotSet.StripACL {
		t.Errorf("SetACL passthrough failed: %+v", gotSet)
	}
}
