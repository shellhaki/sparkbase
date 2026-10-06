package drivers

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shellhaki/sparkbase/server/internal/sbcrypto"
)

// LocalStorage keeps each storage service in its own directory under the
// host root. Only the API touches the files; tenants reach them through the
// project API and signed URLs. A Garage backend for native S3 is added with
// the Docker install.
type LocalStorage struct{}

func NewLocalStorage() *LocalStorage { return &LocalStorage{} }

func (s *LocalStorage) Kind() string     { return "storage" }
func (s *LocalStorage) HostKind() string { return "storage" }

// BucketName is the directory (and, with Garage, bucket) name of a service.
func BucketName(resourceID string) string { return "sb-" + shortID(resourceID) }

// ServiceDir returns the absolute directory of a service on a host root.
func ServiceDir(root, resourceID string) string {
	return filepath.Join(root, BucketName(resourceID))
}

var ErrBadKey = errors.New("invalid object key")

// CleanKey validates an object key: relative, no "..", no empty segments,
// no backslashes or control characters, at most 1024 bytes.
func CleanKey(key string) (string, error) {
	if key == "" || len(key) > 1024 || strings.HasPrefix(key, "/") || strings.Contains(key, "\\") {
		return "", ErrBadKey
	}
	for _, r := range key {
		if r < 0x20 || r == 0x7f {
			return "", ErrBadKey
		}
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.HasPrefix(seg, ".sb") {
			return "", ErrBadKey
		}
	}
	if path.Clean(key) != key {
		return "", ErrBadKey
	}
	return key, nil
}

// ObjectPath maps a key to a file path and proves it stays inside dir.
func ObjectPath(dir, key string) (string, error) {
	k, err := CleanKey(key)
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, filepath.FromSlash(k))
	rel, err := filepath.Rel(dir, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrBadKey
	}
	return p, nil
}

func (s *LocalStorage) dir(t *Target) string { return ServiceDir(t.H.Config.Root, t.R.ID) }

func (s *LocalStorage) Provision(ctx context.Context, t *Target) (*Provisioned, error) {
	if t.H.Config.Root == "" {
		return nil, errors.New("storage host has no root directory")
	}
	if err := os.MkdirAll(s.dir(t), 0o750); err != nil {
		return nil, err
	}
	// The credential secret signs URLs for this service.
	return &Provisioned{
		ExternalName: BucketName(t.R.ID),
		Credentials:  Credentials{Username: "sk_" + sbcrypto.RandomLower(12), Secret: sbcrypto.RandomString(40)},
	}, nil
}

func (s *LocalStorage) Deprovision(ctx context.Context, t *Target) error {
	if t.H.Config.Root == "" {
		return nil
	}
	return os.RemoveAll(s.dir(t))
}

// Pause and resume are enforced by the API, which refuses requests for
// services that are not active.
func (s *LocalStorage) Pause(context.Context, *Target, []Credentials) error  { return nil }
func (s *LocalStorage) Resume(context.Context, *Target, []Credentials) error { return nil }

func (s *LocalStorage) NewCredentials(ctx context.Context, t *Target) (*Credentials, error) {
	return &Credentials{Username: "sk_" + sbcrypto.RandomLower(12), Secret: sbcrypto.RandomString(40)}, nil
}

func (s *LocalStorage) RevokeCredentials(context.Context, *Target, Credentials) error { return nil }
func (s *LocalStorage) ApplyLimits(context.Context, *Target, []Credentials) error     { return nil }

func (s *LocalStorage) MeasureUsage(ctx context.Context, t *Target) (Usage, error) {
	size, count, err := DirUsage(s.dir(t))
	if err != nil {
		return nil, err
	}
	return Usage{"storage_bytes": size, "objects": count}, nil
}

// SetWriteLock is enforced by the API's quota check before each write.
func (s *LocalStorage) SetWriteLock(context.Context, *Target, []Credentials, bool) error { return nil }

// DirUsage sums file sizes under dir.
func DirUsage(dir string) (size, count int64, err error) {
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.Type().IsRegular() && !strings.HasPrefix(d.Name(), ".sb") {
			info, err := d.Info()
			if err != nil {
				return nil
			}
			size += info.Size()
			count++
		}
		return nil
	})
	return size, count, err
}

// Object describes one stored object.
type Object struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"lastModified"`
}

// ListObjects returns up to limit objects whose key starts with prefix,
// after the key "after", in key order.
func ListObjects(dir, prefix, after string, limit int) ([]Object, bool, error) {
	var out []Object
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if !d.Type().IsRegular() || strings.HasPrefix(d.Name(), ".sb") {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return nil
		}
		key := filepath.ToSlash(rel)
		if !strings.HasPrefix(key, prefix) || key <= after {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out = append(out, Object{Key: key, Size: info.Size(), LastModified: info.ModTime().UTC()})
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	more := false
	if limit > 0 && len(out) > limit {
		out, more = out[:limit], true
	}
	if out == nil {
		out = []Object{}
	}
	return out, more, nil
}

// WriteObject streams r into the object atomically and returns its size.
// It refuses to write more than max bytes.
func WriteObject(dir, key string, r io.Reader, max int64) (int64, error) {
	p, err := ObjectPath(dir, key)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp(dir, ".sbtmp-*")
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmp.Name())
	n, err := io.Copy(tmp, io.LimitReader(r, max+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return 0, err
	}
	if n > max {
		return n, ErrTooLarge
	}
	return n, os.Rename(tmp.Name(), p)
}

var ErrTooLarge = errors.New("object too large")

// DeleteObject removes an object and any directories it leaves empty.
func DeleteObject(dir, key string) (bool, error) {
	p, err := ObjectPath(dir, key)
	if err != nil {
		return false, err
	}
	if err := os.Remove(p); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	for d := filepath.Dir(p); d != dir && strings.HasPrefix(d, dir); d = filepath.Dir(d) {
		if os.Remove(d) != nil {
			break
		}
	}
	return true, nil
}
