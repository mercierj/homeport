import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { Toaster } from 'sonner';

// Pages
import { Dashboard } from './pages/Dashboard';
import Migrate from './pages/Migrate';
import { Storage } from './pages/Storage';
import { Database } from './pages/Database';
import { LogExplorer } from './pages/LogExplorer';
import { MetricsDashboard } from './pages/MetricsDashboard';
import { Deploy } from './pages/Deploy';
import { Functions } from './pages/Functions';
import { TerminalPage } from './pages/TerminalPage';
import { Stacks } from './pages/Stacks';
import { Cache } from './pages/Cache';
import { Queues } from './pages/Queues';
import { Backup } from './pages/Backup';
import { Certificates } from './pages/Certificates';
import { Secrets } from './pages/Secrets';
import { Identity } from './pages/Identity';
import { DNS } from './pages/DNS';
import Policies from './pages/Policies';

// Components
import { Sidebar } from './components/navigation/Sidebar';

// API
import { listContainers } from './lib/docker-api';

const queryClient = new QueryClient();

function useHasContent() {
  const { data: containers, isLoading: containersLoading } = useQuery({
    queryKey: ['containers', 'default'],
    queryFn: () => listContainers('default'),
  });

  const isLoading = containersLoading;
  // Only containers (actual running infrastructure) count as "content"
  // Saved discoveries are just import state, accessible from /migrate
  const hasContainers = (containers?.containers?.length ?? 0) > 0;

  return { hasContent: hasContainers, isLoading };
}

function Layout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-screen bg-background overflow-hidden">
      <Sidebar />
      <main className="flex-1 overflow-y-auto">
        <div className="container mx-auto px-6 py-6 max-w-7xl">
          {children}
        </div>
      </main>
    </div>
  );
}

function HomeRoute() {
  const { hasContent, isLoading } = useHasContent();

  if (isLoading) {
    return <div className="text-muted-foreground">Loading...</div>;
  }

  if (!hasContent) {
    return <Navigate to="/migrate" replace />;
  }

  return <Dashboard />;
}

function AppRoutes() {
  return (
    <Layout>
      <Routes>
        {/* Dashboard */}
        <Route path="/" element={<HomeRoute />} />

        {/* Migration - Hero Feature */}
        <Route path="/migrate" element={<Migrate />} />

        {/* Compute & Functions */}
        <Route path="/deploy" element={<Deploy />} />
        <Route path="/functions" element={<Functions />} />
        <Route path="/terminal" element={<TerminalPage />} />
        <Route path="/stacks" element={<Stacks />} />

        {/* Data & Storage */}
        <Route path="/database" element={<Database />} />
        <Route path="/storage" element={<Storage />} />
        <Route path="/cache" element={<Cache />} />
        <Route path="/queues" element={<Queues />} />
        <Route path="/backup" element={<Backup />} />

        {/* Security & Identity */}
        <Route path="/certificates" element={<Certificates />} />
        <Route path="/secrets" element={<Secrets />} />
        <Route path="/identity" element={<Identity />} />
        <Route path="/policies" element={<Policies />} />

        {/* Networking */}
        <Route path="/dns" element={<DNS />} />

        {/* Observability */}
        <Route path="/logs" element={<LogExplorer />} />
        <Route path="/metrics" element={<MetricsDashboard />} />
      </Routes>
    </Layout>
  );
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <AppRoutes />
      </BrowserRouter>
      <Toaster position="bottom-right" />
    </QueryClientProvider>
  );
}
