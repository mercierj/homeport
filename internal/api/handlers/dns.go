package handlers

import (
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/homeport/homeport/internal/app/dns"
	"github.com/homeport/homeport/internal/pkg/httputil"
)

const (
	MaxZoneNameLength   = 253
	MaxRecordNameLength = 253
	MaxRecordValueLen   = 4096
	MinTTL              = 1
	MaxTTL              = 604800
	DefaultTTL          = 3600
)

var (
	zoneNameRegex   = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?)*\.?$`)
	recordNameRegex = regexp.MustCompile(`^(@|\*|[a-zA-Z0-9_]([a-zA-Z0-9_\-]{0,61}[a-zA-Z0-9])?)(\.[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?)*\.?$`)
	dnsUUIDRegex    = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

func validateZoneName(name string) error {
	if name == "" {
		return fmt.Errorf("zone name is required")
	}
	if len(name) > MaxZoneNameLength {
		return fmt.Errorf("zone name must be at most %d characters", MaxZoneNameLength)
	}
	if !zoneNameRegex.MatchString(name) {
		return fmt.Errorf("invalid zone name format")
	}
	if strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("invalid zone name format")
	}
	return nil
}

func validateRecordName(name string) error {
	if name == "" {
		return fmt.Errorf("record name is required")
	}
	if len(name) > MaxRecordNameLength {
		return fmt.Errorf("record name must be at most %d characters", MaxRecordNameLength)
	}
	if !recordNameRegex.MatchString(name) {
		return fmt.Errorf("invalid record name format")
	}
	return nil
}

func validateRecordType(recordType dns.RecordType) error {
	if !dns.ValidRecordTypes[recordType] {
		return fmt.Errorf("invalid record type: %s", recordType)
	}
	return nil
}

func validateRecordValue(recordType dns.RecordType, value string) error {
	if value == "" {
		return fmt.Errorf("record value is required")
	}
	if len(value) > MaxRecordValueLen {
		return fmt.Errorf("record value must be at most %d characters", MaxRecordValueLen)
	}

	switch recordType {
	case dns.RecordTypeA:
		ip := net.ParseIP(value)
		if ip == nil || ip.To4() == nil {
			return fmt.Errorf("invalid IPv4 address: %s", value)
		}
	case dns.RecordTypeAAAA:
		ip := net.ParseIP(value)
		if ip == nil || ip.To4() != nil {
			return fmt.Errorf("invalid IPv6 address: %s", value)
		}
	case dns.RecordTypeCNAME, dns.RecordTypeNS, dns.RecordTypePTR:
		if !zoneNameRegex.MatchString(value) {
			return fmt.Errorf("invalid hostname: %s", value)
		}
	case dns.RecordTypeMX:
		if !zoneNameRegex.MatchString(value) {
			return fmt.Errorf("invalid mail server hostname: %s", value)
		}
	case dns.RecordTypeSRV:
		if !zoneNameRegex.MatchString(value) {
			return fmt.Errorf("invalid SRV target hostname: %s", value)
		}
	case dns.RecordTypeCAA:
		parts := strings.SplitN(value, " ", 3)
		if len(parts) < 3 {
			return fmt.Errorf("invalid CAA record format, expected: flag tag value")
		}
	}

	return nil
}

func validateTTL(ttl uint32) error {
	if ttl < MinTTL {
		return fmt.Errorf("TTL must be at least %d", MinTTL)
	}
	if ttl > MaxTTL {
		return fmt.Errorf("TTL must be at most %d", MaxTTL)
	}
	return nil
}

func validateZoneType(zoneType dns.ZoneType) error {
	if zoneType != dns.ZoneTypePrimary && zoneType != dns.ZoneTypeSecondary {
		return fmt.Errorf("invalid zone type: must be 'primary' or 'secondary'")
	}
	return nil
}

func validateDNSID(id, fieldName string) error {
	if id == "" {
		return fmt.Errorf("%s is required", fieldName)
	}
	if !dnsUUIDRegex.MatchString(id) {
		return fmt.Errorf("invalid %s format", fieldName)
	}
	return nil
}

// DNSHandler handles DNS management HTTP requests.
type DNSHandler struct {
	service *dns.Service
}

// NewDNSHandler creates a new DNS handler.
func NewDNSHandler() (*DNSHandler, error) {
	svc, err := dns.NewService()
	if err != nil {
		return nil, fmt.Errorf("failed to create DNS service: %w", err)
	}
	return &DNSHandler{service: svc}, nil
}

// CreateZoneRequest represents the request body for creating a zone.
type CreateZoneRequest struct {
	Name string       `json:"name"`
	Type dns.ZoneType `json:"type"`
}

// CreateRecordRequest represents the request body for creating a record.
type CreateRecordRequest struct {
	Name     string         `json:"name"`
	Type     dns.RecordType `json:"type"`
	Value    string         `json:"value"`
	TTL      uint32         `json:"ttl"`
	Priority *uint16        `json:"priority,omitempty"`
	Weight   *uint16        `json:"weight,omitempty"`
	Port     *uint16        `json:"port,omitempty"`
}

// UpdateRecordRequest represents the request body for updating a record.
type UpdateRecordRequest struct {
	Name     string         `json:"name"`
	Type     dns.RecordType `json:"type"`
	Value    string         `json:"value"`
	TTL      uint32         `json:"ttl"`
	Priority *uint16        `json:"priority,omitempty"`
	Weight   *uint16        `json:"weight,omitempty"`
	Port     *uint16        `json:"port,omitempty"`
}

// HandleListZones handles GET /dns/zones
func (h *DNSHandler) HandleListZones(w http.ResponseWriter, r *http.Request) {
	zones, err := h.service.ListZones(r.Context())
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"zones": zones,
		"count": len(zones),
	})
}

// HandleCreateZone handles POST /dns/zones
func (h *DNSHandler) HandleCreateZone(w http.ResponseWriter, r *http.Request) {
	var req CreateZoneRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	if err := validateZoneName(req.Name); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if req.Type == "" {
		req.Type = dns.ZoneTypePrimary
	}
	if err := validateZoneType(req.Type); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	zone, err := h.service.CreateZone(r.Context(), req.Name, req.Type)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			httputil.BadRequest(w, r, err.Error())
			return
		}
		httputil.InternalErrorWithMessage(w, r, "Failed to create zone", err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	render.JSON(w, r, zone)
}

// HandleGetZone handles GET /dns/zones/{zoneID}
func (h *DNSHandler) HandleGetZone(w http.ResponseWriter, r *http.Request) {
	zoneID := chi.URLParam(r, "zoneID")

	if err := validateDNSID(zoneID, "zone ID"); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	zone, err := h.service.GetZone(r.Context(), zoneID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.NotFound(w, r, "Zone not found")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, zone)
}

// HandleDeleteZone handles DELETE /dns/zones/{zoneID}
func (h *DNSHandler) HandleDeleteZone(w http.ResponseWriter, r *http.Request) {
	zoneID := chi.URLParam(r, "zoneID")

	if err := validateDNSID(zoneID, "zone ID"); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := h.service.DeleteZone(r.Context(), zoneID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.NotFound(w, r, "Zone not found")
			return
		}
		httputil.InternalErrorWithMessage(w, r, "Failed to delete zone", err)
		return
	}

	render.JSON(w, r, map[string]string{
		"status":  "deleted",
		"zone_id": zoneID,
	})
}

// HandleListRecords handles GET /dns/zones/{zoneID}/records
func (h *DNSHandler) HandleListRecords(w http.ResponseWriter, r *http.Request) {
	zoneID := chi.URLParam(r, "zoneID")

	if err := validateDNSID(zoneID, "zone ID"); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	records, err := h.service.ListRecords(r.Context(), zoneID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.NotFound(w, r, "Zone not found")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"records": records,
		"count":   len(records),
	})
}

// HandleCreateRecord handles POST /dns/zones/{zoneID}/records
func (h *DNSHandler) HandleCreateRecord(w http.ResponseWriter, r *http.Request) {
	zoneID := chi.URLParam(r, "zoneID")

	if err := validateDNSID(zoneID, "zone ID"); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	var req CreateRecordRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	if err := validateRecordName(req.Name); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := validateRecordType(req.Type); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := validateRecordValue(req.Type, req.Value); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if req.TTL == 0 {
		req.TTL = DefaultTTL
	}

	if err := validateTTL(req.TTL); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	record := dns.Record{
		Name:     req.Name,
		Type:     req.Type,
		Value:    req.Value,
		TTL:      req.TTL,
		Priority: req.Priority,
		Weight:   req.Weight,
		Port:     req.Port,
	}

	created, err := h.service.CreateRecord(r.Context(), zoneID, record)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.NotFound(w, r, "Zone not found")
			return
		}
		httputil.InternalErrorWithMessage(w, r, "Failed to create record", err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	render.JSON(w, r, created)
}

// HandleGetRecord handles GET /dns/zones/{zoneID}/records/{recordID}
func (h *DNSHandler) HandleGetRecord(w http.ResponseWriter, r *http.Request) {
	zoneID := chi.URLParam(r, "zoneID")
	recordID := chi.URLParam(r, "recordID")

	if err := validateDNSID(zoneID, "zone ID"); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := validateDNSID(recordID, "record ID"); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	record, err := h.service.GetRecord(r.Context(), zoneID, recordID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.NotFound(w, r, "Record not found")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, record)
}

// HandleUpdateRecord handles PUT /dns/zones/{zoneID}/records/{recordID}
func (h *DNSHandler) HandleUpdateRecord(w http.ResponseWriter, r *http.Request) {
	zoneID := chi.URLParam(r, "zoneID")
	recordID := chi.URLParam(r, "recordID")

	if err := validateDNSID(zoneID, "zone ID"); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := validateDNSID(recordID, "record ID"); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	var req UpdateRecordRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	if err := validateRecordName(req.Name); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := validateRecordType(req.Type); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := validateRecordValue(req.Type, req.Value); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if req.TTL == 0 {
		req.TTL = DefaultTTL
	}

	if err := validateTTL(req.TTL); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	record := dns.Record{
		Name:     req.Name,
		Type:     req.Type,
		Value:    req.Value,
		TTL:      req.TTL,
		Priority: req.Priority,
		Weight:   req.Weight,
		Port:     req.Port,
	}

	updated, err := h.service.UpdateRecord(r.Context(), zoneID, recordID, record)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.NotFound(w, r, "Record not found")
			return
		}
		httputil.InternalErrorWithMessage(w, r, "Failed to update record", err)
		return
	}

	render.JSON(w, r, updated)
}

// HandleDeleteRecord handles DELETE /dns/zones/{zoneID}/records/{recordID}
func (h *DNSHandler) HandleDeleteRecord(w http.ResponseWriter, r *http.Request) {
	zoneID := chi.URLParam(r, "zoneID")
	recordID := chi.URLParam(r, "recordID")

	if err := validateDNSID(zoneID, "zone ID"); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := validateDNSID(recordID, "record ID"); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := h.service.DeleteRecord(r.Context(), zoneID, recordID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.NotFound(w, r, "Record not found")
			return
		}
		httputil.InternalErrorWithMessage(w, r, "Failed to delete record", err)
		return
	}

	render.JSON(w, r, map[string]string{
		"status":    "deleted",
		"record_id": recordID,
	})
}

// HandleValidateZone handles POST /dns/zones/{zoneID}/validate
func (h *DNSHandler) HandleValidateZone(w http.ResponseWriter, r *http.Request) {
	zoneID := chi.URLParam(r, "zoneID")

	if err := validateDNSID(zoneID, "zone ID"); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	result, err := h.service.ValidateZone(r.Context(), zoneID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.NotFound(w, r, "Zone not found")
			return
		}
		httputil.InternalErrorWithMessage(w, r, "Failed to validate zone", err)
		return
	}

	render.JSON(w, r, result)
}
