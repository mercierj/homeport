// Package objectstore provides shared utilities for object storage mapping.
package objectstore

// ObjectStoreOptions defines configuration options for MinIO object storage.
type ObjectStoreOptions struct {
	BucketName string

	// Encryption (local keys only - no cloud KMS)
	SSEEnabled    bool
	SSEType       string // "sse-s3" (auto-generated keys)
	MasterKeyPath string // Path to local master key file

	// TLS
	TLSEnabled bool
	TLSOptions *TLSOptions

	// Versioning
	Versioning bool

	// Lifecycle
	LifecycleRules []LifecycleRule

	// Cloud metadata (source info only)
	CloudProvider string
	Region        string
}

// TLSOptions defines TLS configuration for MinIO.
type TLSOptions struct {
	CertPath   string // Path to TLS certificate file
	KeyPath    string // Path to TLS private key file
	CAPath     string // Path to CA certificate file (optional)
	MinVersion string // Minimum TLS version (e.g., "1.2", "1.3")
}

// LifecycleRule defines an object lifecycle rule.
type LifecycleRule struct {
	ID             string
	Prefix         string
	ExpirationDays int
	Enabled        bool
	Transition     *TransitionRule
}

// TransitionRule defines a storage class transition rule.
type TransitionRule struct {
	Days         int
	StorageClass string // "GLACIER", "DEEP_ARCHIVE", etc. maps to MinIO tier
}

// NewObjectStoreOptions creates a new ObjectStoreOptions with sensible defaults.
func NewObjectStoreOptions(bucketName string) *ObjectStoreOptions {
	return &ObjectStoreOptions{
		BucketName:     bucketName,
		SSEEnabled:     false,
		SSEType:        "sse-s3",
		MasterKeyPath:  "/var/lib/homeport/keys/minio-master.key",
		TLSEnabled:     false,
		Versioning:     false,
		LifecycleRules: []LifecycleRule{},
	}
}

// WithSSE enables server-side encryption with the specified master key path.
// If keyPath is empty, the default path is used.
func (o *ObjectStoreOptions) WithSSE(keyPath string) *ObjectStoreOptions {
	o.SSEEnabled = true
	o.SSEType = "sse-s3"
	if keyPath != "" {
		o.MasterKeyPath = keyPath
	}
	return o
}

// WithTLS enables TLS with the specified certificate and key paths.
func (o *ObjectStoreOptions) WithTLS(certPath, keyPath string) *ObjectStoreOptions {
	o.TLSEnabled = true
	o.TLSOptions = &TLSOptions{
		CertPath:   certPath,
		KeyPath:    keyPath,
		MinVersion: "1.2",
	}
	return o
}

// WithTLSOptions enables TLS with full TLS options.
func (o *ObjectStoreOptions) WithTLSOptions(tlsOpts *TLSOptions) *ObjectStoreOptions {
	o.TLSEnabled = true
	o.TLSOptions = tlsOpts
	return o
}

// WithVersioning enables bucket versioning.
func (o *ObjectStoreOptions) WithVersioning() *ObjectStoreOptions {
	o.Versioning = true
	return o
}

// AddLifecycleRule adds a lifecycle rule to the bucket configuration.
func (o *ObjectStoreOptions) AddLifecycleRule(rule LifecycleRule) *ObjectStoreOptions {
	o.LifecycleRules = append(o.LifecycleRules, rule)
	return o
}

// WithCloudMetadata sets the source cloud provider metadata.
func (o *ObjectStoreOptions) WithCloudMetadata(provider, region string) *ObjectStoreOptions {
	o.CloudProvider = provider
	o.Region = region
	return o
}

// NewLifecycleRule creates a new lifecycle rule with basic settings.
func NewLifecycleRule(id, prefix string, expirationDays int) LifecycleRule {
	return LifecycleRule{
		ID:             id,
		Prefix:         prefix,
		ExpirationDays: expirationDays,
		Enabled:        true,
	}
}

// WithTransition adds a transition rule to the lifecycle rule.
func (r LifecycleRule) WithTransition(days int, storageClass string) LifecycleRule {
	r.Transition = &TransitionRule{
		Days:         days,
		StorageClass: storageClass,
	}
	return r
}
