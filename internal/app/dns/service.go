// Package dns provides DNS zone and record management.
package dns

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ZoneType represents the type of DNS zone.
type ZoneType string

const (
	ZoneTypePrimary   ZoneType = "primary"
	ZoneTypeSecondary ZoneType = "secondary"
)

// RecordType represents the type of DNS record.
type RecordType string

const (
	RecordTypeA     RecordType = "A"
	RecordTypeAAAA  RecordType = "AAAA"
	RecordTypeCNAME RecordType = "CNAME"
	RecordTypeMX    RecordType = "MX"
	RecordTypeTXT   RecordType = "TXT"
	RecordTypeNS    RecordType = "NS"
	RecordTypeSRV   RecordType = "SRV"
	RecordTypeCAA   RecordType = "CAA"
	RecordTypePTR   RecordType = "PTR"
)

// ValidRecordTypes contains all valid DNS record types.
var ValidRecordTypes = map[RecordType]bool{
	RecordTypeA:     true,
	RecordTypeAAAA:  true,
	RecordTypeCNAME: true,
	RecordTypeMX:    true,
	RecordTypeTXT:   true,
	RecordTypeNS:    true,
	RecordTypeSRV:   true,
	RecordTypeCAA:   true,
	RecordTypePTR:   true,
}

// Zone represents a DNS zone.
type Zone struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Type         ZoneType  `json:"type"`
	Serial       uint32    `json:"serial"`
	RecordsCount int       `json:"records_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Record represents a DNS record.
type Record struct {
	ID        string     `json:"id"`
	ZoneID    string     `json:"zone_id"`
	Name      string     `json:"name"`
	Type      RecordType `json:"type"`
	Value     string     `json:"value"`
	TTL       uint32     `json:"ttl"`
	Priority  *uint16    `json:"priority,omitempty"`
	Weight    *uint16    `json:"weight,omitempty"`
	Port      *uint16    `json:"port,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// ValidationError represents a DNS zone validation error.
type ValidationError struct {
	Record  string `json:"record,omitempty"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationResult represents the result of a zone validation.
type ValidationResult struct {
	Valid    bool              `json:"valid"`
	Errors   []ValidationError `json:"errors,omitempty"`
	Warnings []ValidationError `json:"warnings,omitempty"`
}

// Service provides DNS zone and record management.
type Service struct {
	mu      sync.RWMutex
	zones   map[string]*Zone
	records map[string]map[string]*Record // zoneID -> recordID -> Record
}

// NewService creates a new DNS management service.
func NewService() (*Service, error) {
	return &Service{
		zones:   make(map[string]*Zone),
		records: make(map[string]map[string]*Record),
	}, nil
}

// ListZones returns all DNS zones.
func (s *Service) ListZones(ctx context.Context) ([]Zone, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	zones := make([]Zone, 0, len(s.zones))
	for _, zone := range s.zones {
		zoneCopy := *zone
		if records, ok := s.records[zone.ID]; ok {
			zoneCopy.RecordsCount = len(records)
		}
		zones = append(zones, zoneCopy)
	}

	return zones, nil
}

// GetZone returns a specific zone by ID.
func (s *Service) GetZone(ctx context.Context, zoneID string) (*Zone, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	zone, ok := s.zones[zoneID]
	if !ok {
		return nil, fmt.Errorf("zone not found: %s", zoneID)
	}

	zoneCopy := *zone
	if records, ok := s.records[zoneID]; ok {
		zoneCopy.RecordsCount = len(records)
	}

	return &zoneCopy, nil
}

// CreateZone creates a new DNS zone.
func (s *Service) CreateZone(ctx context.Context, name string, zoneType ZoneType) (*Zone, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, z := range s.zones {
		if z.Name == name {
			return nil, fmt.Errorf("zone with name '%s' already exists", name)
		}
	}

	now := time.Now()
	zone := &Zone{
		ID:           uuid.New().String(),
		Name:         name,
		Type:         zoneType,
		Serial:       generateSerial(),
		RecordsCount: 0,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	s.zones[zone.ID] = zone
	s.records[zone.ID] = make(map[string]*Record)

	// Add default NS record
	nsRecord := &Record{
		ID:        uuid.New().String(),
		ZoneID:    zone.ID,
		Name:      "@",
		Type:      RecordTypeNS,
		Value:     "ns1." + name,
		TTL:       86400,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.records[zone.ID][nsRecord.ID] = nsRecord

	return zone, nil
}

// DeleteZone deletes a DNS zone and all its records.
func (s *Service) DeleteZone(ctx context.Context, zoneID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.zones[zoneID]; !ok {
		return fmt.Errorf("zone not found: %s", zoneID)
	}

	delete(s.zones, zoneID)
	delete(s.records, zoneID)

	return nil
}

// ListRecords returns all records in a zone.
func (s *Service) ListRecords(ctx context.Context, zoneID string) ([]Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.zones[zoneID]; !ok {
		return nil, fmt.Errorf("zone not found: %s", zoneID)
	}

	zoneRecords, ok := s.records[zoneID]
	if !ok {
		return []Record{}, nil
	}

	records := make([]Record, 0, len(zoneRecords))
	for _, record := range zoneRecords {
		records = append(records, *record)
	}

	return records, nil
}

// GetRecord returns a specific record by ID.
func (s *Service) GetRecord(ctx context.Context, zoneID, recordID string) (*Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.zones[zoneID]; !ok {
		return nil, fmt.Errorf("zone not found: %s", zoneID)
	}

	zoneRecords, ok := s.records[zoneID]
	if !ok {
		return nil, fmt.Errorf("record not found: %s", recordID)
	}

	record, ok := zoneRecords[recordID]
	if !ok {
		return nil, fmt.Errorf("record not found: %s", recordID)
	}

	return record, nil
}

// CreateRecord creates a new DNS record in a zone.
func (s *Service) CreateRecord(ctx context.Context, zoneID string, record Record) (*Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	zone, ok := s.zones[zoneID]
	if !ok {
		return nil, fmt.Errorf("zone not found: %s", zoneID)
	}

	now := time.Now()
	newRecord := &Record{
		ID:        uuid.New().String(),
		ZoneID:    zoneID,
		Name:      record.Name,
		Type:      record.Type,
		Value:     record.Value,
		TTL:       record.TTL,
		Priority:  record.Priority,
		Weight:    record.Weight,
		Port:      record.Port,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if s.records[zoneID] == nil {
		s.records[zoneID] = make(map[string]*Record)
	}
	s.records[zoneID][newRecord.ID] = newRecord

	zone.Serial = generateSerial()
	zone.UpdatedAt = now

	return newRecord, nil
}

// UpdateRecord updates an existing DNS record.
func (s *Service) UpdateRecord(ctx context.Context, zoneID, recordID string, record Record) (*Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	zone, ok := s.zones[zoneID]
	if !ok {
		return nil, fmt.Errorf("zone not found: %s", zoneID)
	}

	zoneRecords, ok := s.records[zoneID]
	if !ok {
		return nil, fmt.Errorf("record not found: %s", recordID)
	}

	existing, ok := zoneRecords[recordID]
	if !ok {
		return nil, fmt.Errorf("record not found: %s", recordID)
	}

	now := time.Now()
	existing.Name = record.Name
	existing.Type = record.Type
	existing.Value = record.Value
	existing.TTL = record.TTL
	existing.Priority = record.Priority
	existing.Weight = record.Weight
	existing.Port = record.Port
	existing.UpdatedAt = now

	zone.Serial = generateSerial()
	zone.UpdatedAt = now

	return existing, nil
}

// DeleteRecord deletes a DNS record from a zone.
func (s *Service) DeleteRecord(ctx context.Context, zoneID, recordID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	zone, ok := s.zones[zoneID]
	if !ok {
		return fmt.Errorf("zone not found: %s", zoneID)
	}

	zoneRecords, ok := s.records[zoneID]
	if !ok {
		return fmt.Errorf("record not found: %s", recordID)
	}

	if _, ok := zoneRecords[recordID]; !ok {
		return fmt.Errorf("record not found: %s", recordID)
	}

	delete(zoneRecords, recordID)

	zone.Serial = generateSerial()
	zone.UpdatedAt = time.Now()

	return nil
}

// ValidateZone validates a DNS zone configuration.
func (s *Service) ValidateZone(ctx context.Context, zoneID string) (*ValidationResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	zone, ok := s.zones[zoneID]
	if !ok {
		return nil, fmt.Errorf("zone not found: %s", zoneID)
	}

	result := &ValidationResult{
		Valid:    true,
		Errors:   []ValidationError{},
		Warnings: []ValidationError{},
	}

	zoneRecords := s.records[zoneID]

	// Check for NS records
	hasNS := false
	for _, record := range zoneRecords {
		if record.Type == RecordTypeNS && (record.Name == "@" || record.Name == zone.Name) {
			hasNS = true
			break
		}
	}
	if !hasNS {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "NS",
			Message: "Zone must have at least one NS record at the apex",
		})
		result.Valid = false
	}

	// Check for CNAME conflicts
	cnameNames := make(map[string]bool)
	otherNames := make(map[string]bool)
	for _, record := range zoneRecords {
		if record.Type == RecordTypeCNAME {
			cnameNames[record.Name] = true
		} else {
			otherNames[record.Name] = true
		}
	}
	for name := range cnameNames {
		if otherNames[name] {
			result.Errors = append(result.Errors, ValidationError{
				Record:  name,
				Field:   "CNAME",
				Message: fmt.Sprintf("CNAME record '%s' cannot coexist with other record types", name),
			})
			result.Valid = false
		}
	}

	// Check for low TTL values
	for _, record := range zoneRecords {
		if record.TTL < 300 {
			result.Warnings = append(result.Warnings, ValidationError{
				Record:  record.Name,
				Field:   "ttl",
				Message: fmt.Sprintf("TTL of %d seconds is unusually low", record.TTL),
			})
		}
	}

	return result, nil
}

func generateSerial() uint32 {
	now := time.Now()
	base := uint32(now.Year())*1000000 + uint32(now.Month())*10000 + uint32(now.Day())*100
	increment := uint32(now.Hour()*60+now.Minute()) % 100
	return base + increment
}
