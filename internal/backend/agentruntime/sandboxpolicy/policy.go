package sandboxpolicy

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
)

// Document is the non-secret, versioned policy snapshot. Credentials are
// resolved by the control plane from CredentialRef, never stored here.
type Document struct {
	Enabled        bool                `json:"enabled"`
	DefaultProfile string              `json:"default_profile"`
	Profiles       map[string]Profile  `json:"profiles"`
	Routes         []Route             `json:"routes"`
	NodePools      map[string]NodePool `json:"node_pools"`
}

type Profile struct {
	Driver         string  `json:"driver"`
	NodePool       string  `json:"node_pool"`
	ImageDigest    string  `json:"image_digest,omitempty"`
	Network        string  `json:"network"`
	CPUs           float64 `json:"cpus"`
	MemoryMiB      int     `json:"memory_mib"`
	PIDs           int     `json:"pids"`
	TimeoutSeconds int     `json:"timeout_seconds"`
	MaxOutputBytes int     `json:"max_output_bytes"`
	// File-capable profiles require explicit memory and artifact quotas.
	ScratchMiB       int `json:"scratch_mib,omitempty"`
	MaxArtifactBytes int `json:"max_artifact_bytes,omitempty"`
}

type Route struct {
	Origin  string `json:"origin"`
	Profile string `json:"profile"`
}

type NodePool struct {
	Endpoint       string `json:"endpoint"`
	CredentialRef  string `json:"credential_ref"`
	Environment    string `json:"environment"`
	MaxConcurrency int    `json:"max_concurrency"`
}

func DefaultDocument() Document {
	return Document{Enabled: false, Profiles: map[string]Profile{}, Routes: []Route{}, NodePools: map[string]NodePool{}}
}

// Validate rejects native and any unbounded/privileged execution. Provenance
// and native policy verification will be added separately, not by a DB flag.
var pinnedImage = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9./:_-]*@sha256:[a-f0-9]{64}$`)

func (d Document) Validate() error {
	if len(d.Profiles) > 32 || len(d.Routes) > 64 || len(d.NodePools) > 16 {
		return errors.New("sandbox policy exceeds configured shape limits")
	}
	if !d.Enabled {
		if d.DefaultProfile != "" || len(d.Profiles) > 0 || len(d.Routes) > 0 || len(d.NodePools) > 0 {
			return errors.New("disabled sandbox policy must be empty")
		}
		return nil
	}
	if d.DefaultProfile == "" || len(d.Profiles) == 0 || len(d.NodePools) == 0 {
		return errors.New("enabled sandbox policy needs a default profile and node pool")
	}
	for name, p := range d.Profiles {
		if !validID(name) {
			return fmt.Errorf("invalid sandbox profile name %q", name)
		}
		if err := ValidateOCIProfile(p); err != nil {
			return fmt.Errorf("profile %q: %w", name, err)
		}
		if _, ok := d.NodePools[p.NodePool]; !ok {
			return fmt.Errorf("profile %q references missing pool", name)
		}
	}
	if _, ok := d.Profiles[d.DefaultProfile]; !ok {
		return errors.New("default sandbox profile is missing")
	}
	seen := map[string]bool{}
	for _, r := range d.Routes {
		if r.Origin != "repo_release" && r.Origin != "external" && r.Origin != "generated" && r.Origin != "unknown" {
			return fmt.Errorf("unsupported sandbox origin %q", r.Origin)
		}
		if seen[r.Origin] {
			return fmt.Errorf("duplicate sandbox route %q", r.Origin)
		}
		seen[r.Origin] = true
		if _, ok := d.Profiles[r.Profile]; !ok {
			return fmt.Errorf("sandbox route %q references missing profile", r.Origin)
		}
	}
	for name, p := range d.NodePools {
		if !validID(name) || !validID(p.CredentialRef) || (p.Environment != "development" && p.Environment != "production") || p.MaxConcurrency < 1 || p.MaxConcurrency > 64 {
			return fmt.Errorf("invalid sandbox node pool %q", name)
		}
		u, err := url.Parse(p.Endpoint)
		if err != nil || u == nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return fmt.Errorf("invalid sandbox node endpoint for pool %q", name)
		}
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "https" && !(u.Scheme == "http" && ip != nil && ip.IsLoopback() && p.Environment == "development") {
			return fmt.Errorf("pool %q needs HTTPS or development loopback", name)
		}
	}
	return nil
}

// ValidateOCIProfile is rechecked by the Node's driver, independently of the
// Broker's DB policy. An unrecognized driver, network mode or mutable image is
// never coerced to a permissive fallback.
func ValidateOCIProfile(p Profile) error {
	if p.Driver != "oci" || p.Network != "deny" {
		return errors.New("only network-denied OCI profiles are supported")
	}
	if !pinnedImage.MatchString(p.ImageDigest) || p.CPUs <= 0 || p.CPUs > 8 || p.MemoryMiB < 64 || p.MemoryMiB > 16384 || p.PIDs < 1 || p.PIDs > 1024 || p.TimeoutSeconds < 1 || p.TimeoutSeconds > 3600 || p.MaxOutputBytes < 1 || p.MaxOutputBytes > 8<<20 {
		return errors.New("invalid limits or unpinned image")
	}
	// Older isolated stdin-only profiles may omit file limits. File-capable
	// execution rejects missing values instead of silently selecting defaults.
	if p.ScratchMiB != 0 && (p.ScratchMiB < 8 || p.ScratchMiB > 512 || p.ScratchMiB > p.MemoryMiB/2) {
		return errors.New("invalid OCI scratch memory quota")
	}
	if p.MaxArtifactBytes < 0 || p.MaxArtifactBytes > 8<<20 {
		return errors.New("invalid OCI artifact budget")
	}
	return nil
}

// ValidateFileOCIProfile is the stricter admission gate for staged tasks.
// Legacy stateless stdin-only profiles remain valid but cannot receive files.
func ValidateFileOCIProfile(p Profile) error {
	if err := ValidateOCIProfile(p); err != nil {
		return err
	}
	if p.ScratchMiB == 0 || p.MaxArtifactBytes == 0 {
		return errors.New("file OCI profile needs explicit scratch and artifact budgets")
	}
	return nil
}

func validID(s string) bool {
	if len(s) < 1 || len(s) > 80 {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func (d Document) ProfileFor(origin string) (string, bool) {
	if !d.Enabled {
		return "", false
	}
	for _, r := range d.Routes {
		if r.Origin == origin {
			return r.Profile, true
		}
	}
	return d.DefaultProfile, true
}
