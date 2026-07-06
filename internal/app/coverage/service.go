package coverage

import (
	_ "embed"
	"os"
	"sort"

	datamigration "github.com/homeport/homeport/internal/app/datamigration"
	domaincoverage "github.com/homeport/homeport/internal/domain/coverage"
	mapperregistry "github.com/homeport/homeport/internal/infrastructure/mapper"
	"gopkg.in/yaml.v3"
)

//go:embed services.yaml
var defaultCatalogData []byte

type Catalog struct {
	Services []domaincoverage.ServiceCoverage `yaml:"services" json:"services"`
}

type Drift struct {
	MapperWithoutLedger []string `json:"mapper_without_ledger" yaml:"mapper_without_ledger"`
	LedgerWithoutMapper []string `json:"ledger_without_mapper" yaml:"ledger_without_mapper"`
	Executors           []string `json:"executors" yaml:"executors"`
}

type Service struct {
	catalog Catalog
}

func LoadCatalog(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return LoadCatalogData(data)
}

func LoadDefaultCatalog() (*Catalog, error) {
	return LoadCatalogData(defaultCatalogData)
}

func LoadCatalogData(data []byte) (*Catalog, error) {
	var catalog Catalog
	if err := yaml.Unmarshal(data, &catalog); err != nil {
		return nil, err
	}
	return &catalog, nil
}

func NewService(catalog Catalog) *Service {
	return &Service{catalog: catalog}
}

func (s *Service) Catalog() Catalog {
	return s.catalog
}

func (s *Service) RegisteredMapperTypes() []string {
	types := mapperregistry.NewRegistry().SupportedTypes()
	out := make([]string, 0, len(types))
	for _, t := range types {
		out = append(out, string(t))
	}
	sort.Strings(out)
	return out
}

func (s *Service) RegisteredExecutors() []string {
	executors := datamigration.NewService().ListExecutors()
	sort.Strings(executors)
	return executors
}

func (s *Service) FindDrift() Drift {
	mapperTypes := s.RegisteredMapperTypes()
	ledgerTypes := make(map[string]bool)
	mapperSet := make(map[string]bool, len(mapperTypes))

	for _, t := range mapperTypes {
		mapperSet[t] = true
	}
	for _, service := range s.catalog.Services {
		for _, t := range service.ResourceTypes {
			if t != "" {
				ledgerTypes[t] = true
			}
		}
	}

	drift := Drift{
		MapperWithoutLedger: []string{},
		LedgerWithoutMapper: []string{},
		Executors:           s.RegisteredExecutors(),
	}
	for _, t := range mapperTypes {
		if !ledgerTypes[t] {
			drift.MapperWithoutLedger = append(drift.MapperWithoutLedger, t)
		}
	}
	for t := range ledgerTypes {
		if !mapperSet[t] {
			drift.LedgerWithoutMapper = append(drift.LedgerWithoutMapper, t)
		}
	}
	sort.Strings(drift.LedgerWithoutMapper)
	return drift
}
