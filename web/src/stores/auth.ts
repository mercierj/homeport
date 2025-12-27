import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';

/**
 * Auth store for UI state only.
 *
 * SECURITY NOTE: The actual session token is stored in an HttpOnly cookie
 * managed by the backend (internal/api/handlers/auth.go). This store only
 * tracks UI state (username, isAuthenticated) for display purposes.
 *
 * The HttpOnly cookie ensures JavaScript cannot access the session token,
 * protecting against XSS attacks. This pattern works correctly on localhost
 * since cookies are still sent with requests regardless of the Secure flag.
 */
interface AuthState {
  username: string | null;
  isAuthenticated: boolean;
  login: (username: string) => void;
  logout: () => void;
  // Sync auth state with backend session
  checkSession: () => Promise<void>;
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      username: null,
      isAuthenticated: false,
      login: (username) => {
        set({ username, isAuthenticated: true });
      },
      logout: () => set({ username: null, isAuthenticated: false }),
      checkSession: async () => {
        try {
          const response = await fetch('/api/auth/me', {
            credentials: 'include', // Include HttpOnly cookies
          });
          if (response.ok) {
            const data = await response.json();
            set({ username: data.username, isAuthenticated: true });
          } else {
            set({ username: null, isAuthenticated: false });
          }
        } catch {
          set({ username: null, isAuthenticated: false });
        }
      },
    }),
    {
      name: 'auth-storage',
      storage: createJSONStorage(() => sessionStorage),
      partialize: (state) => ({
        username: state.username,
        isAuthenticated: state.isAuthenticated,
      }),
    }
  )
);
