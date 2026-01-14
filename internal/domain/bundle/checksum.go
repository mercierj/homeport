package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// ComputeChecksum computes the SHA-256 checksum of the given data.
func ComputeChecksum(data []byte) string {
	hash := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(hash[:])
}

// ComputeFileChecksum computes the SHA-256 checksum of a file.
func ComputeFileChecksum(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = file.Close() }()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("failed to compute hash: %w", err)
	}

	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

// VerifyChecksum verifies that the data matches the expected checksum.
func VerifyChecksum(data []byte, expected string) bool {
	actual := ComputeChecksum(data)
	return actual == expected
}

// VerifyChecksums verifies all file checksums in the bundle.
func VerifyChecksums(b *Bundle) error {
	if b.Manifest == nil || b.Manifest.Checksums == nil {
		return nil
	}

	for path, expectedChecksum := range b.Manifest.Checksums {
		file, ok := b.Files[path]
		if !ok {
			return &BundleError{
				Op:   "verify",
				Path: path,
				Err:  ErrChecksumNotFound,
			}
		}

		actualChecksum := ComputeChecksum(file.Content)
		if actualChecksum != expectedChecksum {
			return &BundleError{
				Op:   "verify",
				Path: path,
				Err:  fmt.Errorf("%w: expected %s, got %s", ErrInvalidChecksum, expectedChecksum, actualChecksum),
			}
		}
	}

	return nil
}

// ComputeAllChecksums computes checksums for all files in the bundle
// and updates the manifest.
func ComputeAllChecksums(b *Bundle) {
	if b.Manifest == nil {
		b.Manifest = NewManifest()
	}
	if b.Manifest.Checksums == nil {
		b.Manifest.Checksums = make(map[string]string)
	}

	for path, file := range b.Files {
		checksum := ComputeChecksum(file.Content)
		file.Checksum = checksum
		b.Manifest.Checksums[path] = checksum
	}
}

// ParseChecksum parses a checksum string and returns the algorithm and hash.
// Format: "sha256:abc123..."
func ParseChecksum(checksum string) (algorithm, hash string, err error) {
	if len(checksum) < 8 {
		return "", "", fmt.Errorf("invalid checksum format: %s", checksum)
	}

	// Find the colon separator
	colonIdx := -1
	for i, c := range checksum {
		if c == ':' {
			colonIdx = i
			break
		}
	}

	if colonIdx == -1 {
		return "", "", fmt.Errorf("invalid checksum format: missing algorithm prefix")
	}

	algorithm = checksum[:colonIdx]
	hash = checksum[colonIdx+1:]

	if algorithm == "" || hash == "" {
		return "", "", fmt.Errorf("invalid checksum format: %s", checksum)
	}

	return algorithm, hash, nil
}

// ChecksumResult contains the result of a checksum verification.
type ChecksumResult struct {
	Path     string
	Expected string
	Actual   string
	Valid    bool
	Error    error
}

// VerifyChecksumsDetailed verifies all file checksums and returns detailed results.
func VerifyChecksumsDetailed(b *Bundle) []ChecksumResult {
	var results []ChecksumResult

	if b.Manifest == nil || b.Manifest.Checksums == nil {
		return results
	}

	for path, expectedChecksum := range b.Manifest.Checksums {
		result := ChecksumResult{
			Path:     path,
			Expected: expectedChecksum,
		}

		file, ok := b.Files[path]
		if !ok {
			result.Error = ErrChecksumNotFound
			results = append(results, result)
			continue
		}

		result.Actual = ComputeChecksum(file.Content)
		result.Valid = result.Actual == result.Expected

		if !result.Valid {
			result.Error = ErrInvalidChecksum
		}

		results = append(results, result)
	}

	return results
}
