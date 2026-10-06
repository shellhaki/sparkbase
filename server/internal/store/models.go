package store

import (
	"encoding/json"
	"time"
)

type User struct {
	ID          string     `db:"id" json:"id"`
	Email       string     `db:"email" json:"email"`
	Name        *string    `db:"name" json:"name"`
	Role        string     `db:"role" json:"role"`
	LastLoginAt *time.Time `db:"last_login_at" json:"lastLoginAt"`
	DisabledAt  *time.Time `db:"disabled_at" json:"disabledAt"`
	CreatedAt   time.Time  `db:"created_at" json:"createdAt"`
}

func (u *User) IsAdmin() bool { return u.Role == "admin" }

type Session struct {
	ID        string    `db:"id"`
	UserID    string    `db:"user_id"`
	ExpiresAt time.Time `db:"expires_at"`
}

type AuthToken struct {
	ID        string     `db:"id" json:"id"`
	Kind      string     `db:"kind" json:"kind"`
	UserID    *string    `db:"user_id" json:"-"`
	Email     *string    `db:"email" json:"email"`
	Role      *string    `db:"role" json:"role"`
	InvitedBy *string    `db:"invited_by" json:"invitedBy"`
	ExpiresAt time.Time  `db:"expires_at" json:"expiresAt"`
	UsedAt    *time.Time `db:"used_at" json:"usedAt"`
	CreatedAt time.Time  `db:"created_at" json:"createdAt"`
}

type Project struct {
	ID        string    `db:"id" json:"id"`
	OwnerID   string    `db:"owner_id" json:"ownerId"`
	Slug      string    `db:"slug" json:"ref"`
	Name      string    `db:"name" json:"name"`
	Status    string    `db:"status" json:"status"`
	CreatedAt time.Time `db:"created_at" json:"createdAt"`
}

type ProjectKey struct {
	ID        string     `db:"id"`
	ProjectID string     `db:"project_id"`
	Kind      string     `db:"kind"`
	Prefix    string     `db:"prefix"`
	KeyEnc    []byte     `db:"key_enc"`
	CreatedAt time.Time  `db:"created_at"`
	RevokedAt *time.Time `db:"revoked_at"`
}

// HostConfig carries per-kind settings for a data host.
type HostConfig struct {
	// postgres
	AdminUser  string `json:"adminUser,omitempty"`
	AdminDB    string `json:"adminDb,omitempty"`
	PublicHost string `json:"publicHost,omitempty"` // native host in direct mode
	PublicPort int    `json:"publicPort,omitempty"`
	PoolerAddr string `json:"poolerAddr,omitempty"`
	PoolerPort int    `json:"poolerPort,omitempty"`
	HasVector  bool   `json:"hasVector,omitempty"`
	PgBinDir   string `json:"pgBinDir,omitempty"`
	// storage
	Backend    string `json:"backend,omitempty"` // "local" or "garage"
	Root       string `json:"root,omitempty"`
	AdminURL   string `json:"adminUrl,omitempty"`
	S3Endpoint string `json:"s3Endpoint,omitempty"`
	S3Public   string `json:"s3Public,omitempty"`
	Region     string `json:"region,omitempty"`
}

type Host struct {
	ID             string     `db:"id" json:"id"`
	Kind           string     `db:"kind" json:"kind"`
	Name           string     `db:"name" json:"name"`
	InternalAddr   string     `db:"internal_addr" json:"internalAddr"`
	Config         HostConfig `db:"config" json:"config"`
	AdminSecretEnc []byte     `db:"admin_secret_enc" json:"-"`
	Weight         int        `db:"weight" json:"weight"`
	Status         string     `db:"status" json:"status"`
	BootstrappedAt *time.Time `db:"bootstrapped_at" json:"bootstrappedAt"`
	CreatedAt      time.Time  `db:"created_at" json:"createdAt"`
	ResourceCount  int        `db:"resource_count" json:"resourceCount"`
}

// Limits maps limit names to values; -1 means unlimited.
type Limits map[string]int64

func (l Limits) Get(name string) int64 { return l[name] }

// Unlimited reports whether a limit is unset-as-unlimited (-1).
func (l Limits) Unlimited(name string) bool { return l[name] < 0 }

type Plan struct {
	ID        string `db:"id" json:"id"`
	Kind      string `db:"kind" json:"kind"`
	Name      string `db:"name" json:"name"`
	Limits    Limits `db:"limits" json:"limits"`
	IsDefault bool   `db:"is_default" json:"isDefault"`
	Active    bool   `db:"active" json:"active"`
	Sort      int    `db:"sort" json:"-"`
}

type Resource struct {
	ID                  string          `db:"id" json:"id"`
	ProjectID           string          `db:"project_id" json:"projectId"`
	Kind                string          `db:"kind" json:"kind"`
	Name                string          `db:"name" json:"name"`
	PlanID              string          `db:"plan_id" json:"plan"`
	HostID              *string         `db:"host_id" json:"-"`
	Status              string          `db:"status" json:"status"`
	PausedBeforeSuspend bool            `db:"paused_before_suspend" json:"-"`
	ExternalName        *string         `db:"external_name" json:"-"`
	Endpoint            *string         `db:"endpoint" json:"endpoint"`
	ProvisionState      json.RawMessage `db:"provision_state" json:"-"`
	WriteLocked         bool            `db:"write_locked" json:"writeLocked"`
	LastError           *string         `db:"last_error" json:"lastError"`
	LastActiveAt        *time.Time      `db:"last_active_at" json:"lastActiveAt"`
	CreatedAt           time.Time       `db:"created_at" json:"createdAt"`
	// Joined fields
	ProjectSlug string `db:"project_slug" json:"projectRef"`
	OwnerID     string `db:"owner_id" json:"-"`
	Limits      Limits `db:"limits" json:"limits"`
}

func (r *Resource) Ext() string {
	if r.ExternalName == nil {
		return ""
	}
	return *r.ExternalName
}

type Credential struct {
	ID         string     `db:"id"`
	ResourceID string     `db:"resource_id"`
	Username   string     `db:"username"`
	SecretEnc  []byte     `db:"secret_enc"`
	ExtraEnc   []byte     `db:"extra_enc"`
	CreatedAt  time.Time  `db:"created_at"`
	RevokeAt   *time.Time `db:"revoke_at"`
	RevokedAt  *time.Time `db:"revoked_at"`
}

type APIKey struct {
	ID         string     `db:"id" json:"id"`
	UserID     string     `db:"user_id" json:"-"`
	ProjectID  *string    `db:"project_id" json:"projectId"`
	Name       string     `db:"name" json:"name"`
	Prefix     string     `db:"prefix" json:"prefix"`
	Scopes     []string   `db:"scopes" json:"scopes"`
	LastUsedAt *time.Time `db:"last_used_at" json:"lastUsedAt"`
	ExpiresAt  *time.Time `db:"expires_at" json:"expiresAt"`
	RevokedAt  *time.Time `db:"revoked_at" json:"revokedAt"`
	CreatedAt  time.Time  `db:"created_at" json:"createdAt"`
}

type Job struct {
	ID          int64           `db:"id" json:"id"`
	Type        string          `db:"type" json:"type"`
	Payload     json.RawMessage `db:"payload" json:"payload"`
	Status      string          `db:"status" json:"status"`
	DedupeKey   *string         `db:"dedupe_key" json:"dedupeKey"`
	RunAt       time.Time       `db:"run_at" json:"runAt"`
	Attempts    int             `db:"attempts" json:"attempts"`
	MaxAttempts int             `db:"max_attempts" json:"maxAttempts"`
	LockedAt    *time.Time      `db:"locked_at" json:"lockedAt"`
	LastError   *string         `db:"last_error" json:"lastError"`
	FinishedAt  *time.Time      `db:"finished_at" json:"finishedAt"`
	CreatedAt   time.Time       `db:"created_at" json:"createdAt"`
}

type AuditEntry struct {
	ID         int64           `db:"id" json:"id"`
	ActorID    *string         `db:"actor_id" json:"actorId"`
	ProjectID  *string         `db:"project_id" json:"projectId"`
	ResourceID *string         `db:"resource_id" json:"resourceId"`
	Action     string          `db:"action" json:"action"`
	Detail     json.RawMessage `db:"detail" json:"detail"`
	CreatedAt  time.Time       `db:"created_at" json:"createdAt"`
	ActorEmail *string         `db:"actor_email" json:"actorEmail"`
}

type Notification struct {
	ID         string     `db:"id" json:"id"`
	UserID     string     `db:"user_id" json:"-"`
	ResourceID *string    `db:"resource_id" json:"resourceId"`
	Level      string     `db:"level" json:"level"`
	Message    string     `db:"message" json:"message"`
	ReadAt     *time.Time `db:"read_at" json:"readAt"`
	CreatedAt  time.Time  `db:"created_at" json:"createdAt"`
}

type Backup struct {
	ID         string     `db:"id" json:"id"`
	ResourceID string     `db:"resource_id" json:"resourceId"`
	Status     string     `db:"status" json:"status"`
	ObjectKey  *string    `db:"object_key" json:"-"`
	SizeBytes  *int64     `db:"size_bytes" json:"sizeBytes"`
	Checksum   *string    `db:"checksum" json:"checksum"`
	ExpiresAt  *time.Time `db:"expires_at" json:"expiresAt"`
	Error      *string    `db:"error" json:"error"`
	CreatedAt  time.Time  `db:"created_at" json:"createdAt"`
	FinishedAt *time.Time `db:"finished_at" json:"finishedAt"`
}

type UsageSample struct {
	Metric    string    `db:"metric" json:"metric"`
	SampledAt time.Time `db:"sampled_at" json:"sampledAt"`
	Value     int64     `db:"value" json:"value"`
}
