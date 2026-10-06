package api_test

import (
	"context"
	"testing"

	"github.com/shellhaki/sparkbase/server/internal/sbcrypto"
	"github.com/shellhaki/sparkbase/server/internal/testenv"
)

func TestRekeyKeepsEverySecretReadable(t *testing.T) {
	e := testenv.New(t)
	ctx := context.Background()
	admin := e.Admin()
	p := e.Project(admin, "rekey")
	id := e.Service(admin, p, "auth", "auth")
	before := admin.Do("GET", "/v1/resources/"+id+"/connection", nil).Must(t, 200).Str("jwtSecret")

	newBox, _ := sbcrypto.NewBox(sbcrypto.RandomBytes(32))
	n, err := e.App.Store.Rekey(ctx, e.App.Box.Open, newBox.Seal)
	if err != nil || n < 5 {
		t.Fatalf("rekey: %d %v", n, err)
	}
	// The old key no longer opens stored secrets; the new one does.
	e.App.Box = newBox
	e.App.Tenants.Box = newBox
	e.App.Tenants.Close()
	if got := admin.Do("GET", "/v1/resources/"+id+"/connection", nil).Must(t, 200).Str("jwtSecret"); got != before {
		t.Fatal("JWT secret changed after rekey")
	}
	keys := admin.Do("GET", "/v1/projects/"+p.ID+"/keys", nil).Must(t, 200)
	if keys.Str("secretKey") != p.Secret {
		t.Fatal("project key changed after rekey")
	}
	e.ProjectClient(p.Secret).Do("GET", "/p/"+p.Ref+"/auth/admin/users", nil).Must(t, 200)
}
