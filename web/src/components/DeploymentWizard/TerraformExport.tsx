import { useState, useCallback } from 'react';
import {
  Download,
  Folder,
  FolderOpen,
  FileCode2,
  File,
  Copy,
  Check,
  ChevronRight,
  ChevronDown,
  ArrowLeft,
  Loader2,
  Terminal,
  BookOpen,
  Settings,
} from 'lucide-react';
import { buttonVariants } from '@/lib/button-variants';
import type { Resource, ExportTerraformConfig } from '@/lib/migrate-api';
import { exportTerraform } from '@/lib/migrate-api';

interface TerraformExportProps {
  provider: 'hetzner' | 'scaleway' | 'ovh';
  resources: Resource[];
  config: {
    project_name: string;
    domain: string;
    region: string;
  };
  onBack?: () => void;
}

// Simulated file structure for the Terraform export
interface FileNode {
  name: string;
  type: 'file' | 'folder';
  children?: FileNode[];
  content?: string;
}

const providerDisplayNames: Record<string, string> = {
  hetzner: 'Hetzner Cloud',
  scaleway: 'Scaleway',
  ovh: 'OVHcloud',
};

const providerRegions: Record<string, Record<string, string>> = {
  hetzner: {
    fsn1: 'Falkenstein, Germany',
    nbg1: 'Nuremberg, Germany',
    hel1: 'Helsinki, Finland',
    ash: 'Ashburn, USA',
    hil: 'Hillsboro, USA',
  },
  scaleway: {
    'fr-par': 'Paris, France',
    'nl-ams': 'Amsterdam, Netherlands',
    'pl-waw': 'Warsaw, Poland',
  },
  ovh: {
    gra: 'Gravelines, France',
    sbg: 'Strasbourg, France',
    bhs: 'Beauharnois, Canada',
    waw: 'Warsaw, Poland',
    uk: 'London, UK',
  },
};

function generateFileTree(provider: string, projectName: string): FileNode {
  return {
    name: 'terraform',
    type: 'folder',
    children: [
      {
        name: 'main.tf',
        type: 'file',
        content: `# Main Terraform configuration for ${projectName}
# Provider: ${providerDisplayNames[provider]}

terraform {
  required_version = ">= 1.0.0"
  required_providers {
    ${provider} = {
      source  = "${provider === 'hetzner' ? 'hetznercloud/hcloud' : provider === 'scaleway' ? 'scaleway/scaleway' : 'ovh/ovh'}"
      version = "~> ${provider === 'hetzner' ? '1.45' : provider === 'scaleway' ? '2.35' : '0.36'}"
    }
  }
}

# See variables.tf for configuration options
# See outputs.tf for exported values`,
      },
      {
        name: 'variables.tf',
        type: 'file',
        content: `# Input variables for ${projectName}

variable "project_name" {
  description = "Name of the project"
  type        = string
  default     = "${projectName}"
}

variable "region" {
  description = "Region to deploy resources"
  type        = string
}

variable "domain" {
  description = "Base domain for services"
  type        = string
}

variable "ssh_public_key" {
  description = "SSH public key for server access"
  type        = string
}`,
      },
      {
        name: 'outputs.tf',
        type: 'file',
        content: `# Output values

output "server_ip" {
  description = "Public IP of the main server"
  value       = module.compute.server_ip
}

output "domain" {
  description = "Domain name for services"
  value       = var.domain
}

output "ssh_command" {
  description = "SSH command to connect"
  value       = "ssh root@\${module.compute.server_ip}"
}`,
      },
      {
        name: 'terraform.tfvars.example',
        type: 'file',
        content: `# Copy this file to terraform.tfvars and fill in your values

project_name   = "${projectName}"
region         = "fsn1"
domain         = "example.com"
ssh_public_key = "ssh-ed25519 AAAA..."

# Provider credentials (or set via environment variables)
# ${provider === 'hetzner' ? 'hcloud_token' : provider === 'scaleway' ? 'scw_access_key\nscw_secret_key' : 'ovh_application_key\novh_application_secret'} = "..."`,
      },
      {
        name: 'modules',
        type: 'folder',
        children: [
          {
            name: 'compute',
            type: 'folder',
            children: [
              { name: 'main.tf', type: 'file', content: '# Compute resources' },
              { name: 'variables.tf', type: 'file', content: '# Compute variables' },
              { name: 'outputs.tf', type: 'file', content: '# Compute outputs' },
            ],
          },
          {
            name: 'networking',
            type: 'folder',
            children: [
              { name: 'main.tf', type: 'file', content: '# Network resources' },
              { name: 'variables.tf', type: 'file', content: '# Network variables' },
              { name: 'outputs.tf', type: 'file', content: '# Network outputs' },
            ],
          },
          {
            name: 'storage',
            type: 'folder',
            children: [
              { name: 'main.tf', type: 'file', content: '# Storage resources' },
              { name: 'variables.tf', type: 'file', content: '# Storage variables' },
              { name: 'outputs.tf', type: 'file', content: '# Storage outputs' },
            ],
          },
        ],
      },
      {
        name: 'scripts',
        type: 'folder',
        children: [
          {
            name: 'deploy.sh',
            type: 'file',
            content: `#!/bin/bash
# Deploy script for ${projectName}

set -e

echo "Initializing Terraform..."
terraform init

echo "Planning infrastructure..."
terraform plan -out=tfplan

echo "Applying infrastructure..."
terraform apply tfplan

echo "Deployment complete!"`,
          },
          {
            name: 'setup-docker.sh',
            type: 'file',
            content: `#!/bin/bash
# Docker setup script

set -e

# Install Docker
curl -fsSL https://get.docker.com | sh

# Enable and start Docker
systemctl enable docker
systemctl start docker

echo "Docker installed successfully!"`,
          },
        ],
      },
      {
        name: 'docker-compose.yml',
        type: 'file',
        content: `# Docker Compose configuration
version: '3.8'

services:
  traefik:
    image: traefik:v3.0
    # ... configuration`,
      },
    ],
  };
}

function FileTreeItem({
  node,
  depth = 0,
  selectedFile,
  onSelect,
}: {
  node: FileNode;
  depth?: number;
  selectedFile: string | null;
  onSelect: (node: FileNode) => void;
}) {
  const [isOpen, setIsOpen] = useState(depth < 2);
  const isSelected = selectedFile === node.name && node.type === 'file';

  const handleClick = () => {
    if (node.type === 'folder') {
      setIsOpen(!isOpen);
    } else {
      onSelect(node);
    }
  };

  const Icon =
    node.type === 'folder'
      ? isOpen
        ? FolderOpen
        : Folder
      : node.name.endsWith('.tf')
      ? FileCode2
      : node.name.endsWith('.sh')
      ? Terminal
      : node.name.endsWith('.yml') || node.name.endsWith('.yaml')
      ? Settings
      : File;

  const iconColor =
    node.type === 'folder'
      ? 'text-blue-400'
      : node.name.endsWith('.tf')
      ? 'text-purple-400'
      : node.name.endsWith('.sh')
      ? 'text-green-400'
      : node.name.endsWith('.yml') || node.name.endsWith('.yaml')
      ? 'text-orange-400'
      : 'text-muted-foreground';

  return (
    <div>
      <button
        onClick={handleClick}
        className={`w-full flex items-center gap-2 px-2 py-1.5 text-sm text-left rounded transition-colors ${
          isSelected
            ? 'bg-accent/20 text-accent'
            : 'hover:bg-muted/50 text-foreground'
        }`}
        style={{ paddingLeft: `${depth * 16 + 8}px` }}
      >
        {node.type === 'folder' && (
          <span className="text-muted-foreground">
            {isOpen ? (
              <ChevronDown className="h-3 w-3" />
            ) : (
              <ChevronRight className="h-3 w-3" />
            )}
          </span>
        )}
        {node.type === 'file' && <span className="w-3" />}
        <Icon className={`h-4 w-4 ${iconColor}`} />
        <span className="truncate">{node.name}</span>
      </button>
      {node.type === 'folder' && isOpen && node.children && (
        <div>
          {node.children.map((child, index) => (
            <FileTreeItem
              key={`${child.name}-${index}`}
              node={child}
              depth={depth + 1}
              selectedFile={selectedFile}
              onSelect={onSelect}
            />
          ))}
        </div>
      )}
    </div>
  );
}

export function TerraformExport({
  provider,
  resources,
  config,
  onBack,
}: TerraformExportProps) {
  const [isDownloading, setIsDownloading] = useState(false);
  const [copied, setCopied] = useState(false);
  const [selectedFile, setSelectedFile] = useState<FileNode | null>(null);
  const [downloadError, setDownloadError] = useState<string | null>(null);

  const fileTree = generateFileTree(provider, config.project_name);
  const regionDisplay =
    providerRegions[provider]?.[config.region] || config.region;

  const deployCommand = `cd terraform && terraform init && terraform plan && terraform apply`;

  const handleDownload = useCallback(async () => {
    setIsDownloading(true);
    setDownloadError(null);

    try {
      const exportConfig: ExportTerraformConfig = {
        provider,
        project_name: config.project_name,
        domain: config.domain,
        region: config.region,
      };

      const blob = await exportTerraform(resources, exportConfig);

      // Create download link
      const url = window.URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `${config.project_name}-terraform.zip`;
      document.body.appendChild(a);
      a.click();
      window.URL.revokeObjectURL(url);
      document.body.removeChild(a);
    } catch (error) {
      setDownloadError(
        error instanceof Error ? error.message : 'Download failed'
      );
    } finally {
      setIsDownloading(false);
    }
  }, [provider, resources, config]);

  const handleCopyCommand = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(deployCommand);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Fallback for older browsers
      const textArea = document.createElement('textarea');
      textArea.value = deployCommand;
      document.body.appendChild(textArea);
      textArea.select();
      document.execCommand('copy');
      document.body.removeChild(textArea);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  }, [deployCommand]);

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <div className="flex items-center gap-3 mb-2">
            <h2 className="text-2xl font-bold">Terraform Export</h2>
            <span className="badge-freedom px-3 py-1 text-xs">EU Provider</span>
          </div>
          <p className="text-muted-foreground">
            Export your infrastructure as Terraform configuration for{' '}
            {providerDisplayNames[provider]}
          </p>
        </div>
        <div className="text-right text-sm">
          <p className="text-muted-foreground">Target Region</p>
          <p className="font-medium">{regionDisplay}</p>
        </div>
      </div>

      {/* Summary Stats */}
      <div className="grid grid-cols-3 gap-4">
        <div className="card-stat">
          <p className="card-stat-label">Resources</p>
          <p className="card-stat-value">{resources.length}</p>
        </div>
        <div className="card-stat">
          <p className="card-stat-label">Provider</p>
          <p className="card-stat-value text-lg">
            {providerDisplayNames[provider]}
          </p>
        </div>
        <div className="card-stat">
          <p className="card-stat-label">Project</p>
          <p className="card-stat-value text-lg truncate">
            {config.project_name}
          </p>
        </div>
      </div>

      {/* File Tree and Preview */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* File Tree */}
        <div className="border rounded-lg overflow-hidden">
          <div className="bg-muted/50 border-b px-4 py-3 flex items-center gap-2">
            <Folder className="h-4 w-4 text-blue-400" />
            <span className="font-medium text-sm">Project Files</span>
            <span className="text-xs text-muted-foreground ml-auto">
              Click to preview
            </span>
          </div>
          <div className="p-2 max-h-80 overflow-y-auto bg-background">
            <FileTreeItem
              node={fileTree}
              selectedFile={selectedFile?.name || null}
              onSelect={setSelectedFile}
            />
          </div>
        </div>

        {/* File Preview */}
        <div className="border rounded-lg overflow-hidden">
          <div className="bg-muted/50 border-b px-4 py-3 flex items-center gap-2">
            <FileCode2 className="h-4 w-4 text-purple-400" />
            <span className="font-medium text-sm">
              {selectedFile?.name || 'Select a file to preview'}
            </span>
          </div>
          <div className="code-block max-h-80 overflow-y-auto">
            {selectedFile?.content ? (
              <pre className="text-xs font-mono whitespace-pre-wrap">
                {selectedFile.content}
              </pre>
            ) : (
              <div className="text-muted-foreground text-sm p-4 text-center">
                Select a file from the tree to preview its contents
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Deploy Command */}
      <div className="terminal">
        <div className="terminal-header">
          <div className="flex items-center gap-2">
            <Terminal className="h-4 w-4" />
            <span>Deploy Command</span>
          </div>
          <button
            onClick={handleCopyCommand}
            className="flex items-center gap-1.5 text-xs hover:text-accent transition-colors"
          >
            {copied ? (
              <>
                <Check className="h-3.5 w-3.5 text-green-400" />
                <span className="text-green-400">Copied!</span>
              </>
            ) : (
              <>
                <Copy className="h-3.5 w-3.5" />
                <span>Copy</span>
              </>
            )}
          </button>
        </div>
        <div className="terminal-body">
          <code className="text-sm font-mono">{deployCommand}</code>
        </div>
      </div>

      {/* Step-by-Step Instructions */}
      <div className="card-action">
        <div className="flex items-center gap-2 mb-4">
          <BookOpen className="h-5 w-5 text-accent" />
          <h3 className="font-semibold">Deployment Instructions</h3>
        </div>
        <ol className="space-y-4">
          <li className="flex gap-4">
            <span className="flex-shrink-0 w-7 h-7 rounded-full bg-accent/20 text-accent flex items-center justify-center text-sm font-medium">
              1
            </span>
            <div>
              <p className="font-medium">Download and extract ZIP</p>
              <p className="text-sm text-muted-foreground">
                Click the download button below to get your Terraform
                configuration files
              </p>
            </div>
          </li>
          <li className="flex gap-4">
            <span className="flex-shrink-0 w-7 h-7 rounded-full bg-accent/20 text-accent flex items-center justify-center text-sm font-medium">
              2
            </span>
            <div>
              <p className="font-medium">Configure credentials</p>
              <p className="text-sm text-muted-foreground">
                Copy{' '}
                <code className="code-inline">terraform.tfvars.example</code> to{' '}
                <code className="code-inline">terraform.tfvars</code> and fill
                in your {providerDisplayNames[provider]} API credentials
              </p>
            </div>
          </li>
          <li className="flex gap-4">
            <span className="flex-shrink-0 w-7 h-7 rounded-full bg-accent/20 text-accent flex items-center justify-center text-sm font-medium">
              3
            </span>
            <div>
              <p className="font-medium">Review and customize</p>
              <p className="text-sm text-muted-foreground">
                Edit <code className="code-inline">terraform.tfvars</code> to
                customize region, SSH keys, and other settings
              </p>
            </div>
          </li>
          <li className="flex gap-4">
            <span className="flex-shrink-0 w-7 h-7 rounded-full bg-accent/20 text-accent flex items-center justify-center text-sm font-medium">
              4
            </span>
            <div>
              <p className="font-medium">Deploy infrastructure</p>
              <p className="text-sm text-muted-foreground">
                Run{' '}
                <code className="code-inline">./scripts/deploy.sh</code> or
                execute Terraform commands manually
              </p>
            </div>
          </li>
        </ol>
      </div>

      {/* Error Alert */}
      {downloadError && (
        <div className="alert-error">
          <p className="font-medium">Download failed</p>
          <p className="text-sm">{downloadError}</p>
        </div>
      )}

      {/* Actions */}
      <div className="flex items-center justify-between pt-4 border-t">
        {onBack ? (
          <button
            onClick={onBack}
            className={buttonVariants({ variant: 'ghost' })}
          >
            <ArrowLeft className="h-4 w-4 mr-2" />
            Back
          </button>
        ) : (
          <div />
        )}

        <div className="flex items-center gap-3">
          <button
            onClick={handleDownload}
            disabled={isDownloading}
            className={buttonVariants({ variant: 'freedom', size: 'lg' })}
          >
            {isDownloading ? (
              <>
                <Loader2 className="h-5 w-5 animate-spin" />
                Generating...
              </>
            ) : (
              <>
                <Download className="h-5 w-5" />
                Download Terraform ZIP
              </>
            )}
          </button>
        </div>
      </div>
    </div>
  );
}
