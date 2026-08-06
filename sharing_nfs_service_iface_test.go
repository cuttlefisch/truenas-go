package truenas

import (
	"context"
	"errors"
	"testing"
)

// errNFSMockSentinel proves a mock func was invoked rather than the nil-default
// path taken. A distinguishable value beats asserting "err != nil", which the
// default path could also satisfy if it ever changed.
var errNFSMockSentinel = errors.New("sharing nfs mock sentinel")

func TestMockSharingNFSService_ImplementsInterface(t *testing.T) {
	var _ SharingNFSServiceAPI = (*SharingNFSService)(nil)
	var _ SharingNFSServiceAPI = (*MockSharingNFSService)(nil)
}

func TestMockSharingNFSService_DefaultsToNil(t *testing.T) {
	mock := &MockSharingNFSService{}
	ctx := context.Background()

	share, err := mock.Get(ctx, 1)
	if err != nil || share != nil {
		t.Fatalf("Get: got (%v, %v), want (nil, nil)", share, err)
	}
	created, err := mock.Create(ctx, CreateSharingNFSOpts{})
	if err != nil || created != nil {
		t.Fatalf("Create: got (%v, %v), want (nil, nil)", created, err)
	}
	list, err := mock.List(ctx)
	if err != nil || list != nil {
		t.Fatalf("List: got (%v, %v), want (nil, nil)", list, err)
	}
	updated, err := mock.Update(ctx, 1, UpdateSharingNFSOpts{})
	if err != nil || updated != nil {
		t.Fatalf("Update: got (%v, %v), want (nil, nil)", updated, err)
	}
	if err := mock.Delete(ctx, 1); err != nil {
		t.Fatalf("Delete: got %v, want nil", err)
	}
}

func TestMockSharingNFSService_FuncsAreCalled(t *testing.T) {
	ctx := context.Background()
	want := &SharingNFS{ID: 7, Path: "/mnt/tank/x"}

	mock := &MockSharingNFSService{
		CreateFunc: func(context.Context, CreateSharingNFSOpts) (*SharingNFS, error) { return want, nil },
		GetFunc:    func(context.Context, int64) (*SharingNFS, error) { return want, nil },
		ListFunc:   func(context.Context) ([]SharingNFS, error) { return []SharingNFS{*want}, nil },
		UpdateFunc: func(context.Context, int64, UpdateSharingNFSOpts) (*SharingNFS, error) { return want, nil },
		DeleteFunc: func(context.Context, int64) error { return errNFSMockSentinel },
	}

	if got, _ := mock.Create(ctx, CreateSharingNFSOpts{}); got != want {
		t.Error("CreateFunc not invoked")
	}
	if got, _ := mock.Get(ctx, 1); got != want {
		t.Error("GetFunc not invoked")
	}
	if got, _ := mock.List(ctx); len(got) != 1 {
		t.Error("ListFunc not invoked")
	}
	if got, _ := mock.Update(ctx, 1, UpdateSharingNFSOpts{}); got != want {
		t.Error("UpdateFunc not invoked")
	}
	if err := mock.Delete(ctx, 1); !errors.Is(err, errNFSMockSentinel) {
		t.Error("DeleteFunc not invoked")
	}
}
