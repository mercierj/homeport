package bundle

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version represents a semantic version.
type Version struct {
	Major int
	Minor int
	Patch int
}

// String returns the version as a string (e.g., "1.2.3").
func (v *Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// VersionConstraint represents a version constraint (e.g., ">=1.0.0").
type VersionConstraint struct {
	Operator string
	Version  *Version
}

var versionRegex = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)
var constraintRegex = regexp.MustCompile(`^(>=|<=|>|<|=)?(\d+\.\d+\.\d+)$`)

// ParseVersion parses a version string (e.g., "1.2.3").
func ParseVersion(s string) (*Version, error) {
	matches := versionRegex.FindStringSubmatch(strings.TrimSpace(s))
	if matches == nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidVersion, s)
	}

	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	patch, _ := strconv.Atoi(matches[3])

	return &Version{
		Major: major,
		Minor: minor,
		Patch: patch,
	}, nil
}

// ParseConstraint parses a version constraint (e.g., ">=1.0.0").
func ParseConstraint(s string) (*VersionConstraint, error) {
	matches := constraintRegex.FindStringSubmatch(strings.TrimSpace(s))
	if matches == nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidVersion, s)
	}

	operator := matches[1]
	if operator == "" {
		operator = "="
	}

	version, err := ParseVersion(matches[2])
	if err != nil {
		return nil, err
	}

	return &VersionConstraint{
		Operator: operator,
		Version:  version,
	}, nil
}

// Compare compares two versions.
// Returns -1 if v < other, 0 if v == other, 1 if v > other.
func (v *Version) Compare(other *Version) int {
	if v.Major != other.Major {
		if v.Major < other.Major {
			return -1
		}
		return 1
	}
	if v.Minor != other.Minor {
		if v.Minor < other.Minor {
			return -1
		}
		return 1
	}
	if v.Patch != other.Patch {
		if v.Patch < other.Patch {
			return -1
		}
		return 1
	}
	return 0
}

// Satisfies checks if the version satisfies the constraint.
func (v *Version) Satisfies(c *VersionConstraint) bool {
	cmp := v.Compare(c.Version)
	switch c.Operator {
	case "=":
		return cmp == 0
	case ">":
		return cmp > 0
	case ">=":
		return cmp >= 0
	case "<":
		return cmp < 0
	case "<=":
		return cmp <= 0
	default:
		return false
	}
}

// CheckCompatibility checks if a bundle version is compatible with the tool version.
func CheckCompatibility(bundleVersion, toolVersion string) error {
	bv, err := ParseVersion(bundleVersion)
	if err != nil {
		return fmt.Errorf("invalid bundle version: %w", err)
	}

	tv, err := ParseVersion(toolVersion)
	if err != nil {
		return fmt.Errorf("invalid tool version: %w", err)
	}

	// Major version must match
	if bv.Major != tv.Major {
		return fmt.Errorf("%w: bundle v%s requires tool v%d.x.x, got v%s",
			ErrIncompatibleVersion, bundleVersion, bv.Major, toolVersion)
	}

	// Bundle minor version cannot exceed tool minor version
	if bv.Minor > tv.Minor {
		return fmt.Errorf("%w: bundle v%s requires tool v%d.%d.x or higher, got v%s",
			ErrIncompatibleVersion, bundleVersion, bv.Major, bv.Minor, toolVersion)
	}

	return nil
}

// IsCompatible is a convenience function that returns true if versions are compatible.
func IsCompatible(bundleVersion, toolVersion string) bool {
	return CheckCompatibility(bundleVersion, toolVersion) == nil
}

// CheckDependencyVersions checks if all required tool dependencies are satisfied.
func CheckDependencyVersions(deps *Dependencies, installed map[string]string) []DependencyError {
	var errors []DependencyError

	if deps == nil {
		return errors
	}

	// Check Docker version
	if deps.Docker != "" {
		if dockerVersion, ok := installed["docker"]; ok {
			if err := checkVersionConstraint(dockerVersion, deps.Docker); err != nil {
				errors = append(errors, DependencyError{
					Tool:       "docker",
					Required:   deps.Docker,
					Installed:  dockerVersion,
					Err:        err,
				})
			}
		} else {
			errors = append(errors, DependencyError{
				Tool:     "docker",
				Required: deps.Docker,
				Err:      ErrToolNotInstalled,
			})
		}
	}

	// Check Docker Compose version
	if deps.DockerCompose != "" {
		if composeVersion, ok := installed["docker-compose"]; ok {
			if err := checkVersionConstraint(composeVersion, deps.DockerCompose); err != nil {
				errors = append(errors, DependencyError{
					Tool:       "docker-compose",
					Required:   deps.DockerCompose,
					Installed:  composeVersion,
					Err:        err,
				})
			}
		} else {
			errors = append(errors, DependencyError{
				Tool:     "docker-compose",
				Required: deps.DockerCompose,
				Err:      ErrToolNotInstalled,
			})
		}
	}

	// Check other tools
	if deps.Tools != nil {
		for tool, required := range deps.Tools {
			if installedVersion, ok := installed[tool]; ok {
				if err := checkVersionConstraint(installedVersion, required); err != nil {
					errors = append(errors, DependencyError{
						Tool:       tool,
						Required:   required,
						Installed:  installedVersion,
						Err:        err,
					})
				}
			} else {
				errors = append(errors, DependencyError{
					Tool:     tool,
					Required: required,
					Err:      ErrToolNotInstalled,
				})
			}
		}
	}

	return errors
}

// checkVersionConstraint checks if an installed version satisfies a constraint.
func checkVersionConstraint(installed, constraint string) error {
	c, err := ParseConstraint(constraint)
	if err != nil {
		return err
	}

	v, err := ParseVersion(installed)
	if err != nil {
		return err
	}

	if !v.Satisfies(c) {
		return fmt.Errorf("version %s does not satisfy %s", installed, constraint)
	}

	return nil
}

// DependencyError represents a dependency version mismatch.
type DependencyError struct {
	Tool      string
	Required  string
	Installed string
	Err       error
}

func (e DependencyError) Error() string {
	if e.Installed == "" {
		return fmt.Sprintf("%s: required %s, not installed", e.Tool, e.Required)
	}
	return fmt.Sprintf("%s: required %s, installed %s", e.Tool, e.Required, e.Installed)
}

// ErrToolNotInstalled indicates a required tool is not installed.
var ErrToolNotInstalled = fmt.Errorf("tool not installed")

// String returns the constraint as a string (e.g., ">=1.0.0").
func (c *VersionConstraint) String() string {
	return c.Operator + c.Version.String()
}

// MustParseVersion parses a version or panics.
// Use only for known-good version strings.
func MustParseVersion(s string) *Version {
	v, err := ParseVersion(s)
	if err != nil {
		panic(err)
	}
	return v
}

// GreaterThan returns true if v > other.
func (v *Version) GreaterThan(other *Version) bool {
	return v.Compare(other) > 0
}

// LessThan returns true if v < other.
func (v *Version) LessThan(other *Version) bool {
	return v.Compare(other) < 0
}

// Equal returns true if v == other.
func (v *Version) Equal(other *Version) bool {
	return v.Compare(other) == 0
}

// GreaterThanOrEqual returns true if v >= other.
func (v *Version) GreaterThanOrEqual(other *Version) bool {
	return v.Compare(other) >= 0
}

// LessThanOrEqual returns true if v <= other.
func (v *Version) LessThanOrEqual(other *Version) bool {
	return v.Compare(other) <= 0
}
