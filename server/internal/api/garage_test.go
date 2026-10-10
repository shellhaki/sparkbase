package api_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shellhaki/sparkbase/server/internal/s3"
	"github.com/shellhaki/sparkbase/server/internal/testenv"
)

func nativeS3(t *testing.T, c *testenv.Client, id string) (*s3.Client, string) {
	t.Helper()
	conn := c.Do("GET", "/v1/resources/"+id+"/connection", nil).Must(t, 200)
	return &s3.Client{
		Endpoint: conn.Str("native", "endpoint"), Region: conn.Str("native", "region"),
		AccessKey: conn.Str("native", "accessKeyId"), SecretKey: conn.Str("native", "secretAccessKey"),
	}, conn.Str("native", "bucket")
}

func TestGarageStorage(t *testing.T) {
	e := testenv.New(t, testenv.WithGarage())
	ctx := context.Background()
	admin := e.Admin()
	pa := e.Project(admin, "a")
	pb := e.Project(admin, "b")
	ida := e.Service(admin, pa, "storage", "files")
	idb := e.Service(admin, pb, "storage", "files")
	api := e.ProjectClient(pa.Secret)
	base := "/p/" + pa.Ref + "/storage/files"

	// Project API through Garage.
	api.Do("PUT", base+"/objects/docs/hello.txt", []byte("hello garage")).Must(t, 201)
	if got := api.Do("GET", base+"/objects/docs/hello.txt", nil).Must(t, 200); string(got.Raw) != "hello garage" {
		t.Fatalf("download: %q", got.Raw)
	}
	list := api.Do("GET", base+"/objects?prefix=docs/", nil).Must(t, 200)
	if list.Str("objects", "0", "key") != "docs/hello.txt" {
		t.Fatalf("list: %s", list)
	}
	if r := api.Do("GET", base+"/objects/missing", nil); r.Status != 404 {
		t.Fatalf("missing object: %s", r)
	}

	// Presigned URLs work with a plain HTTP client.
	url := api.Do("POST", base+"/sign", map[string]any{"key": "docs/hello.txt"}).Must(t, 200).Str("url")
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || string(body) != "hello garage" {
		t.Fatalf("presigned get: %d %s", res.StatusCode, body)
	}
	put := api.Do("POST", base+"/sign", map[string]any{"key": "direct.bin", "method": "PUT"}).Must(t, 200).Str("url")
	req, _ := http.NewRequest("PUT", put, bytes.NewReader([]byte("direct")))
	if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode != 200 {
		t.Fatalf("presigned put: %v %v", err, res)
	}

	// Native S3 with each service's own key.
	ca, bucketA := nativeS3(t, admin, ida)
	cb, bucketB := nativeS3(t, admin, idb)
	if err := cb.Put(ctx, bucketB, "secret.txt", strings.NewReader("b-only"), 6, "text/plain"); err != nil {
		t.Fatalf("native put: %v", err)
	}
	if objs, _, err := ca.List(ctx, bucketA, "", "", 100); err != nil || len(objs) != 2 {
		t.Fatalf("native list: %v %v", objs, err)
	}

	t.Run("a key cannot reach another bucket", func(t *testing.T) {
		if _, _, err := ca.List(ctx, bucketB, "", "", 10); err == nil {
			t.Fatal("A listed B's bucket")
		}
		if _, err := ca.Get(ctx, bucketB, "secret.txt", ""); err == nil {
			t.Fatal("A read B's object")
		}
		if err := ca.Put(ctx, bucketB, "x", strings.NewReader("x"), 1, ""); err == nil {
			t.Fatal("A wrote to B's bucket")
		}
		if err := ca.Delete(ctx, bucketB, "secret.txt"); err == nil {
			t.Fatal("A deleted from B's bucket")
		}
	})

	t.Run("pause removes the key's access", func(t *testing.T) {
		admin.Do("POST", "/v1/resources/"+idb+"/pause", nil).Must(t, 200)
		if _, err := cb.Get(ctx, bucketB, "secret.txt", ""); err == nil {
			t.Fatal("paused key still reads")
		}
		admin.Do("POST", "/v1/resources/"+idb+"/resume", nil).Must(t, 200)
		res, err := cb.Get(ctx, bucketB, "secret.txt", "")
		if err != nil {
			t.Fatalf("resumed key: %v", err)
		}
		res.Body.Close()
	})

	t.Run("rotation revokes the old key", func(t *testing.T) {
		admin.Do("PATCH", "/v1/admin/settings", map[string]any{"rotationGraceSeconds": 0}).Must(t, 200)
		admin.Do("POST", "/v1/resources/"+ida+"/credentials/rotate", nil).Must(t, 204)
		if err := e.App.RevokeDue(ctx); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ca.List(ctx, bucketA, "", "", 10); err == nil {
			t.Fatal("rotated-out key still works")
		}
		fresh, _ := nativeS3(t, admin, ida)
		if _, _, err := fresh.List(ctx, bucketA, "", "", 10); err != nil {
			t.Fatalf("new key: %v", err)
		}
		api.Do("GET", base+"/objects/docs/hello.txt", nil).Must(t, 200)
	})

	t.Run("usage and quota", func(t *testing.T) {
		if err := e.App.SampleUsage(ctx); err != nil {
			t.Fatal(err)
		}
		u := admin.Do("GET", "/v1/resources/"+ida+"/usage", nil).Must(t, 200)
		if u.Get("usage", "storage_bytes").(float64) <= 0 {
			t.Fatalf("usage: %s", u)
		}
		admin.Do("POST", "/v1/admin/plans", map[string]any{"id": "storage-tiny", "kind": "storage", "name": "Tiny", "limits": map[string]any{"storage_mb": 1, "max_upload_mb": 1}}).Must(t, 201)
		admin.Do("PATCH", "/v1/resources/"+ida, map[string]any{"plan": "storage-tiny"}).Must(t, 200)
		big := make([]byte, 700_000)
		api.Do("PUT", base+"/objects/big1", big).Must(t, 201)
		if r := api.Do("PUT", base+"/objects/big2", big); r.Code() != "quota_exceeded" {
			t.Fatalf("write past the Garage quota: %s", r)
		}
	})

	t.Run("delete empties and removes the bucket", func(t *testing.T) {
		admin.Do("DELETE", "/v1/resources/"+idb, nil).Must(t, 202)
		e.WaitGone(admin, idb)
		if _, _, err := cb.List(ctx, bucketB, "", "", 10); err == nil {
			t.Fatal("deleted bucket still reachable")
		}
	})
}

// A backup service writing to the owner's own bucket, then restoring from it.
// A Garage storage service stands in for R2 or any S3 bucket.
func TestBackupToOwnBucket(t *testing.T) {
	e := testenv.New(t, testenv.WithGarage())
	ctx := context.Background()
	admin := e.Admin()
	admin.Do("PATCH", "/v1/admin/settings", map[string]any{"allowPrivateTargets": true}).Must(t, 200)
	p := e.Project(admin, "bk")
	pgID := e.Service(admin, p, "postgres", "main")
	bucketID := e.Service(admin, p, "storage", "offsite")
	e.Service(admin, p, "backup", "nightly")
	sec := e.ProjectClient(p.Secret)
	pre := "/p/" + p.Ref

	sec.Do("POST", pre+"/postgres/main/query", map[string]any{"sql": "create table notes (body text)"}).Must(t, 200)
	sec.Do("POST", pre+"/postgres/main/query", map[string]any{"sql": "insert into notes values ('keep me')"}).Must(t, 200)

	own, bucket := nativeS3(t, admin, bucketID)
	dest := map[string]any{"type": "s3", "endpoint": own.Endpoint, "region": own.Region, "bucket": bucket, "prefix": "sparkbase", "accessKey": own.AccessKey}

	// Wrong secret: refused at save time.
	bad := map[string]any{"destination": merge(dest, map[string]any{"secretKey": "wrong"})}
	sec.Do("PATCH", pre+"/backup/nightly/settings", bad).Must(t, 422)

	set := sec.Do("PATCH", pre+"/backup/nightly/settings", map[string]any{"targets": []string{"main"}, "destination": merge(dest, map[string]any{"secretKey": own.SecretKey})}).Must(t, 200)
	if strings.Contains(string(set.Raw), own.SecretKey) || set.Get("settings", "hasSecret") != true {
		t.Fatalf("settings leak: %s", set.Raw)
	}
	// Saving again without the secret keeps it.
	sec.Do("PATCH", pre+"/backup/nightly/settings", map[string]any{"destination": dest}).Must(t, 200)

	sec.Do("POST", pre+"/backup/nightly/run", nil).Must(t, 202)
	var backupID string
	deadline := time.Now().Add(60 * time.Second)
	for backupID == "" {
		for _, x := range sec.Do("GET", pre+"/backup/nightly/runs", nil).Must(t, 200).Get("runs").([]any) {
			b := x.(map[string]any)["backup"].(map[string]any)
			if b["status"] == "done" {
				backupID = b["id"].(string)
			}
			if b["status"] == "failed" {
				t.Fatalf("backup failed: %v", b)
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("backup did not finish")
		}
		time.Sleep(200 * time.Millisecond)
	}

	objs, _, err := own.List(ctx, bucket, "sparkbase/", "", 100)
	if err != nil || len(objs) != 1 || !strings.HasSuffix(objs[0].Key, ".dump.enc") {
		t.Fatalf("objects in the bucket: %v %v", objs, err)
	}

	sec.Do("POST", pre+"/postgres/main/query", map[string]any{"sql": "delete from notes"}).Must(t, 200)
	admin.Do("POST", "/v1/resources/"+pgID+"/backups/"+backupID+"/restore", nil).Must(t, 204)
	deadline = time.Now().Add(60 * time.Second)
	for {
		r := sec.Do("POST", pre+"/postgres/main/query", map[string]any{"sql": "select body from notes"})
		if r.Status == 200 && strings.Contains(string(r.Raw), "keep me") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("restore from the bucket did not bring the row back: %s", r.Raw)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func merge(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
