package api_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

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
