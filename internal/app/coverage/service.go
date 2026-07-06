package coverage

import (
	_ "embed"
	"fmt"
	"os"
	"sort"
	"strings"

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

func SaveCatalog(path string, catalog Catalog) error {
	data, err := yaml.Marshal(catalog)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (c *Catalog) AddMissing(row domaincoverage.ServiceCoverage) error {
	if row.Provider == "" || row.Service == "" {
		return fmt.Errorf("provider and service are required")
	}
	for _, existing := range c.Services {
		if existing.Provider == row.Provider && strings.EqualFold(existing.Service, row.Service) {
			return fmt.Errorf("coverage row already exists for %s/%s", row.Provider, row.Service)
		}
	}
	row.Status = domaincoverage.StatusMissing
	if row.Blocker == "" {
		row.Blocker = "not modeled yet"
	}
	c.Services = append(c.Services, row)
	return nil
}

func (c *Catalog) Promote(provider, service string, status domaincoverage.Status) error {
	for i := range c.Services {
		row := &c.Services[i]
		if row.Provider != provider || !strings.EqualFold(row.Service, service) {
			continue
		}
		if status == domaincoverage.StatusFull && (!fullChecklist(*row) || row.Blocker != "") {
			return fmt.Errorf("cannot promote %s/%s to full until all checklist columns are true, blocker is empty, and unresolved manual steps are cleared", provider, service)
		}
		row.Status = status
		if status != domaincoverage.StatusMissing {
			row.Blocker = ""
		}
		return nil
	}
	return fmt.Errorf("coverage row not found for %s/%s", provider, service)
}

func fullChecklist(row domaincoverage.ServiceCoverage) bool {
	return row.Discover && row.Cost && row.Provision && row.Migrate && row.APICompat && row.EnvDNS && row.HA && row.Backup && row.Validate && row.Cutover && row.Rollback
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
