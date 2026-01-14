import { useState, useMemo } from 'react';
import { HardDrive, FolderOpen, Plus, Trash2 } from 'lucide-react';
import { ServiceMigrationCard } from '../ServiceMigrationCard';
import { useMigrationConfigStore } from '@/stores/migration-config';
import type { BucketMigration, EbsVolumeMigration, EfsMountTarget, EfsConfig } from '../types';
import type { Resource } from '@/lib/migrate-api';

// Resource type matchers for filtering which cards to show
const S3_TYPES = ['aws_s3_bucket', 'google_storage_bucket', 'azurerm_storage_account', 'azurerm_storage_container'];
const EBS_TYPES = ['aws_ebs_volume', 'google_compute_disk', 'azurerm_managed_disk'];
const EFS_TYPES = ['aws_efs_file_system', 'google_filestore_instance', 'azurerm_storage_share'];

// ============================================================================
// Bucket List Component
// ============================================================================

interface BucketListProps {
  buckets: BucketMigration[];
  onBucketsChange: (buckets: BucketMigration[]) => void;
}

function BucketList({ buckets, onBucketsChange }: BucketListProps) {
  const [newBucket, setNewBucket] = useState<BucketMigration>({
    sourceBucket: '',
    targetBucket: '',
    prefix: '',
  });

  const handleAddBucket = () => {
    if (newBucket.sourceBucket.trim() && newBucket.targetBucket.trim()) {
      onBucketsChange([...buckets, { ...newBucket }]);
      setNewBucket({ sourceBucket: '', targetBucket: '', prefix: '' });
    }
  };

  const handleRemoveBucket = (index: number) => {
    onBucketsChange(buckets.filter((_, i) => i !== index));
  };

  const handleUpdateBucket = (index: number, field: keyof BucketMigration, value: string) => {
    const updated = buckets.map((bucket, i) =>
      i === index ? { ...bucket, [field]: value } : bucket
    );
    onBucketsChange(updated);
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      handleAddBucket();
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <label className="block text-sm font-medium text-gray-700">
          Bucket Mappings
        </label>
        <span className="text-xs text-gray-500">
          {buckets.length} bucket{buckets.length !== 1 ? 's' : ''} configured
        </span>
      </div>

      {/* Bucket Table */}
      <div className="border border-gray-200 rounded-lg overflow-hidden">
        {/* Table Header */}
        <div className="bg-muted border-b border-border px-4 py-2">
          <div className="grid grid-cols-[1fr_1fr_1fr_auto] gap-3 text-xs font-medium text-gray-500 uppercase tracking-wider">
            <span>Source Bucket</span>
            <span>Target Bucket</span>
            <span>Prefix (Optional)</span>
            <span className="w-10"></span>
          </div>
        </div>

        {/* Existing Buckets */}
        {buckets.length > 0 && (
          <div className="divide-y divide-gray-100">
            {buckets.map((bucket, index) => (
              <div key={index} className="px-4 py-2 bg-white hover:bg-muted transition-colors">
                <div className="grid grid-cols-[1fr_1fr_1fr_auto] gap-3 items-center">
                  <input
                    type="text"
                    value={bucket.sourceBucket}
                    onChange={(e) => handleUpdateBucket(index, 'sourceBucket', e.target.value)}
                    placeholder="my-s3-bucket"
                    className="w-full px-2 py-1.5 text-sm border border-gray-200 rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                  />
                  <input
                    type="text"
                    value={bucket.targetBucket}
                    onChange={(e) => handleUpdateBucket(index, 'targetBucket', e.target.value)}
                    placeholder="my-minio-bucket"
                    className="w-full px-2 py-1.5 text-sm border border-gray-200 rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                  />
                  <input
                    type="text"
                    value={bucket.prefix || ''}
                    onChange={(e) => handleUpdateBucket(index, 'prefix', e.target.value)}
                    placeholder="data/"
                    className="w-full px-2 py-1.5 text-sm border border-gray-200 rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                  />
                  <button
                    type="button"
                    onClick={() => handleRemoveBucket(index)}
                    className="p-1.5 text-muted-foreground/60 hover:text-error hover:bg-error/10 rounded transition-colors"
                    aria-label="Remove bucket"
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}

        {/* Add New Bucket Row */}
        <div className="px-4 py-2 bg-muted border-t border-border">
          <div className="grid grid-cols-[1fr_1fr_1fr_auto] gap-3 items-center">
            <input
              type="text"
              value={newBucket.sourceBucket}
              onChange={(e) => setNewBucket({ ...newBucket, sourceBucket: e.target.value })}
              onKeyDown={handleKeyDown}
              placeholder="Source bucket name"
              className="w-full px-2 py-1.5 text-sm border border-input rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            />
            <input
              type="text"
              value={newBucket.targetBucket}
              onChange={(e) => setNewBucket({ ...newBucket, targetBucket: e.target.value })}
              onKeyDown={handleKeyDown}
              placeholder="Target bucket name"
              className="w-full px-2 py-1.5 text-sm border border-input rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            />
            <input
              type="text"
              value={newBucket.prefix || ''}
              onChange={(e) => setNewBucket({ ...newBucket, prefix: e.target.value })}
              onKeyDown={handleKeyDown}
              placeholder="Optional prefix"
              className="w-full px-2 py-1.5 text-sm border border-input rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            />
            <button
              type="button"
              onClick={handleAddBucket}
              disabled={!newBucket.sourceBucket.trim() || !newBucket.targetBucket.trim()}
              className="p-1.5 text-primary hover:bg-primary/10 rounded transition-colors disabled:text-muted-foreground/40 disabled:hover:bg-transparent disabled:cursor-not-allowed"
              aria-label="Add bucket"
            >
              <Plus className="w-4 h-4" />
            </button>
          </div>
        </div>

        {/* Empty State */}
        {buckets.length === 0 && (
          <div className="px-4 py-6 text-center text-sm text-gray-500">
            No buckets configured. Add a bucket mapping above.
          </div>
        )}
      </div>

      <p className="text-xs text-gray-500">
        Map S3 buckets to MinIO buckets. Use prefix to migrate only specific paths.
      </p>
    </div>
  );
}

// ============================================================================
// EBS Volume List Component
// ============================================================================

interface EbsVolumeListProps {
  volumes: EbsVolumeMigration[];
  onVolumesChange: (volumes: EbsVolumeMigration[]) => void;
}

function EbsVolumeList({ volumes, onVolumesChange }: EbsVolumeListProps) {
  const [newVolume, setNewVolume] = useState<EbsVolumeMigration>({
    volumeId: '',
    volumeName: '',
    size: 0,
    storageDriver: 'local',
  });

  const handleAddVolume = () => {
    if (newVolume.volumeId.trim() && newVolume.volumeName.trim()) {
      onVolumesChange([...volumes, { ...newVolume }]);
      setNewVolume({ volumeId: '', volumeName: '', size: 0, storageDriver: 'local' });
    }
  };

  const handleRemoveVolume = (index: number) => {
    onVolumesChange(volumes.filter((_, i) => i !== index));
  };

  const handleUpdateVolume = (index: number, field: keyof EbsVolumeMigration, value: string | number) => {
    const updated = volumes.map((volume, i) =>
      i === index ? { ...volume, [field]: value } : volume
    );
    onVolumesChange(updated);
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      handleAddVolume();
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <label className="block text-sm font-medium text-gray-700">
          Volume Mappings
        </label>
        <span className="text-xs text-gray-500">
          {volumes.length} volume{volumes.length !== 1 ? 's' : ''} configured
        </span>
      </div>

      <div className="border border-gray-200 rounded-lg overflow-hidden">
        <div className="bg-muted border-b border-border px-4 py-2">
          <div className="grid grid-cols-[1fr_1fr_80px_120px_auto] gap-3 text-xs font-medium text-gray-500 uppercase tracking-wider">
            <span>Volume ID</span>
            <span>Docker Volume Name</span>
            <span>Size (GB)</span>
            <span>Driver</span>
            <span className="w-10"></span>
          </div>
        </div>

        {volumes.length > 0 && (
          <div className="divide-y divide-gray-100">
            {volumes.map((volume, index) => (
              <div key={index} className="px-4 py-2 bg-white hover:bg-muted transition-colors">
                <div className="grid grid-cols-[1fr_1fr_80px_120px_auto] gap-3 items-center">
                  <input
                    type="text"
                    value={volume.volumeId}
                    onChange={(e) => handleUpdateVolume(index, 'volumeId', e.target.value)}
                    placeholder="vol-0123456789"
                    className="w-full px-2 py-1.5 text-sm border border-gray-200 rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                  />
                  <input
                    type="text"
                    value={volume.volumeName}
                    onChange={(e) => handleUpdateVolume(index, 'volumeName', e.target.value)}
                    placeholder="my-docker-volume"
                    className="w-full px-2 py-1.5 text-sm border border-gray-200 rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                  />
                  <input
                    type="number"
                    value={volume.size || ''}
                    onChange={(e) => handleUpdateVolume(index, 'size', parseInt(e.target.value) || 0)}
                    placeholder="100"
                    className="w-full px-2 py-1.5 text-sm border border-gray-200 rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                  />
                  <select
                    value={volume.storageDriver}
                    onChange={(e) => handleUpdateVolume(index, 'storageDriver', e.target.value)}
                    className="w-full px-2 py-1.5 text-sm border border-gray-200 rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                  >
                    <option value="local">Local</option>
                    <option value="nfs">NFS</option>
                    <option value="overlay2">Overlay2</option>
                  </select>
                  <button
                    type="button"
                    onClick={() => handleRemoveVolume(index)}
                    className="p-1.5 text-muted-foreground/60 hover:text-error hover:bg-error/10 rounded transition-colors"
                    aria-label="Remove volume"
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}

        <div className="px-4 py-2 bg-muted border-t border-border">
          <div className="grid grid-cols-[1fr_1fr_80px_120px_auto] gap-3 items-center">
            <input
              type="text"
              value={newVolume.volumeId}
              onChange={(e) => setNewVolume({ ...newVolume, volumeId: e.target.value })}
              onKeyDown={handleKeyDown}
              placeholder="EBS Volume ID"
              className="w-full px-2 py-1.5 text-sm border border-input rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            />
            <input
              type="text"
              value={newVolume.volumeName}
              onChange={(e) => setNewVolume({ ...newVolume, volumeName: e.target.value })}
              onKeyDown={handleKeyDown}
              placeholder="Docker volume name"
              className="w-full px-2 py-1.5 text-sm border border-input rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            />
            <input
              type="number"
              value={newVolume.size || ''}
              onChange={(e) => setNewVolume({ ...newVolume, size: parseInt(e.target.value) || 0 })}
              onKeyDown={handleKeyDown}
              placeholder="GB"
              className="w-full px-2 py-1.5 text-sm border border-input rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            />
            <select
              value={newVolume.storageDriver}
              onChange={(e) => setNewVolume({ ...newVolume, storageDriver: e.target.value as EbsVolumeMigration['storageDriver'] })}
              className="w-full px-2 py-1.5 text-sm border border-input rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            >
              <option value="local">Local</option>
              <option value="nfs">NFS</option>
              <option value="overlay2">Overlay2</option>
            </select>
            <button
              type="button"
              onClick={handleAddVolume}
              disabled={!newVolume.volumeId.trim() || !newVolume.volumeName.trim()}
              className="p-1.5 text-primary hover:bg-primary/10 rounded transition-colors disabled:text-muted-foreground/40 disabled:hover:bg-transparent disabled:cursor-not-allowed"
              aria-label="Add volume"
            >
              <Plus className="w-4 h-4" />
            </button>
          </div>
        </div>

        {volumes.length === 0 && (
          <div className="px-4 py-6 text-center text-sm text-gray-500">
            No volumes configured. Add a volume mapping above.
          </div>
        )}
      </div>

      <p className="text-xs text-gray-500">
        Map EBS volumes to Docker volumes. The storage driver determines how data is stored locally.
      </p>
    </div>
  );
}

// ============================================================================
// EFS File System List Component
// ============================================================================

interface EfsFileSystemListProps {
  fileSystems: EfsMountTarget[];
  onFileSystemsChange: (fileSystems: EfsMountTarget[]) => void;
}

function EfsFileSystemList({ fileSystems, onFileSystemsChange }: EfsFileSystemListProps) {
  const [newFileSystem, setNewFileSystem] = useState<EfsMountTarget>({
    fileSystemId: '',
    fileSystemName: '',
    targetPath: '',
  });

  const handleAddFileSystem = () => {
    if (newFileSystem.fileSystemId.trim() && newFileSystem.targetPath.trim()) {
      onFileSystemsChange([...fileSystems, { ...newFileSystem }]);
      setNewFileSystem({ fileSystemId: '', fileSystemName: '', targetPath: '' });
    }
  };

  const handleRemoveFileSystem = (index: number) => {
    onFileSystemsChange(fileSystems.filter((_, i) => i !== index));
  };

  const handleUpdateFileSystem = (index: number, field: keyof EfsMountTarget, value: string) => {
    const updated = fileSystems.map((fs, i) =>
      i === index ? { ...fs, [field]: value } : fs
    );
    onFileSystemsChange(updated);
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      handleAddFileSystem();
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <label className="block text-sm font-medium text-gray-700">
          File System Mappings
        </label>
        <span className="text-xs text-gray-500">
          {fileSystems.length} file system{fileSystems.length !== 1 ? 's' : ''} configured
        </span>
      </div>

      <div className="border border-gray-200 rounded-lg overflow-hidden">
        <div className="bg-muted border-b border-border px-4 py-2">
          <div className="grid grid-cols-[1fr_1fr_1fr_auto] gap-3 text-xs font-medium text-gray-500 uppercase tracking-wider">
            <span>File System ID</span>
            <span>Display Name</span>
            <span>Target Mount Path</span>
            <span className="w-10"></span>
          </div>
        </div>

        {fileSystems.length > 0 && (
          <div className="divide-y divide-gray-100">
            {fileSystems.map((fs, index) => (
              <div key={index} className="px-4 py-2 bg-white hover:bg-muted transition-colors">
                <div className="grid grid-cols-[1fr_1fr_1fr_auto] gap-3 items-center">
                  <input
                    type="text"
                    value={fs.fileSystemId}
                    onChange={(e) => handleUpdateFileSystem(index, 'fileSystemId', e.target.value)}
                    placeholder="fs-0123456789"
                    className="w-full px-2 py-1.5 text-sm border border-gray-200 rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                  />
                  <input
                    type="text"
                    value={fs.fileSystemName}
                    onChange={(e) => handleUpdateFileSystem(index, 'fileSystemName', e.target.value)}
                    placeholder="my-shared-storage"
                    className="w-full px-2 py-1.5 text-sm border border-gray-200 rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                  />
                  <input
                    type="text"
                    value={fs.targetPath}
                    onChange={(e) => handleUpdateFileSystem(index, 'targetPath', e.target.value)}
                    placeholder="/mnt/nfs/shared"
                    className="w-full px-2 py-1.5 text-sm border border-gray-200 rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                  />
                  <button
                    type="button"
                    onClick={() => handleRemoveFileSystem(index)}
                    className="p-1.5 text-muted-foreground/60 hover:text-error hover:bg-error/10 rounded transition-colors"
                    aria-label="Remove file system"
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}

        <div className="px-4 py-2 bg-muted border-t border-border">
          <div className="grid grid-cols-[1fr_1fr_1fr_auto] gap-3 items-center">
            <input
              type="text"
              value={newFileSystem.fileSystemId}
              onChange={(e) => setNewFileSystem({ ...newFileSystem, fileSystemId: e.target.value })}
              onKeyDown={handleKeyDown}
              placeholder="EFS File System ID"
              className="w-full px-2 py-1.5 text-sm border border-input rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            />
            <input
              type="text"
              value={newFileSystem.fileSystemName}
              onChange={(e) => setNewFileSystem({ ...newFileSystem, fileSystemName: e.target.value })}
              onKeyDown={handleKeyDown}
              placeholder="Display name"
              className="w-full px-2 py-1.5 text-sm border border-input rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            />
            <input
              type="text"
              value={newFileSystem.targetPath}
              onChange={(e) => setNewFileSystem({ ...newFileSystem, targetPath: e.target.value })}
              onKeyDown={handleKeyDown}
              placeholder="Target mount path"
              className="w-full px-2 py-1.5 text-sm border border-input rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            />
            <button
              type="button"
              onClick={handleAddFileSystem}
              disabled={!newFileSystem.fileSystemId.trim() || !newFileSystem.targetPath.trim()}
              className="p-1.5 text-primary hover:bg-primary/10 rounded transition-colors disabled:text-muted-foreground/40 disabled:hover:bg-transparent disabled:cursor-not-allowed"
              aria-label="Add file system"
            >
              <Plus className="w-4 h-4" />
            </button>
          </div>
        </div>

        {fileSystems.length === 0 && (
          <div className="px-4 py-6 text-center text-sm text-gray-500">
            No file systems configured. Add a file system mapping above.
          </div>
        )}
      </div>

      <p className="text-xs text-gray-500">
        Map EFS file systems to local NFS mount points. Data will be synced to the target paths.
      </p>
    </div>
  );
}

// ============================================================================
// Main StorageMigration Component
// ============================================================================

interface StorageMigrationProps {
  resources: Resource[];
}

export function StorageMigration({ resources = [] }: StorageMigrationProps) {
  const { storage, setStorageConfig } = useMigrationConfigStore();

  // Determine which service cards to show based on discovered resources
  const { hasS3, hasEBS, hasEFS } = useMemo(() => ({
    hasS3: resources.some(r => S3_TYPES.includes(r.type)),
    hasEBS: resources.some(r => EBS_TYPES.includes(r.type)),
    hasEFS: resources.some(r => EFS_TYPES.includes(r.type)),
  }), [resources]);

  // S3 handlers
  const handleS3Toggle = (enabled: boolean) => {
    setStorageConfig({ enabled });
  };

  const handleBucketsChange = (buckets: BucketMigration[]) => {
    setStorageConfig({ buckets });
  };

  const handlePreserveMetadataChange = (preserveMetadata: boolean) => {
    setStorageConfig({ preserveMetadata });
  };

  const handlePreserveVersionsChange = (preserveVersions: boolean) => {
    setStorageConfig({ preserveVersions });
  };

  const handleFilterPatternChange = (filterPattern: string) => {
    setStorageConfig({ filterPattern: filterPattern || undefined });
  };

  const handleExcludePatternChange = (excludePattern: string) => {
    setStorageConfig({ excludePattern: excludePattern || undefined });
  };

  // EBS handlers
  const handleEbsToggle = (enabled: boolean) => {
    setStorageConfig({ ebs: { ...storage.ebs, enabled } });
  };

  const handleEbsVolumesChange = (volumes: EbsVolumeMigration[]) => {
    setStorageConfig({ ebs: { ...storage.ebs, volumes } });
  };

  const handleEbsOutputDirectoryChange = (outputDirectory: string) => {
    setStorageConfig({ ebs: { ...storage.ebs, outputDirectory } });
  };

  const handleEbsCreateSnapshotsChange = (createSnapshots: boolean) => {
    setStorageConfig({ ebs: { ...storage.ebs, createSnapshots } });
  };

  const handleEbsEncryptionChange = (encryptionEnabled: boolean) => {
    setStorageConfig({ ebs: { ...storage.ebs, encryptionEnabled } });
  };

  // EFS handlers
  const handleEfsToggle = (enabled: boolean) => {
    setStorageConfig({ efs: { ...storage.efs, enabled } });
  };

  const handleEfsFileSystemsChange = (fileSystems: EfsMountTarget[]) => {
    setStorageConfig({ efs: { ...storage.efs, fileSystems } });
  };

  const handleEfsServerImageChange = (nfsServerImage: string) => {
    setStorageConfig({ efs: { ...storage.efs, nfsServerImage } });
  };

  const handleEfsExportOptionsChange = (exportOptions: string) => {
    setStorageConfig({ efs: { ...storage.efs, exportOptions } });
  };

  const handleEfsSyncMethodChange = (syncMethod: EfsConfig['syncMethod']) => {
    setStorageConfig({ efs: { ...storage.efs, syncMethod } });
  };

  // If no storage resources discovered, show empty state
  if (!hasS3 && !hasEBS && !hasEFS) {
    return (
      <div className="text-center py-8 text-muted-foreground">
        <HardDrive className="w-12 h-12 mx-auto mb-3 opacity-50" />
        <p className="font-medium">No storage resources discovered</p>
        <p className="text-sm mt-1">S3 buckets, EBS volumes, or EFS file systems will appear here when detected.</p>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* S3 to MinIO Section - only show if S3 resources discovered */}
      {hasS3 && (
      <ServiceMigrationCard
        title="Amazon S3 → MinIO"
        description="Migrate S3 buckets to self-hosted MinIO"
        icon={HardDrive}
        enabled={storage.enabled}
        onToggle={handleS3Toggle}
        defaultExpanded={true}
      >
        <div className="space-y-6">
          {/* Bucket List */}
          <BucketList
            buckets={storage.buckets}
            onBucketsChange={handleBucketsChange}
          />

          {/* Options Grid */}
          <div className="grid grid-cols-2 gap-4">
            {/* Preserve Metadata */}
            <label className="flex items-center gap-3 cursor-pointer">
              <input
                type="checkbox"
                checked={storage.preserveMetadata}
                onChange={(e) => handlePreserveMetadataChange(e.target.checked)}
                className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
              />
              <div>
                <span className="text-sm font-medium text-gray-700">Preserve Metadata</span>
                <p className="text-xs text-gray-500">Keep object metadata and tags</p>
              </div>
            </label>

            {/* Preserve Versions */}
            <label className="flex items-center gap-3 cursor-pointer">
              <input
                type="checkbox"
                checked={storage.preserveVersions}
                onChange={(e) => handlePreserveVersionsChange(e.target.checked)}
                className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
              />
              <div>
                <span className="text-sm font-medium text-gray-700">Preserve Versions</span>
                <p className="text-xs text-gray-500">Migrate all object versions</p>
              </div>
            </label>
          </div>

          {/* Filter Patterns */}
          <div className="grid grid-cols-2 gap-4">
            {/* Filter Pattern */}
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">
                Filter Pattern
              </label>
              <input
                type="text"
                value={storage.filterPattern || ''}
                onChange={(e) => handleFilterPatternChange(e.target.value)}
                placeholder="*.jpg, data/**/*.json"
                className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
              />
              <p className="text-xs text-gray-500 mt-1">
                Glob pattern for files to include (empty = all files)
              </p>
            </div>

            {/* Exclude Pattern */}
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">
                Exclude Pattern
              </label>
              <input
                type="text"
                value={storage.excludePattern || ''}
                onChange={(e) => handleExcludePatternChange(e.target.value)}
                placeholder="*.tmp, temp/**"
                className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
              />
              <p className="text-xs text-gray-500 mt-1">
                Glob pattern for files to exclude
              </p>
            </div>
          </div>
        </div>
      </ServiceMigrationCard>
      )}

      {/* EBS to Docker Volumes Section - only show if EBS resources discovered */}
      {hasEBS && (
      <ServiceMigrationCard
        title="Amazon EBS → Docker Volumes"
        description="Migrate EBS snapshots to local volumes"
        icon={HardDrive}
        enabled={storage.ebs.enabled}
        onToggle={handleEbsToggle}
        defaultExpanded={true}
      >
        <div className="space-y-6">
          {/* Volume List */}
          <EbsVolumeList
            volumes={storage.ebs.volumes}
            onVolumesChange={handleEbsVolumesChange}
          />

          {/* Output Directory */}
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">
              Output Directory
            </label>
            <input
              type="text"
              value={storage.ebs.outputDirectory}
              onChange={(e) => handleEbsOutputDirectoryChange(e.target.value)}
              placeholder="/data/volumes"
              className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            />
            <p className="text-xs text-gray-500 mt-1">
              Local directory where volume data will be stored
            </p>
          </div>

          {/* Options Grid */}
          <div className="grid grid-cols-2 gap-4">
            {/* Create Snapshots */}
            <label className="flex items-center gap-3 cursor-pointer">
              <input
                type="checkbox"
                checked={storage.ebs.createSnapshots}
                onChange={(e) => handleEbsCreateSnapshotsChange(e.target.checked)}
                className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
              />
              <div>
                <span className="text-sm font-medium text-gray-700">Create Snapshots</span>
                <p className="text-xs text-gray-500">Create EBS snapshots before migration</p>
              </div>
            </label>

            {/* Enable Encryption */}
            <label className="flex items-center gap-3 cursor-pointer">
              <input
                type="checkbox"
                checked={storage.ebs.encryptionEnabled}
                onChange={(e) => handleEbsEncryptionChange(e.target.checked)}
                className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
              />
              <div>
                <span className="text-sm font-medium text-gray-700">Enable Encryption</span>
                <p className="text-xs text-gray-500">Encrypt volumes with LUKS</p>
              </div>
            </label>
          </div>
        </div>
      </ServiceMigrationCard>
      )}

      {/* EFS to NFS/Local Section - only show if EFS resources discovered */}
      {hasEFS && (
      <ServiceMigrationCard
        title="Amazon EFS → NFS/Local"
        description="Migrate EFS file systems"
        icon={FolderOpen}
        enabled={storage.efs.enabled}
        onToggle={handleEfsToggle}
        defaultExpanded={true}
      >
        <div className="space-y-6">
          {/* File System List */}
          <EfsFileSystemList
            fileSystems={storage.efs.fileSystems}
            onFileSystemsChange={handleEfsFileSystemsChange}
          />

          {/* NFS Server Configuration */}
          <div className="grid grid-cols-2 gap-4">
            {/* NFS Server Image */}
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">
                NFS Server Image
              </label>
              <input
                type="text"
                value={storage.efs.nfsServerImage}
                onChange={(e) => handleEfsServerImageChange(e.target.value)}
                placeholder="itsthenetwork/nfs-server-alpine:12"
                className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
              />
              <p className="text-xs text-gray-500 mt-1">
                Docker image for NFS server container
              </p>
            </div>

            {/* Export Options */}
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">
                Export Options
              </label>
              <input
                type="text"
                value={storage.efs.exportOptions}
                onChange={(e) => handleEfsExportOptionsChange(e.target.value)}
                placeholder="rw,sync,no_subtree_check"
                className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
              />
              <p className="text-xs text-gray-500 mt-1">
                NFS export options for shared directories
              </p>
            </div>
          </div>

          {/* Sync Method */}
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-2">
              Sync Method
            </label>
            <div className="flex gap-4">
              <label className="flex items-center gap-2 cursor-pointer">
                <input
                  type="radio"
                  name="syncMethod"
                  value="rsync"
                  checked={storage.efs.syncMethod === 'rsync'}
                  onChange={() => handleEfsSyncMethodChange('rsync')}
                  className="w-4 h-4 text-primary border-input focus:ring-blue-500"
                />
                <div>
                  <span className="text-sm font-medium text-gray-700">rsync</span>
                  <p className="text-xs text-gray-500">Standard file sync (recommended)</p>
                </div>
              </label>
              <label className="flex items-center gap-2 cursor-pointer">
                <input
                  type="radio"
                  name="syncMethod"
                  value="datasync"
                  checked={storage.efs.syncMethod === 'datasync'}
                  onChange={() => handleEfsSyncMethodChange('datasync')}
                  className="w-4 h-4 text-primary border-input focus:ring-blue-500"
                />
                <div>
                  <span className="text-sm font-medium text-gray-700">AWS DataSync</span>
                  <p className="text-xs text-gray-500">Use AWS DataSync agent</p>
                </div>
              </label>
            </div>
          </div>
        </div>
      </ServiceMigrationCard>
      )}
    </div>
  );
}

export default StorageMigration;
