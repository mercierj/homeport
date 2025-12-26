import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter, Routes, Route, Link } from 'react-router-dom';
import { Dashboard } from './pages/Dashboard';
import { Migrate } from './pages/Migrate';
import { Storage } from './pages/Storage';
import { Database } from './pages/Database';

const queryClient = new QueryClient();

function Layout({ children }: { children: React.ReactNode }) {
  return (
    <div className="min-h-screen bg-background">
      <nav className="border-b">
        <div className="container mx-auto px-4 py-3 flex items-center gap-6">
          <Link to="/" className="font-bold text-xl">AgnosTech</Link>
          <Link to="/" className="text-muted-foreground hover:text-foreground">Dashboard</Link>
          <Link to="/migrate" className="text-muted-foreground hover:text-foreground">Migrate</Link>
          <Link to="/storage" className="text-muted-foreground hover:text-foreground">Storage</Link>
          <Link to="/database" className="text-muted-foreground hover:text-foreground">Database</Link>
        </div>
      </nav>
      <main className="container mx-auto px-4 py-6">
        {children}
      </main>
    </div>
  );
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Layout>
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route path="/migrate" element={<Migrate />} />
            <Route path="/storage" element={<Storage />} />
            <Route path="/database" element={<Database />} />
          </Routes>
        </Layout>
      </BrowserRouter>
    </QueryClientProvider>
  );
}
