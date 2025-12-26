import { create } from 'zustand';

interface StorageCredentials {
  endpoint: string;
  accessKey: string;
  secretKey: string;
}

interface DatabaseCredentials {
  host: string;
  port: number;
  user: string;
  password: string;
  database: string;
}

interface CredentialsStore {
  storage: StorageCredentials | null;
  database: DatabaseCredentials | null;
  setStorageCredentials: (creds: StorageCredentials) => void;
  setDatabaseCredentials: (creds: DatabaseCredentials) => void;
  clearStorageCredentials: () => void;
  clearDatabaseCredentials: () => void;
}

export const useCredentialsStore = create<CredentialsStore>((set) => ({
  storage: null,
  database: null,
  setStorageCredentials: (creds) => set({ storage: creds }),
  setDatabaseCredentials: (creds) => set({ database: creds }),
  clearStorageCredentials: () => set({ storage: null }),
  clearDatabaseCredentials: () => set({ database: null }),
}));
