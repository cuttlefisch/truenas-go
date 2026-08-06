package truenas

import "context"

// SharingNFSServiceAPI defines the interface for NFS export operations.
type SharingNFSServiceAPI interface {
	Create(ctx context.Context, opts CreateSharingNFSOpts) (*SharingNFS, error)
	Get(ctx context.Context, id int64) (*SharingNFS, error)
	List(ctx context.Context) ([]SharingNFS, error)
	Update(ctx context.Context, id int64, opts UpdateSharingNFSOpts) (*SharingNFS, error)
	Delete(ctx context.Context, id int64) error
}

// Compile-time checks.
var _ SharingNFSServiceAPI = (*SharingNFSService)(nil)
var _ SharingNFSServiceAPI = (*MockSharingNFSService)(nil)

// MockSharingNFSService is a test double for SharingNFSServiceAPI.
type MockSharingNFSService struct {
	CreateFunc func(ctx context.Context, opts CreateSharingNFSOpts) (*SharingNFS, error)
	GetFunc    func(ctx context.Context, id int64) (*SharingNFS, error)
	ListFunc   func(ctx context.Context) ([]SharingNFS, error)
	UpdateFunc func(ctx context.Context, id int64, opts UpdateSharingNFSOpts) (*SharingNFS, error)
	DeleteFunc func(ctx context.Context, id int64) error
}

func (m *MockSharingNFSService) Create(ctx context.Context, opts CreateSharingNFSOpts) (*SharingNFS, error) {
	if m.CreateFunc != nil {
		return m.CreateFunc(ctx, opts)
	}
	return nil, nil
}

func (m *MockSharingNFSService) Get(ctx context.Context, id int64) (*SharingNFS, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, id)
	}
	return nil, nil
}

func (m *MockSharingNFSService) List(ctx context.Context) ([]SharingNFS, error) {
	if m.ListFunc != nil {
		return m.ListFunc(ctx)
	}
	return nil, nil
}

func (m *MockSharingNFSService) Update(ctx context.Context, id int64, opts UpdateSharingNFSOpts) (*SharingNFS, error) {
	if m.UpdateFunc != nil {
		return m.UpdateFunc(ctx, id, opts)
	}
	return nil, nil
}

func (m *MockSharingNFSService) Delete(ctx context.Context, id int64) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, id)
	}
	return nil
}
