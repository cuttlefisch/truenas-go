package truenas

import "context"

// FilesystemServiceAPI defines the interface for filesystem operations.
type FilesystemServiceAPI interface {
	Client() FileCaller
	WriteFile(ctx context.Context, path string, params WriteFileParams) error
	Stat(ctx context.Context, path string) (*StatResult, error)
	SetPermissions(ctx context.Context, opts SetPermOpts) error
	GetACL(ctx context.Context, opts GetACLOpts) (*ACL, error)
	SetACL(ctx context.Context, opts SetACLOpts) error
	PreflightSetPerm(ctx context.Context, opts SetPermOpts) error
}

// Compile-time checks.
var _ FilesystemServiceAPI = (*FilesystemService)(nil)
var _ FilesystemServiceAPI = (*MockFilesystemService)(nil)

// MockFilesystemService is a test double for FilesystemServiceAPI.
type MockFilesystemService struct {
	ClientFunc           func() FileCaller
	WriteFileFunc        func(ctx context.Context, path string, params WriteFileParams) error
	StatFunc             func(ctx context.Context, path string) (*StatResult, error)
	SetPermissionsFunc   func(ctx context.Context, opts SetPermOpts) error
	GetACLFunc           func(ctx context.Context, opts GetACLOpts) (*ACL, error)
	SetACLFunc           func(ctx context.Context, opts SetACLOpts) error
	PreflightSetPermFunc func(ctx context.Context, opts SetPermOpts) error
}

func (m *MockFilesystemService) Client() FileCaller {
	if m.ClientFunc != nil {
		return m.ClientFunc()
	}
	return nil
}

func (m *MockFilesystemService) WriteFile(ctx context.Context, path string, params WriteFileParams) error {
	if m.WriteFileFunc != nil {
		return m.WriteFileFunc(ctx, path, params)
	}
	return nil
}

func (m *MockFilesystemService) Stat(ctx context.Context, path string) (*StatResult, error) {
	if m.StatFunc != nil {
		return m.StatFunc(ctx, path)
	}
	return nil, nil
}

func (m *MockFilesystemService) SetPermissions(ctx context.Context, opts SetPermOpts) error {
	if m.SetPermissionsFunc != nil {
		return m.SetPermissionsFunc(ctx, opts)
	}
	return nil
}

func (m *MockFilesystemService) GetACL(ctx context.Context, opts GetACLOpts) (*ACL, error) {
	if m.GetACLFunc != nil {
		return m.GetACLFunc(ctx, opts)
	}
	return nil, nil
}

func (m *MockFilesystemService) SetACL(ctx context.Context, opts SetACLOpts) error {
	if m.SetACLFunc != nil {
		return m.SetACLFunc(ctx, opts)
	}
	return nil
}

func (m *MockFilesystemService) PreflightSetPerm(ctx context.Context, opts SetPermOpts) error {
	if m.PreflightSetPermFunc != nil {
		return m.PreflightSetPermFunc(ctx, opts)
	}
	return nil
}
