/**
 * Design System Showcase
 *
 * This page demonstrates all components in the Homeport design system.
 * Use this as a visual reference and testing ground for the design system.
 *
 * Access at: /design-showcase (add to router for development)
 */

import { useState } from 'react';
import { Button } from '@/components/ui/button';
import {
  Server,
  Database,
  HardDrive,
  Globe,
  Shield,
  MessageSquare,
  Cloud,
  ArrowRight,
  CheckCircle,
  AlertTriangle,
  XCircle,
  Info,
  Upload,
  Moon,
  Sun,
  Search,
  MoreHorizontal,
  Loader2,
} from 'lucide-react';

export function DesignShowcase() {
  const [isDark, setIsDark] = useState(false);

  const toggleDarkMode = () => {
    document.documentElement.classList.toggle('dark');
    setIsDark(!isDark);
  };

  return (
    <div className="min-h-screen bg-background">
      {/* Header */}
      <div className="sticky top-0 z-50 bg-background border-b">
        <div className="max-w-7xl mx-auto px-6 py-4 flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-bold">Homeport Design System</h1>
            <p className="text-sm text-muted-foreground">
              Component showcase and reference
            </p>
          </div>
          <Button variant="ghost" size="icon" onClick={toggleDarkMode}>
            {isDark ? <Sun className="h-5 w-5" /> : <Moon className="h-5 w-5" />}
          </Button>
        </div>
      </div>

      <div className="max-w-7xl mx-auto px-6 py-8 space-y-12">
        {/* Colors */}
        <Section title="Colors" id="colors">
          <SubSection title="Brand Colors">
            <ColorSwatch name="Primary" className="bg-primary" />
            <ColorSwatch name="Secondary" className="bg-secondary" />
            <ColorSwatch name="Accent" className="bg-accent" />
          </SubSection>

          <SubSection title="Semantic Colors">
            <ColorSwatch name="Success" className="bg-success" />
            <ColorSwatch name="Warning" className="bg-warning" />
            <ColorSwatch name="Error" className="bg-error" />
            <ColorSwatch name="Info" className="bg-info" />
          </SubSection>

          <SubSection title="Cloud Provider Colors">
            <ColorSwatch name="AWS" className="bg-cloud-aws" />
            <ColorSwatch name="GCP" className="bg-cloud-gcp" />
            <ColorSwatch name="Azure" className="bg-cloud-azure" />
            <ColorSwatch name="Freedom" className="bg-freedom" />
          </SubSection>
        </Section>

        {/* Typography */}
        <Section title="Typography" id="typography">
          <div className="space-y-4">
            <div>
              <h1 className="text-4xl font-bold">Heading 1 - 4xl</h1>
              <code className="text-xs text-muted-foreground">text-4xl font-bold</code>
            </div>
            <div>
              <h2 className="text-3xl font-bold">Heading 2 - 3xl</h2>
              <code className="text-xs text-muted-foreground">text-3xl font-bold</code>
            </div>
            <div>
              <h3 className="text-2xl font-semibold">Heading 3 - 2xl</h3>
              <code className="text-xs text-muted-foreground">text-2xl font-semibold</code>
            </div>
            <div>
              <h4 className="text-xl font-medium">Heading 4 - xl</h4>
              <code className="text-xs text-muted-foreground">text-xl font-medium</code>
            </div>
            <div>
              <p className="text-base">Body text - base (default)</p>
              <code className="text-xs text-muted-foreground">text-base</code>
            </div>
            <div>
              <p className="text-sm text-muted-foreground">Small text - sm</p>
              <code className="text-xs text-muted-foreground">text-sm</code>
            </div>
            <div>
              <p className="text-xs text-muted-foreground">Tiny text - xs</p>
              <code className="text-xs text-muted-foreground">text-xs</code>
            </div>
            <div>
              <code className="code-inline">Inline code</code>
            </div>
          </div>
        </Section>

        {/* Buttons */}
        <Section title="Buttons" id="buttons">
          <SubSection title="Variants">
            <div className="flex flex-wrap gap-3">
              <Button variant="primary">Primary</Button>
              <Button variant="secondary">Secondary</Button>
              <Button variant="accent">Accent</Button>
              <Button variant="success">Success</Button>
              <Button variant="warning">Warning</Button>
              <Button variant="error">Error</Button>
              <Button variant="info">Info</Button>
              <Button variant="outline">Outline</Button>
              <Button variant="ghost">Ghost</Button>
              <Button variant="link">Link</Button>
            </div>
          </SubSection>

          <SubSection title="Cloud Provider Variants">
            <div className="flex flex-wrap gap-3">
              <Button variant="aws">
                <Cloud className="h-4 w-4" />
                AWS
              </Button>
              <Button variant="gcp">
                <Cloud className="h-4 w-4" />
                GCP
              </Button>
              <Button variant="azure">
                <Cloud className="h-4 w-4" />
                Azure
              </Button>
              <Button variant="freedom">
                <Server className="h-4 w-4" />
                Self-Hosted
              </Button>
            </div>
          </SubSection>

          <SubSection title="Sizes">
            <div className="flex flex-wrap items-center gap-3">
              <Button size="sm">Small</Button>
              <Button size="default">Default</Button>
              <Button size="lg">Large</Button>
              <Button size="xl">Extra Large</Button>
              <Button size="icon">
                <Search className="h-4 w-4" />
              </Button>
            </div>
          </SubSection>

          <SubSection title="States">
            <div className="flex flex-wrap gap-3">
              <Button disabled>Disabled</Button>
              <Button disabled>
                <Loader2 className="h-4 w-4 animate-spin" />
                Loading...
              </Button>
            </div>
          </SubSection>
        </Section>

        {/* Cards */}
        <Section title="Cards" id="cards">
          <SubSection title="Stat Cards">
            <div className="grid md:grid-cols-3 gap-4">
              <div className="card-stat">
                <p className="card-stat-label">Active Containers</p>
                <p className="card-stat-value">42</p>
                <p className="card-stat-change-positive">↑ 12% from last week</p>
              </div>
              <div className="card-stat">
                <p className="card-stat-label">CPU Usage</p>
                <p className="card-stat-value">65%</p>
                <p className="card-stat-change-negative">↓ 5% from last week</p>
              </div>
              <div className="card-stat">
                <p className="card-stat-label">Memory</p>
                <p className="card-stat-value">4.2 GB</p>
              </div>
            </div>
          </SubSection>

          <SubSection title="Resource Cards">
            <div className="space-y-3">
              <div className="card-resource">
                <div className="flex items-center gap-3">
                  <div className="resource-icon-compute">
                    <Server className="h-5 w-5" />
                  </div>
                  <div className="flex-1">
                    <h4 className="font-medium">web-server-prod</h4>
                    <p className="text-sm text-muted-foreground">t3.medium • us-east-1</p>
                  </div>
                  <span className="badge-aws">AWS EC2</span>
                </div>
              </div>
              <div className="card-resource-selected">
                <div className="flex items-center gap-3">
                  <div className="resource-icon-database">
                    <Database className="h-5 w-5" />
                  </div>
                  <div className="flex-1">
                    <h4 className="font-medium">postgres-main</h4>
                    <p className="text-sm text-muted-foreground">db.t3.large • us-east-1</p>
                  </div>
                  <span className="badge-aws">AWS RDS</span>
                </div>
              </div>
            </div>
          </SubSection>

          <SubSection title="Action Cards">
            <div className="grid md:grid-cols-2 gap-4">
              <div className="card-action">
                <Cloud className="h-8 w-8 mx-auto mb-2 text-primary" />
                <div className="font-medium">Cloud API</div>
                <div className="text-sm text-muted-foreground">
                  Discover live infrastructure
                </div>
              </div>
              <div className="card-action-active">
                <Upload className="h-8 w-8 mx-auto mb-2 text-accent" />
                <div className="font-medium">Upload Files</div>
                <div className="text-sm text-muted-foreground">
                  Terraform, CloudFormation, ARM
                </div>
              </div>
            </div>
          </SubSection>
        </Section>

        {/* Badges */}
        <Section title="Badges" id="badges">
          <SubSection title="Semantic Badges">
            <div className="flex flex-wrap gap-2">
              <span className="badge-default">Default</span>
              <span className="badge-secondary">Secondary</span>
              <span className="badge-success">Success</span>
              <span className="badge-warning">Warning</span>
              <span className="badge-error">Error</span>
              <span className="badge-info">Info</span>
              <span className="badge-outline">Outline</span>
            </div>
          </SubSection>

          <SubSection title="Provider Badges">
            <div className="flex flex-wrap gap-2">
              <span className="badge-aws">AWS</span>
              <span className="badge-gcp">GCP</span>
              <span className="badge-azure">Azure</span>
              <span className="badge-freedom">Self-Hosted</span>
            </div>
          </SubSection>
        </Section>

        {/* Status Indicators */}
        <Section title="Status Indicators" id="status">
          <div className="space-y-3">
            <div className="flex items-center gap-2">
              <span className="status-dot-success" />
              <span className="text-sm">Running</span>
            </div>
            <div className="flex items-center gap-2">
              <span className="status-dot-warning" />
              <span className="text-sm">Degraded</span>
            </div>
            <div className="flex items-center gap-2">
              <span className="status-dot-error" />
              <span className="status-dot-error" />
              <span className="text-sm">Failed</span>
            </div>
            <div className="flex items-center gap-2">
              <span className="status-dot-info" />
              <span className="text-sm">Pending</span>
            </div>
            <div className="flex items-center gap-2">
              <span className="status-dot-muted" />
              <span className="text-sm">Stopped</span>
            </div>
          </div>
        </Section>

        {/* Alerts */}
        <Section title="Alerts" id="alerts">
          <div className="space-y-4">
            <div className="alert-success">
              <CheckCircle className="h-5 w-5 flex-shrink-0" />
              <div>
                <p className="font-medium">Deployment successful!</p>
                <p className="text-sm mt-1">Your stack is now running at localhost:8080</p>
              </div>
            </div>
            <div className="alert-warning">
              <AlertTriangle className="h-5 w-5 flex-shrink-0" />
              <div>
                <p className="font-medium">Configuration review needed</p>
                <p className="text-sm mt-1">Some resources require manual setup</p>
              </div>
            </div>
            <div className="alert-error">
              <XCircle className="h-5 w-5 flex-shrink-0" />
              <p>Failed to connect to Docker daemon</p>
            </div>
            <div className="alert-info">
              <Info className="h-5 w-5 flex-shrink-0" />
              <p>Credentials are never stored and used only for discovery</p>
            </div>
          </div>
        </Section>

        {/* Forms */}
        <Section title="Form Elements" id="forms">
          <div className="max-w-md space-y-4">
            <div>
              <label className="label">Email</label>
              <input
                type="email"
                className="input"
                placeholder="your@email.com"
              />
            </div>
            <div>
              <label className="label label-required">Password</label>
              <input type="password" className="input" />
            </div>
            <div>
              <label className="label">Region</label>
              <select className="select">
                <option>us-east-1</option>
                <option>eu-west-1</option>
                <option>ap-southeast-1</option>
              </select>
            </div>
            <div>
              <label className="label">Description</label>
              <textarea
                className="textarea"
                rows={3}
                placeholder="Enter description..."
              />
            </div>
            <div>
              <label className="label">Error State</label>
              <input type="text" className="input-error" />
              <p className="text-xs text-error mt-1">This field is required</p>
            </div>
          </div>
        </Section>

        {/* Migration Flow */}
        <Section title="Migration Flow" id="migration">
          <div className="migration-flow">
            <div className="migration-source">
              <Cloud className="h-5 w-5 text-cloud-aws" />
              <div>
                <div className="text-xs text-muted-foreground">From</div>
                <div className="font-medium">AWS EC2</div>
                <div className="text-xs text-muted-foreground">us-east-1</div>
              </div>
            </div>
            <ArrowRight className="migration-arrow h-5 w-5" />
            <div className="migration-target">
              <Server className="h-5 w-5 text-freedom" />
              <div>
                <div className="text-xs text-muted-foreground">To</div>
                <div className="font-medium">Docker Container</div>
                <div className="text-xs text-muted-foreground">localhost</div>
              </div>
            </div>
          </div>
        </Section>

        {/* Resource Icons */}
        <Section title="Resource Icons" id="icons">
          <div className="flex flex-wrap gap-4">
            <div>
              <div className="resource-icon-compute">
                <Server className="h-5 w-5" />
              </div>
              <p className="text-xs text-center mt-2">Compute</p>
            </div>
            <div>
              <div className="resource-icon-database">
                <Database className="h-5 w-5" />
              </div>
              <p className="text-xs text-center mt-2">Database</p>
            </div>
            <div>
              <div className="resource-icon-storage">
                <HardDrive className="h-5 w-5" />
              </div>
              <p className="text-xs text-center mt-2">Storage</p>
            </div>
            <div>
              <div className="resource-icon-network">
                <Globe className="h-5 w-5" />
              </div>
              <p className="text-xs text-center mt-2">Network</p>
            </div>
            <div>
              <div className="resource-icon-security">
                <Shield className="h-5 w-5" />
              </div>
              <p className="text-xs text-center mt-2">Security</p>
            </div>
            <div>
              <div className="resource-icon-messaging">
                <MessageSquare className="h-5 w-5" />
              </div>
              <p className="text-xs text-center mt-2">Messaging</p>
            </div>
          </div>
        </Section>

        {/* Code Blocks */}
        <Section title="Code Blocks" id="code">
          <div className="space-y-4">
            <div>
              <p className="text-sm mb-2">Inline: <code className="code-inline">docker-compose up</code></p>
            </div>
            <pre className="code-block">
              <code>{`version: '3.8'
services:
  web:
    image: nginx:alpine
    ports:
      - "80:80"`}</code>
            </pre>
            <div className="terminal">
              <div className="terminal-header">
                <div className="flex gap-1.5">
                  <div className="w-3 h-3 rounded-full bg-red-500" />
                  <div className="w-3 h-3 rounded-full bg-yellow-500" />
                  <div className="w-3 h-3 rounded-full bg-green-500" />
                </div>
                <span className="text-xs text-muted-foreground/60">bash</span>
              </div>
              <div className="terminal-body">
                <div>$ docker-compose up -d</div>
                <div className="text-green-400">Creating network "stack_default"...</div>
                <div className="text-green-400">Creating web-server... done</div>
              </div>
            </div>
          </div>
        </Section>

        {/* Table */}
        <Section title="Table" id="table">
          <div className="table-wrapper">
            <table className="table">
              <thead className="table-header">
                <tr className="table-header-row">
                  <th className="table-header-cell">Resource</th>
                  <th className="table-header-cell">Type</th>
                  <th className="table-header-cell">Region</th>
                  <th className="table-header-cell">Status</th>
                  <th className="table-header-cell">Actions</th>
                </tr>
              </thead>
              <tbody className="table-body">
                <tr className="table-row">
                  <td className="table-cell font-medium">web-server-prod</td>
                  <td className="table-cell">
                    <span className="badge-aws">EC2</span>
                  </td>
                  <td className="table-cell text-muted-foreground">us-east-1</td>
                  <td className="table-cell">
                    <div className="flex items-center gap-2">
                      <span className="status-dot-success" />
                      <span className="text-sm">Running</span>
                    </div>
                  </td>
                  <td className="table-cell">
                    <Button variant="ghost" size="icon-sm">
                      <MoreHorizontal className="h-4 w-4" />
                    </Button>
                  </td>
                </tr>
                <tr className="table-row">
                  <td className="table-cell font-medium">postgres-main</td>
                  <td className="table-cell">
                    <span className="badge-aws">RDS</span>
                  </td>
                  <td className="table-cell text-muted-foreground">us-east-1</td>
                  <td className="table-cell">
                    <div className="flex items-center gap-2">
                      <span className="status-dot-success" />
                      <span className="text-sm">Running</span>
                    </div>
                  </td>
                  <td className="table-cell">
                    <Button variant="ghost" size="icon-sm">
                      <MoreHorizontal className="h-4 w-4" />
                    </Button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </Section>

        {/* Loading States */}
        <Section title="Loading States" id="loading">
          <div className="space-y-4">
            <div className="card p-6 space-y-3">
              <div className="skeleton h-4 w-3/4" />
              <div className="skeleton h-4 w-full" />
              <div className="skeleton h-4 w-1/2" />
            </div>
            <div className="flex items-center gap-2">
              <Loader2 className="h-4 w-4 animate-spin text-primary" />
              <span className="text-sm text-muted-foreground">Loading...</span>
            </div>
          </div>
        </Section>

        {/* Empty State */}
        <Section title="Empty State" id="empty">
          <div className="empty-state">
            <Database className="empty-state-icon" />
            <h3 className="empty-state-title">No resources found</h3>
            <p className="empty-state-description">
              Upload an infrastructure file or connect to your cloud provider to
              discover resources for migration.
            </p>
            <div className="flex gap-3 mt-6">
              <Button variant="primary">
                <Upload className="h-4 w-4" />
                Upload Files
              </Button>
              <Button variant="outline">
                <Cloud className="h-4 w-4" />
                Connect Provider
              </Button>
            </div>
          </div>
        </Section>

        {/* Progress */}
        <Section title="Progress" id="progress">
          <div className="space-y-4">
            <div>
              <div className="flex justify-between text-sm mb-2">
                <span>Migration Progress</span>
                <span className="text-muted-foreground">65%</span>
              </div>
              <div className="progress">
                <div className="progress-indicator" style={{ width: '65%' }} />
              </div>
            </div>
            <div>
              <div className="flex justify-between text-sm mb-2">
                <span>Deployment</span>
                <span className="text-muted-foreground">100%</span>
              </div>
              <div className="progress">
                <div
                  className="progress-indicator bg-success"
                  style={{ width: '100%' }}
                />
              </div>
            </div>
          </div>
        </Section>
      </div>
    </div>
  );
}

// Helper Components

function Section({
  title,
  id,
  children,
}: {
  title: string;
  id: string;
  children: React.ReactNode;
}) {
  return (
    <section id={id} className="scroll-mt-20">
      <h2 className="text-2xl font-bold mb-6 border-b pb-3">{title}</h2>
      <div className="space-y-6">{children}</div>
    </section>
  );
}

function SubSection({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div>
      <h3 className="text-lg font-semibold mb-3">{title}</h3>
      {children}
    </div>
  );
}

function ColorSwatch({
  name,
  className,
}: {
  name: string;
  className: string;
}) {
  return (
    <div className="inline-block">
      <div className={`w-24 h-24 rounded-lg ${className} shadow-md`} />
      <p className="text-sm text-center mt-2">{name}</p>
    </div>
  );
}
