package conformance

import (
	"fmt"
	"os"
	"path/filepath"

	domain "github.com/homeport/homeport/internal/domain/conformance"
	"gopkg.in/yaml.v3"
)

type Service struct {
	dir string
}

func NewService(dir string) *Service { return &Service{dir: dir} }

func (s *Service) Load(provider, service string) (domain.Manifest, error) {
	path := filepath.Join(s.dir, provider+"-"+slug(service)+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.Manifest{}, err
	}
	var manifest domain.Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return domain.Manifest{}, err
	}
	if manifest.Provider != provider || manifest.Service != service {
		return domain.Manifest{}, fmt.Errorf("manifest identity mismatch: got %s/%s", manifest.Provider, manifest.Service)
	}
	return manifest, nil
}

func slug(value string) string {
	out := make([]rune, 0, len(value))
	lastDash := false
	for _, r := range value {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out = append(out, r)
			lastDash = false
			continue
		}
		if !lastDash {
			out = append(out, '-')
			lastDash = true
		}
	}
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	return string(out)
}
