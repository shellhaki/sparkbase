package drivers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shellhaki/sparkbase/server/internal/s3"
	"github.com/shellhaki/sparkbase/server/internal/sbcrypto"
)

// GarageAdmin talks to Garage's admin API v2.
type GarageAdmin struct {
	URL   string
	Token string
	HTTP  *http.Client
}

var errGarageNotFound = errors.New("garage: not found")

func (g *GarageAdmin) call(ctx context.Context, method, op string, q url.Values, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	u := strings.TrimRight(g.URL, "/") + "/v2/" + op
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+g.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := g.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode == 404 {
		return errGarageNotFound
	}
	if res.StatusCode >= 300 {
		return fmt.Errorf("garage %s: %d %s", op, res.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

type garageBucket struct {
	ID      string `json:"id"`
	Bytes   int64  `json:"bytes"`
	Objects int64  `json:"objects"`
}

type garageKey struct {
	AccessKeyID     string  `json:"accessKeyId"`
	SecretAccessKey *string `json:"secretAccessKey"`
	Name            string  `json:"name"`
}

func (g *GarageAdmin) BucketByAlias(ctx context.Context, alias string) (*garageBucket, error) {
	var b garageBucket
	err := g.call(ctx, "GET", "GetBucketInfo", url.Values{"globalAlias": {alias}}, nil, &b)
	if err != nil {
		// Garage answers 400 for an unknown alias on some versions.
		if errors.Is(err, errGarageNotFound) || strings.Contains(err.Error(), "NoSuchBucket") || strings.Contains(err.Error(), "not found") {
			return nil, errGarageNotFound
		}
		return nil, err
	}
	return &b, nil
}

func (g *GarageAdmin) CreateBucket(ctx context.Context, alias string) (*garageBucket, error) {
	var b garageBucket
	return &b, g.call(ctx, "POST", "CreateBucket", nil, map[string]any{"globalAlias": alias}, &b)
}

func (g *GarageAdmin) SetQuota(ctx context.Context, bucketID string, maxSize int64) error {
	quotas := map[string]any{"maxObjects": nil, "maxSize": nil}
	if maxSize > 0 {
		quotas["maxSize"] = maxSize
	}
	return g.call(ctx, "POST", "UpdateBucket", url.Values{"id": {bucketID}}, map[string]any{"quotas": quotas}, nil)
}

func (g *GarageAdmin) DeleteBucket(ctx context.Context, id string) error {
	return g.call(ctx, "POST", "DeleteBucket", url.Values{"id": {id}}, nil, nil)
}

func (g *GarageAdmin) CreateKey(ctx context.Context, name string) (*garageKey, error) {
	var k garageKey
	if err := g.call(ctx, "POST", "CreateKey", nil, map[string]any{"name": name}, &k); err != nil {
		return nil, err
	}
	if k.SecretAccessKey == nil {
		return nil, errors.New("garage did not return a secret key")
	}
	return &k, nil
}

func (g *GarageAdmin) KeyWithSecret(ctx context.Context, id string) (*garageKey, error) {
	var k garageKey
	err := g.call(ctx, "GET", "GetKeyInfo", url.Values{"id": {id}, "showSecretKey": {"true"}}, nil, &k)
	return &k, err
}

func (g *GarageAdmin) DeleteKey(ctx context.Context, id string) error {
	err := g.call(ctx, "POST", "DeleteKey", url.Values{"id": {id}}, nil, nil)
	if errors.Is(err, errGarageNotFound) {
		return nil
	}
	return err
}

func (g *GarageAdmin) ListKeys(ctx context.Context) ([]struct{ ID, Name string }, error) {
	var out []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := g.call(ctx, "GET", "ListKeys", nil, nil, &out); err != nil {
		return nil, err
	}
	res := make([]struct{ ID, Name string }, len(out))
	for i, k := range out {
		res[i] = struct{ ID, Name string }{k.ID, k.Name}
	}
	return res, nil
}

// SetPerms allows or denies read/write/owner for a key on a bucket.
func (g *GarageAdmin) SetPerms(ctx context.Context, allow bool, bucketID, keyID string, read, write, owner bool) error {
	op := "DenyBucketKey"
	if allow {
		op = "AllowBucketKey"
	}
	return g.call(ctx, "POST", op, nil, map[string]any{
		"bucketId": bucketID, "accessKeyId": keyID,
		"permissions": map[string]bool{"read": read, "write": write, "owner": owner},
	}, nil)
}

// BootstrapLayout gives a fresh single-node Garage a storage role.
func (g *GarageAdmin) BootstrapLayout(ctx context.Context, capacity int64) error {
	var layout struct {
		Version int64 `json:"version"`
		Roles   []any `json:"roles"`
	}
	if err := g.call(ctx, "GET", "GetClusterLayout", nil, nil, &layout); err != nil {
		return err
	}
	if len(layout.Roles) > 0 {
		return nil
	}
	var status struct {
		Nodes []struct {
			ID string `json:"id"`
		} `json:"nodes"`
	}
	if err := g.call(ctx, "GET", "GetClusterStatus", nil, nil, &status); err != nil {
		return err
	}
	if len(status.Nodes) == 0 {
		return errors.New("garage reports no nodes")
	}
	roles := []map[string]any{}
	for _, n := range status.Nodes {
		roles = append(roles, map[string]any{"id": n.ID, "zone": "dc1", "capacity": capacity, "tags": []string{"sparkbase"}})
	}
	if err := g.call(ctx, "POST", "UpdateClusterLayout", nil, map[string]any{"roles": roles}, nil); err != nil {
		return err
	}
	return g.call(ctx, "POST", "ApplyClusterLayout", nil, map[string]any{"version": layout.Version + 1}, nil)
}

// Ping checks the admin API answers.
func (g *GarageAdmin) Ping(ctx context.Context) error {
	return g.call(ctx, "GET", "GetClusterStatus", nil, nil, nil)
}

// GarageStorage gives each service one Garage bucket and its own access keys.
type GarageStorage struct{}

func garageAdmin(t *Target) *GarageAdmin {
	return &GarageAdmin{URL: t.H.Config.AdminURL, Token: t.AdminSecret}
}

func garageKeyPrefix(resourceID string) string { return "sb-" + shortID(resourceID) + "-" }

func (s *GarageStorage) bucket(ctx context.Context, t *Target) (*garageBucket, error) {
	return garageAdmin(t).BucketByAlias(ctx, BucketName(t.R.ID))
}

func (s *GarageStorage) Provision(ctx context.Context, t *Target) (*Provisioned, error) {
	g := garageAdmin(t)
	alias := BucketName(t.R.ID)
	b, err := g.BucketByAlias(ctx, alias)
	if errors.Is(err, errGarageNotFound) {
		b, err = g.CreateBucket(ctx, alias)
	}
	if err != nil {
		return nil, err
	}
	if err := g.SetQuota(ctx, b.ID, t.Limits.Get("storage_mb")<<20); err != nil {
		return nil, err
	}
	var key *garageKey
	if id, _ := t.State["key_id"].(string); id != "" {
		key, err = g.KeyWithSecret(ctx, id)
		if errors.Is(err, errGarageNotFound) {
			key = nil
		} else if err != nil {
			return nil, err
		}
	}
	if key == nil || key.SecretAccessKey == nil {
		if key, err = g.CreateKey(ctx, garageKeyPrefix(t.R.ID)+sbcrypto.RandomLower(6)); err != nil {
			return nil, err
		}
		t.State["key_id"] = key.AccessKeyID
		if err := t.Save(t.State); err != nil {
			return nil, err
		}
	}
	if err := g.SetPerms(ctx, true, b.ID, key.AccessKeyID, true, true, false); err != nil {
		return nil, err
	}
	return &Provisioned{ExternalName: alias, Credentials: Credentials{Username: key.AccessKeyID, Secret: *key.SecretAccessKey}}, nil
}

func (s *GarageStorage) Deprovision(ctx context.Context, t *Target) error {
	g := garageAdmin(t)
	b, err := s.bucket(ctx, t)
	if err != nil && !errors.Is(err, errGarageNotFound) {
		return err
	}
	if b != nil {
		// Garage only deletes empty buckets: empty it with a temporary key.
		tmp, err := g.CreateKey(ctx, garageKeyPrefix(t.R.ID)+"cleanup")
		if err != nil {
			return err
		}
		if err := g.SetPerms(ctx, true, b.ID, tmp.AccessKeyID, true, true, true); err != nil {
			return err
		}
		c := &s3.Client{Endpoint: t.H.Config.S3Endpoint, Region: region(t), AccessKey: tmp.AccessKeyID, SecretKey: *tmp.SecretAccessKey}
		for {
			objs, more, err := c.List(ctx, b.Bucket(t), "", "", 1000)
			if err != nil {
				return err
			}
			for _, o := range objs {
				if err := c.Delete(ctx, b.Bucket(t), o.Key); err != nil {
					return err
				}
			}
			if !more || len(objs) == 0 {
				break
			}
		}
		if err := g.DeleteBucket(ctx, b.ID); err != nil {
			return err
		}
	}
	keys, err := g.ListKeys(ctx)
	if err != nil {
		return err
	}
	for _, k := range keys {
		if strings.HasPrefix(k.Name, garageKeyPrefix(t.R.ID)) {
			if err := g.DeleteKey(ctx, k.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// Bucket is the bucket's S3 name (its global alias).
func (b *garageBucket) Bucket(t *Target) string { return BucketName(t.R.ID) }

func region(t *Target) string {
	if t.H.Config.Region != "" {
		return t.H.Config.Region
	}
	return "garage"
}

func (s *GarageStorage) setAll(ctx context.Context, t *Target, creds []Credentials, read, write bool) error {
	g := garageAdmin(t)
	b, err := s.bucket(ctx, t)
	if err != nil {
		return err
	}
	for _, c := range creds {
		// Deny what must be off, allow what must be on.
		if !read || !write {
			if err := g.SetPerms(ctx, false, b.ID, c.Username, !read, !write, false); err != nil {
				return err
			}
		}
		if read || write {
			if err := g.SetPerms(ctx, true, b.ID, c.Username, read, write, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *GarageStorage) Pause(ctx context.Context, t *Target, creds []Credentials) error {
	return s.setAll(ctx, t, creds, false, false)
}

func (s *GarageStorage) Resume(ctx context.Context, t *Target, creds []Credentials) error {
	return s.setAll(ctx, t, creds, true, !t.R.WriteLocked)
}

func (s *GarageStorage) NewCredentials(ctx context.Context, t *Target) (*Credentials, error) {
	g := garageAdmin(t)
	b, err := s.bucket(ctx, t)
	if err != nil {
		return nil, err
	}
	key, err := g.CreateKey(ctx, garageKeyPrefix(t.R.ID)+sbcrypto.RandomLower(6))
	if err != nil {
		return nil, err
	}
	if err := g.SetPerms(ctx, true, b.ID, key.AccessKeyID, true, !t.R.WriteLocked, false); err != nil {
		return nil, err
	}
	return &Credentials{Username: key.AccessKeyID, Secret: *key.SecretAccessKey}, nil
}

func (s *GarageStorage) RevokeCredentials(ctx context.Context, t *Target, c Credentials) error {
	return garageAdmin(t).DeleteKey(ctx, c.Username)
}

func (s *GarageStorage) ApplyLimits(ctx context.Context, t *Target, creds []Credentials) error {
	b, err := s.bucket(ctx, t)
	if err != nil {
		return err
	}
	return garageAdmin(t).SetQuota(ctx, b.ID, t.Limits.Get("storage_mb")<<20)
}

func (s *GarageStorage) MeasureUsage(ctx context.Context, t *Target) (Usage, error) {
	b, err := s.bucket(ctx, t)
	if err != nil {
		return nil, err
	}
	return Usage{"storage_bytes": b.Bytes, "objects": b.Objects}, nil
}

func (s *GarageStorage) SetWriteLock(ctx context.Context, t *Target, creds []Credentials, locked bool) error {
	read := t.R.Status == "active" || t.R.Status == "provisioning"
	return s.setAll(ctx, t, creds, read, read && !locked)
}

// Storage picks the backend from the host's config.
type Storage struct {
	Local  *LocalStorage
	Garage *GarageStorage
}

func NewStorage() *Storage { return &Storage{Local: NewLocalStorage(), Garage: &GarageStorage{}} }

func (s *Storage) Kind() string     { return "storage" }
func (s *Storage) HostKind() string { return "storage" }

func (s *Storage) pick(t *Target) Driver {
	if t.H.Config.Backend == "garage" {
		return s.Garage
	}
	return s.Local
}

func (s *Storage) Provision(ctx context.Context, t *Target) (*Provisioned, error) {
	return s.pick(t).Provision(ctx, t)
}
func (s *Storage) Deprovision(ctx context.Context, t *Target) error {
	return s.pick(t).Deprovision(ctx, t)
}
func (s *Storage) Pause(ctx context.Context, t *Target, c []Credentials) error {
	return s.pick(t).Pause(ctx, t, c)
}
func (s *Storage) Resume(ctx context.Context, t *Target, c []Credentials) error {
	return s.pick(t).Resume(ctx, t, c)
}
func (s *Storage) NewCredentials(ctx context.Context, t *Target) (*Credentials, error) {
	return s.pick(t).NewCredentials(ctx, t)
}
func (s *Storage) RevokeCredentials(ctx context.Context, t *Target, c Credentials) error {
	return s.pick(t).RevokeCredentials(ctx, t, c)
}
func (s *Storage) ApplyLimits(ctx context.Context, t *Target, c []Credentials) error {
	return s.pick(t).ApplyLimits(ctx, t, c)
}
func (s *Storage) MeasureUsage(ctx context.Context, t *Target) (Usage, error) {
	return s.pick(t).MeasureUsage(ctx, t)
}
func (s *Storage) SetWriteLock(ctx context.Context, t *Target, c []Credentials, locked bool) error {
	return s.pick(t).SetWriteLock(ctx, t, c, locked)
}

// Kind and HostKind for the sub-drivers, to satisfy Driver.
func (s *GarageStorage) Kind() string     { return "storage" }
func (s *GarageStorage) HostKind() string { return "storage" }
