package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ObjectMeta describes a stored object without its contents.
type ObjectMeta struct {
	Key         string
	Size        int64
	ContentType string
	Provider    string
}

// Store is the file storage abstraction. Swap drivers without changing CRM domain code.
type Store interface {
	Provider() string
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) (*ObjectMeta, error)
	Open(ctx context.Context, key string) (io.ReadCloser, *ObjectMeta, error)
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
}

// New creates a storage driver from configuration.
func New(driver, root string) (Store, error) {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "", "local", "filesystem", "fs":
		return NewLocal(root)
	default:
		return nil, fmt.Errorf("unsupported storage driver %q (implement Store to add providers)", driver)
	}
}

// Local is a filesystem-backed Store. Suitable for development and single-node deploys.
type Local struct {
	root string
}

func NewLocal(root string) (*Local, error) {
	if strings.TrimSpace(root) == "" {
		root = "./storage"
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	return &Local{root: abs}, nil
}

func (l *Local) Provider() string { return "local" }

func (l *Local) resolve(key string) (string, error) {
	clean := filepath.Clean("/" + strings.ReplaceAll(key, "\\", "/"))
	clean = strings.TrimPrefix(clean, string(filepath.Separator))
	full := filepath.Join(l.root, clean)
	if !strings.HasPrefix(full, l.root) {
		return "", fmt.Errorf("invalid storage key")
	}
	return full, nil
}

func (l *Local) Put(_ context.Context, key string, r io.Reader, size int64, contentType string) (*ObjectMeta, error) {
	path, err := l.resolve(key)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	n, err := io.Copy(f, r)
	if err != nil {
		return nil, err
	}
	if size > 0 && n != size {
		// tolerate unknown size; prefer written bytes
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return &ObjectMeta{Key: key, Size: n, ContentType: contentType, Provider: l.Provider()}, nil
}

func (l *Local) Open(_ context.Context, key string) (io.ReadCloser, *ObjectMeta, error) {
	path, err := l.resolve(key)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	return f, &ObjectMeta{Key: key, Size: st.Size(), ContentType: "application/octet-stream", Provider: l.Provider()}, nil
}

func (l *Local) Delete(_ context.Context, key string) error {
	path, err := l.resolve(key)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (l *Local) Exists(_ context.Context, key string) (bool, error) {
	path, err := l.resolve(key)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

// NewObjectKey builds a namespaced object key.
func NewObjectKey(prefix, filename string) string {
	safe := filepath.Base(strings.ReplaceAll(filename, "..", "_"))
	if safe == "" || safe == "." {
		safe = "file"
	}
	return fmt.Sprintf("%s/%d_%s", strings.Trim(prefix, "/"), time.Now().UTC().UnixNano(), safe)
}
