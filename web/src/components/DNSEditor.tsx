import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';
import {
  RefreshCw,
  Plus,
  Trash2,
  Edit2,
  Loader2,
  Globe,
  ArrowLeft,
  CheckCircle,
  AlertCircle,
  X,
  Server,
  Clock,
} from 'lucide-react';
import {
  listZones,
  getZone,
  createZone,
  deleteZone,
  listRecords,
  createRecord,
  updateRecord,
  deleteRecord,
  validateZone,
  getRecordTypeColor,
  formatTTL,
  validateRecordValue,
  TTL_PRESETS,
  RECORD_TYPE_HINTS,
  RECORD_TYPES,
  type Zone,
  type Record as DNSRecord,
  type RecordType,
  type ZoneType,
  type CreateRecordRequest,
  type UpdateRecordRequest,
  type ValidationResult,
} from '@/lib/dns-api';

interface DNSEditorProps {
  onError?: (error: Error) => void;
  onSuccess?: (message: string) => void;
}

type ViewMode = 'zones' | 'zone-detail';

interface RecordFormData {
  name: string;
  type: RecordType;
  value: string;
  ttl: number;
  priority?: number;
  weight?: number;
  port?: number;
}

const defaultRecordForm: RecordFormData = {
  name: '',
  type: 'A',
  value: '',
  ttl: 3600,
  priority: undefined,
  weight: undefined,
  port: undefined,
};

export function DNSEditor({ onError, onSuccess }: DNSEditorProps) {
  const queryClient = useQueryClient();
  const [viewMode, setViewMode] = useState<ViewMode>('zones');
  const [selectedZoneId, setSelectedZoneId] = useState<string | null>(null);

  // Zone form state
  const [showNewZoneForm, setShowNewZoneForm] = useState(false);
  const [newZoneName, setNewZoneName] = useState('');
  const [newZoneType, setNewZoneType] = useState<ZoneType>('primary');

  // Record form state
  const [showRecordForm, setShowRecordForm] = useState(false);
  const [editingRecordId, setEditingRecordId] = useState<string | null>(null);
  const [recordForm, setRecordForm] = useState<RecordFormData>(defaultRecordForm);
  const [recordFormErrors, setRecordFormErrors] = useState<string[]>([]);

  // Validation state
  const [validationResult, setValidationResult] = useState<ValidationResult | null>(null);
  const [showValidation, setShowValidation] = useState(false);

  // Zones query
  const {
    data: zonesData,
    isLoading: zonesLoading,
    error: zonesError,
    refetch: refetchZones,
  } = useQuery({
    queryKey: ['dns-zones'],
    queryFn: listZones,
    refetchInterval: 30000,
  });

  // Selected zone query
  const {
    data: selectedZone,
    isLoading: zoneLoading,
  } = useQuery({
    queryKey: ['dns-zone', selectedZoneId],
    queryFn: () => selectedZoneId ? getZone(selectedZoneId) : Promise.resolve(null),
    enabled: !!selectedZoneId,
  });

  // Records query
  const {
    data: recordsData,
    isLoading: recordsLoading,
    refetch: refetchRecords,
  } = useQuery({
    queryKey: ['dns-records', selectedZoneId],
    queryFn: () => selectedZoneId ? listRecords(selectedZoneId) : Promise.resolve({ records: [], count: 0 }),
    enabled: !!selectedZoneId,
    refetchInterval: 30000,
  });

  // Mutations
  const createZoneMutation = useMutation({
    mutationFn: ({ name, type }: { name: string; type: ZoneType }) => createZone(name, type),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['dns-zones'] });
      setShowNewZoneForm(false);
      setNewZoneName('');
      setNewZoneType('primary');
      onSuccess?.('Zone created successfully');
    },
    onError: (err: Error) => {
      onError?.(err);
    },
  });

  const deleteZoneMutation = useMutation({
    mutationFn: (zoneId: string) => deleteZone(zoneId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['dns-zones'] });
      if (selectedZoneId) {
        setSelectedZoneId(null);
        setViewMode('zones');
      }
      onSuccess?.('Zone deleted successfully');
    },
    onError: (err: Error) => {
      onError?.(err);
    },
  });

  const createRecordMutation = useMutation({
    mutationFn: ({ zoneId, record }: { zoneId: string; record: CreateRecordRequest }) =>
      createRecord(zoneId, record),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['dns-records', selectedZoneId] });
      queryClient.invalidateQueries({ queryKey: ['dns-zones'] });
      resetRecordForm();
      onSuccess?.('Record created successfully');
    },
    onError: (err: Error) => {
      onError?.(err);
    },
  });

  const updateRecordMutation = useMutation({
    mutationFn: ({
      zoneId,
      recordId,
      record,
    }: {
      zoneId: string;
      recordId: string;
      record: UpdateRecordRequest;
    }) => updateRecord(zoneId, recordId, record),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['dns-records', selectedZoneId] });
      resetRecordForm();
      onSuccess?.('Record updated successfully');
    },
    onError: (err: Error) => {
      onError?.(err);
    },
  });

  const deleteRecordMutation = useMutation({
    mutationFn: ({ zoneId, recordId }: { zoneId: string; recordId: string }) =>
      deleteRecord(zoneId, recordId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['dns-records', selectedZoneId] });
      queryClient.invalidateQueries({ queryKey: ['dns-zones'] });
      onSuccess?.('Record deleted successfully');
    },
    onError: (err: Error) => {
      onError?.(err);
    },
  });

  const validateZoneMutation = useMutation({
    mutationFn: (zoneId: string) => validateZone(zoneId),
    onSuccess: (result) => {
      setValidationResult(result);
      setShowValidation(true);
      if (result.valid) {
        onSuccess?.('Zone validation passed');
      }
    },
    onError: (err: Error) => {
      onError?.(err);
    },
  });

  const resetRecordForm = () => {
    setShowRecordForm(false);
    setEditingRecordId(null);
    setRecordForm(defaultRecordForm);
    setRecordFormErrors([]);
  };

  const handleSelectZone = (zone: Zone) => {
    setSelectedZoneId(zone.id);
    setViewMode('zone-detail');
    setShowValidation(false);
    setValidationResult(null);
  };

  const handleBackToZones = () => {
    setViewMode('zones');
    setSelectedZoneId(null);
    resetRecordForm();
    setShowValidation(false);
    setValidationResult(null);
  };

  const handleEditRecord = (record: DNSRecord) => {
    setEditingRecordId(record.id);
    setRecordForm({
      name: record.name,
      type: record.type,
      value: record.value,
      ttl: record.ttl,
      priority: record.priority,
      weight: record.weight,
      port: record.port,
    });
    setShowRecordForm(true);
    setRecordFormErrors([]);
  };

  const handleSubmitZone = (e: React.FormEvent) => {
    e.preventDefault();
    if (!newZoneName.trim()) return;
    createZoneMutation.mutate({ name: newZoneName.trim(), type: newZoneType });
  };

  const handleSubmitRecord = (e: React.FormEvent) => {
    e.preventDefault();

    // Validate
    const errors: string[] = [];
    if (!recordForm.name.trim()) {
      errors.push('Name is required');
    }

    const valueValidation = validateRecordValue(recordForm.type, recordForm.value);
    if (!valueValidation.valid && valueValidation.message) {
      errors.push(valueValidation.message);
    }

    if (recordForm.ttl < 1) {
      errors.push('TTL must be at least 1 second');
    }

    if (recordForm.type === 'MX' && (recordForm.priority === undefined || recordForm.priority < 0)) {
      errors.push('Priority is required for MX records');
    }

    if (recordForm.type === 'SRV') {
      if (recordForm.weight === undefined || recordForm.weight < 0) {
        errors.push('Weight is required for SRV records');
      }
      if (recordForm.port === undefined || recordForm.port < 1 || recordForm.port > 65535) {
        errors.push('Port (1-65535) is required for SRV records');
      }
    }

    if (errors.length > 0) {
      setRecordFormErrors(errors);
      return;
    }

    if (!selectedZoneId) return;

    const recordData: CreateRecordRequest = {
      name: recordForm.name.trim(),
      type: recordForm.type,
      value: recordForm.value.trim(),
      ttl: recordForm.ttl,
      priority: recordForm.type === 'MX' || recordForm.type === 'SRV' ? recordForm.priority : undefined,
      weight: recordForm.type === 'SRV' ? recordForm.weight : undefined,
      port: recordForm.type === 'SRV' ? recordForm.port : undefined,
    };

    if (editingRecordId) {
      updateRecordMutation.mutate({
        zoneId: selectedZoneId,
        recordId: editingRecordId,
        record: recordData,
      });
    } else {
      createRecordMutation.mutate({
        zoneId: selectedZoneId,
        record: recordData,
      });
    }
  };

  const handleDeleteZone = (zone: Zone) => {
    if (confirm(`Delete zone "${zone.name}"? This will delete all records in this zone.`)) {
      deleteZoneMutation.mutate(zone.id);
    }
  };

  const handleDeleteRecord = (record: DNSRecord) => {
    if (!selectedZoneId) return;
    if (confirm(`Delete record "${record.name}" (${record.type})?`)) {
      deleteRecordMutation.mutate({ zoneId: selectedZoneId, recordId: record.id });
    }
  };

  // Loading state
  if (zonesLoading && viewMode === 'zones') {
    return (
      <div className="space-y-4 rounded-lg border p-4">
        <div className="flex items-center justify-between">
          <Skeleton className="h-6 w-48" />
          <Skeleton className="h-9 w-24" />
        </div>
        <div className="space-y-2">
          {[1, 2, 3].map((i) => (
            <div key={i} className="flex items-center justify-between p-4 rounded-lg border">
              <div className="flex items-center gap-4">
                <Skeleton className="h-8 w-8 rounded" />
                <div className="space-y-2">
                  <Skeleton className="h-4 w-40" />
                  <Skeleton className="h-3 w-32" />
                </div>
              </div>
              <Skeleton className="h-8 w-20" />
            </div>
          ))}
        </div>
      </div>
    );
  }

  // Error state
  if (zonesError) {
    return (
      <div className="card p-4">
        <div className="text-error">
          Error loading DNS zones. DNS management may not be configured.
        </div>
      </div>
    );
  }

  const zones = zonesData?.zones || [];
  const records = recordsData?.records || [];

  // Zones list view
  if (viewMode === 'zones') {
    return (
      <div className="card p-4 space-y-4">
        <div className="flex items-center justify-between flex-wrap gap-2">
          <div className="flex items-center gap-2">
            <Globe className="h-5 w-5 text-primary" />
            <h2 className="text-lg font-semibold">DNS Zones ({zones.length})</h2>
          </div>
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={() => refetchZones()}>
              <RefreshCw className="h-4 w-4 mr-2" />
              Refresh
            </Button>
            <Button size="sm" onClick={() => setShowNewZoneForm(true)}>
              <Plus className="h-4 w-4 mr-2" />
              New Zone
            </Button>
          </div>
        </div>

        {/* New Zone Form */}
        {showNewZoneForm && (
          <div className="rounded-lg border p-4 bg-muted/50">
            <div className="flex items-center justify-between mb-4">
              <h3 className="font-medium">Create New Zone</h3>
              <Button variant="ghost" size="sm" onClick={() => setShowNewZoneForm(false)}>
                <X className="h-4 w-4" />
              </Button>
            </div>
            <form onSubmit={handleSubmitZone} className="space-y-4">
              <div>
                <label className="block text-sm font-medium mb-1">Domain Name *</label>
                <input
                  type="text"
                  value={newZoneName}
                  onChange={(e) => setNewZoneName(e.target.value)}
                  placeholder="example.com"
                  className="w-full px-3 py-2 rounded-md border bg-background"
                  required
                />
              </div>
              <div>
                <label className="block text-sm font-medium mb-1">Zone Type</label>
                <select
                  value={newZoneType}
                  onChange={(e) => setNewZoneType(e.target.value as ZoneType)}
                  className="w-full px-3 py-2 rounded-md border bg-background"
                >
                  <option value="primary">Primary</option>
                  <option value="secondary">Secondary</option>
                </select>
              </div>
              <div className="flex items-center gap-2">
                <Button type="submit" disabled={createZoneMutation.isPending || !newZoneName.trim()}>
                  {createZoneMutation.isPending ? (
                    <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  ) : (
                    <Plus className="h-4 w-4 mr-2" />
                  )}
                  Create Zone
                </Button>
                <Button type="button" variant="outline" onClick={() => setShowNewZoneForm(false)}>
                  Cancel
                </Button>
              </div>
              {createZoneMutation.error && (
                <p className="text-sm text-error">
                  {(createZoneMutation.error as Error).message}
                </p>
              )}
            </form>
          </div>
        )}

        {/* Zones List */}
        {zones.length === 0 ? (
          <div className="empty-state border rounded-lg">
            <Globe className="empty-state-icon" />
            <p className="empty-state-title">No DNS zones found</p>
            <p className="empty-state-description">Click "New Zone" to create a DNS zone</p>
          </div>
        ) : (
          <div className="space-y-2">
            {zones.map((zone) => (
              <div
                key={zone.id}
                className="flex items-center justify-between p-4 rounded-lg border hover:bg-muted/50 cursor-pointer"
                onClick={() => handleSelectZone(zone)}
              >
                <div className="flex items-center gap-4">
                  <Server className="h-6 w-6 text-primary" />
                  <div>
                    <p className="font-medium">{zone.name}</p>
                    <div className="flex items-center gap-2 text-sm text-muted-foreground mt-1">
                      <span className={cn(
                        zone.type === 'primary' ? 'badge-success' : 'badge-info'
                      )}>
                        {zone.type}
                      </span>
                      <span>{zone.record_count} records</span>
                    </div>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={(e) => {
                      e.stopPropagation();
                      handleDeleteZone(zone);
                    }}
                    disabled={deleteZoneMutation.isPending}
                    title="Delete zone"
                  >
                    {deleteZoneMutation.isPending ? (
                      <Loader2 className="h-4 w-4 animate-spin" />
                    ) : (
                      <Trash2 className="h-4 w-4" />
                    )}
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    );
  }

  // Zone detail view
  return (
    <div className="card p-4 space-y-4">
      <div className="flex items-center justify-between flex-wrap gap-2">
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="sm" onClick={handleBackToZones}>
            <ArrowLeft className="h-4 w-4" />
          </Button>
          <Globe className="h-5 w-5 text-primary" />
          <h2 className="text-lg font-semibold">
            {zoneLoading ? <Skeleton className="h-6 w-40" /> : selectedZone?.name}
          </h2>
          {selectedZone && (
            <span className={cn(
              selectedZone.type === 'primary' ? 'badge-success' : 'badge-info'
            )}>
              {selectedZone.type}
            </span>
          )}
        </div>
        <div className="flex items-center gap-2 flex-wrap">
          <Button
            variant="outline"
            size="sm"
            onClick={() => selectedZoneId && validateZoneMutation.mutate(selectedZoneId)}
            disabled={validateZoneMutation.isPending}
          >
            {validateZoneMutation.isPending ? (
              <Loader2 className="h-4 w-4 mr-2 animate-spin" />
            ) : (
              <CheckCircle className="h-4 w-4 mr-2" />
            )}
            Validate
          </Button>
          <Button variant="outline" size="sm" onClick={() => refetchRecords()}>
            <RefreshCw className="h-4 w-4 mr-2" />
            Refresh
          </Button>
          <Button size="sm" onClick={() => { resetRecordForm(); setShowRecordForm(true); }}>
            <Plus className="h-4 w-4 mr-2" />
            Add Record
          </Button>
        </div>
      </div>

      {/* Validation Results */}
      {showValidation && validationResult && (
        <div className={cn(
          validationResult.valid ? 'alert-success' : 'alert-error'
        )}>
          <div className="flex items-center justify-between mb-2">
            <div className="flex items-center gap-2">
              {validationResult.valid ? (
                <CheckCircle className="h-5 w-5 text-success" />
              ) : (
                <AlertCircle className="h-5 w-5 text-error" />
              )}
              <h3 className="font-medium">
                {validationResult.valid ? 'Zone is valid' : 'Zone has issues'}
              </h3>
            </div>
            <Button variant="ghost" size="sm" onClick={() => setShowValidation(false)}>
              <X className="h-4 w-4" />
            </Button>
          </div>
          {validationResult.errors.length > 0 && (
            <div className="mt-2">
              <p className="text-sm font-medium mb-1">Errors:</p>
              <ul className="text-sm list-disc list-inside">
                {validationResult.errors.map((err, i) => (
                  <li key={i}>{err.message} {err.record_name && `(${err.record_name})`}</li>
                ))}
              </ul>
            </div>
          )}
          {validationResult.warnings.length > 0 && (
            <div className="mt-2 text-warning">
              <p className="text-sm font-medium mb-1">Warnings:</p>
              <ul className="text-sm list-disc list-inside">
                {validationResult.warnings.map((warn, i) => (
                  <li key={i}>{warn.message} {warn.record_name && `(${warn.record_name})`}</li>
                ))}
              </ul>
            </div>
          )}
        </div>
      )}

      {/* Record Form */}
      {showRecordForm && (
        <div className="rounded-lg border p-4 bg-muted/50">
          <div className="flex items-center justify-between mb-4">
            <h3 className="font-medium">
              {editingRecordId ? 'Edit Record' : 'Add New Record'}
            </h3>
            <Button variant="ghost" size="sm" onClick={resetRecordForm}>
              <X className="h-4 w-4" />
            </Button>
          </div>
          <form onSubmit={handleSubmitRecord} className="space-y-4">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <label className="block text-sm font-medium mb-1">Name *</label>
                <input
                  type="text"
                  value={recordForm.name}
                  onChange={(e) => setRecordForm({ ...recordForm, name: e.target.value })}
                  placeholder="@, www, mail, etc."
                  className="w-full px-3 py-2 rounded-md border bg-background"
                  required
                />
                <p className="text-xs text-muted-foreground mt-1">
                  Use @ for root domain or subdomain name
                </p>
              </div>
              <div>
                <label className="block text-sm font-medium mb-1">Type *</label>
                <select
                  value={recordForm.type}
                  onChange={(e) => setRecordForm({ ...recordForm, type: e.target.value as RecordType })}
                  className="w-full px-3 py-2 rounded-md border bg-background"
                >
                  {RECORD_TYPES.map((type) => (
                    <option key={type} value={type}>{type}</option>
                  ))}
                </select>
              </div>
            </div>

            <div>
              <label className="block text-sm font-medium mb-1">Value *</label>
              <input
                type="text"
                value={recordForm.value}
                onChange={(e) => setRecordForm({ ...recordForm, value: e.target.value })}
                placeholder={RECORD_TYPE_HINTS[recordForm.type]}
                className="w-full px-3 py-2 rounded-md border bg-background"
                required
              />
              <p className="text-xs text-muted-foreground mt-1">
                {RECORD_TYPE_HINTS[recordForm.type]}
              </p>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
              <div>
                <label className="block text-sm font-medium mb-1">TTL</label>
                <select
                  value={recordForm.ttl}
                  onChange={(e) => setRecordForm({ ...recordForm, ttl: Number(e.target.value) })}
                  className="w-full px-3 py-2 rounded-md border bg-background"
                >
                  {TTL_PRESETS.map((preset) => (
                    <option key={preset.value} value={preset.value}>{preset.label}</option>
                  ))}
                </select>
              </div>

              {(recordForm.type === 'MX' || recordForm.type === 'SRV') && (
                <div>
                  <label className="block text-sm font-medium mb-1">Priority *</label>
                  <input
                    type="number"
                    value={recordForm.priority ?? ''}
                    onChange={(e) => setRecordForm({ ...recordForm, priority: e.target.value ? Number(e.target.value) : undefined })}
                    placeholder="10"
                    min="0"
                    className="w-full px-3 py-2 rounded-md border bg-background"
                  />
                </div>
              )}

              {recordForm.type === 'SRV' && (
                <>
                  <div>
                    <label className="block text-sm font-medium mb-1">Weight *</label>
                    <input
                      type="number"
                      value={recordForm.weight ?? ''}
                      onChange={(e) => setRecordForm({ ...recordForm, weight: e.target.value ? Number(e.target.value) : undefined })}
                      placeholder="5"
                      min="0"
                      className="w-full px-3 py-2 rounded-md border bg-background"
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-medium mb-1">Port *</label>
                    <input
                      type="number"
                      value={recordForm.port ?? ''}
                      onChange={(e) => setRecordForm({ ...recordForm, port: e.target.value ? Number(e.target.value) : undefined })}
                      placeholder="5060"
                      min="1"
                      max="65535"
                      className="w-full px-3 py-2 rounded-md border bg-background"
                    />
                  </div>
                </>
              )}
            </div>

            {recordFormErrors.length > 0 && (
              <div className="text-sm text-error">
                <ul className="list-disc list-inside">
                  {recordFormErrors.map((err, i) => (
                    <li key={i}>{err}</li>
                  ))}
                </ul>
              </div>
            )}

            <div className="flex items-center gap-2">
              <Button
                type="submit"
                disabled={createRecordMutation.isPending || updateRecordMutation.isPending}
              >
                {(createRecordMutation.isPending || updateRecordMutation.isPending) ? (
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                ) : editingRecordId ? (
                  <Edit2 className="h-4 w-4 mr-2" />
                ) : (
                  <Plus className="h-4 w-4 mr-2" />
                )}
                {editingRecordId ? 'Update Record' : 'Add Record'}
              </Button>
              <Button type="button" variant="outline" onClick={resetRecordForm}>
                Cancel
              </Button>
            </div>
          </form>
        </div>
      )}

      {/* Records List */}
      {recordsLoading ? (
        <div className="space-y-2">
          {[1, 2, 3].map((i) => (
            <div key={i} className="flex items-center justify-between p-4 rounded-lg border">
              <div className="flex items-center gap-4">
                <Skeleton className="h-6 w-12" />
                <div className="space-y-2">
                  <Skeleton className="h-4 w-32" />
                  <Skeleton className="h-3 w-48" />
                </div>
              </div>
              <Skeleton className="h-8 w-20" />
            </div>
          ))}
        </div>
      ) : records.length === 0 ? (
        <div className="empty-state border rounded-lg">
          <Server className="empty-state-icon" />
          <p className="empty-state-title">No DNS records found</p>
          <p className="empty-state-description">Click "Add Record" to create a DNS record</p>
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead>
              <tr className="border-b">
                <th className="text-left py-2 px-3 font-medium">Type</th>
                <th className="text-left py-2 px-3 font-medium">Name</th>
                <th className="text-left py-2 px-3 font-medium">Value</th>
                <th className="text-left py-2 px-3 font-medium">TTL</th>
                <th className="text-left py-2 px-3 font-medium">Priority</th>
                <th className="text-right py-2 px-3 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {records.map((record) => (
                <tr key={record.id} className="border-b hover:bg-muted/50">
                  <td className="py-2 px-3">
                    <span className={cn(
                      'px-2 py-0.5 rounded text-xs font-medium',
                      getRecordTypeColor(record.type)
                    )}>
                      {record.type}
                    </span>
                  </td>
                  <td className="py-2 px-3 font-mono text-sm">{record.name}</td>
                  <td className="py-2 px-3 font-mono text-sm max-w-xs truncate" title={record.value}>
                    {record.value}
                  </td>
                  <td className="py-2 px-3 text-sm text-muted-foreground">
                    <span className="flex items-center gap-1">
                      <Clock className="h-3 w-3" />
                      {formatTTL(record.ttl)}
                    </span>
                  </td>
                  <td className="py-2 px-3 text-sm text-muted-foreground">
                    {record.priority !== undefined ? record.priority : '-'}
                    {record.type === 'SRV' && record.weight !== undefined && ` / ${record.weight}`}
                    {record.type === 'SRV' && record.port !== undefined && ` / ${record.port}`}
                  </td>
                  <td className="py-2 px-3 text-right">
                    <div className="flex items-center justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => handleEditRecord(record)}
                        disabled={updateRecordMutation.isPending || deleteRecordMutation.isPending}
                        title="Edit record"
                      >
                        <Edit2 className="h-4 w-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => handleDeleteRecord(record)}
                        disabled={deleteRecordMutation.isPending}
                        title="Delete record"
                      >
                        {deleteRecordMutation.isPending ? (
                          <Loader2 className="h-4 w-4 animate-spin" />
                        ) : (
                          <Trash2 className="h-4 w-4" />
                        )}
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Zone Info Footer */}
      {selectedZone && (
        <div className="text-xs text-muted-foreground text-center pt-2 border-t">
          Serial: {selectedZone.serial} | Refresh: {formatTTL(selectedZone.refresh)} |
          Retry: {formatTTL(selectedZone.retry)} | Expire: {formatTTL(selectedZone.expire)}
        </div>
      )}
    </div>
  );
}
