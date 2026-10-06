// Package drivers implements one Driver per service kind. The API and worker
// only talk to the Driver interface.
package drivers

import (
	"context"
	"errors"
	"fmt"

	"github.com/shellhaki/sparkbase/server/internal/store"
)

// Target is everything a driver needs to act on one service.
type Target struct {
	R           *store.Resource
	H           *store.Host
	AdminSecret string // decrypted host admin secret
	Limits      store.Limits
	// State is the provision_state checkpoint map; Save persists it.
	State map[string]any
	Save  func(map[string]any) error
}

// Step runs fn once and records name in State, so retries resume.
func (t *Target) Step(name string, fn func() error) error {
	if done, _ := t.State[name].(bool); done {
		return nil
	}
	if err := fn(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	t.State[name] = true
	if t.Save != nil {
		return t.Save(t.State)
	}
	return nil
}

// Credentials are login details for one service.
type Credentials struct {
	Username string
	Secret   string
	Extra    string // kind-specific, e.g. the auth JWT secret
}

type Provisioned struct {
	ExternalName string
	Credentials  Credentials
}

// Usage maps metric names to values, e.g. storage_bytes, connections.
type Usage map[string]int64

type Driver interface {
	Kind() string
	HostKind() string
	// Provision creates the service. It must be safe to call again after a
	// partial failure: use t.Step and idempotent statements.
	Provision(ctx context.Context, t *Target) (*Provisioned, error)
	Deprovision(ctx context.Context, t *Target) error
	Pause(ctx context.Context, t *Target, creds []Credentials) error
	Resume(ctx context.Context, t *Target, creds []Credentials) error
	// NewCredentials adds a second working credential for rotation.
	NewCredentials(ctx context.Context, t *Target) (*Credentials, error)
	// RevokeCredentials removes a credential after its grace period.
	RevokeCredentials(ctx context.Context, t *Target, c Credentials) error
	ApplyLimits(ctx context.Context, t *Target, creds []Credentials) error
	MeasureUsage(ctx context.Context, t *Target) (Usage, error)
	SetWriteLock(ctx context.Context, t *Target, creds []Credentials, locked bool) error
}

var ErrUnknownKind = errors.New("unknown service kind")

// Registry maps service kinds to drivers.
type Registry map[string]Driver

func (r Registry) Get(kind string) (Driver, error) {
	d, ok := r[kind]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownKind, kind)
	}
	return d, nil
}

// HostKindFor returns the host kind that holds services of a kind.
func HostKindFor(kind string) string {
	switch kind {
	case "postgres", "vector", "queue", "auth":
		return "postgres"
	case "cache":
		return "cache"
	case "storage":
		return "storage"
	}
	return ""
}

// Kinds is every service kind, in display order.
var Kinds = []string{"postgres", "cache", "storage", "vector", "queue", "auth"}
