import { fetchAPI } from './api';

// User types
export interface User {
  id: string;
  username: string;
  email: string;
  roles: string[];
  last_login?: string;
  created_at: string;
  updated_at: string;
  enabled: boolean;
}

export interface UsersResponse {
  users: User[];
  count: number;
}

export interface CreateUserRequest {
  username: string;
  email: string;
  password: string;
  roles: string[];
  enabled?: boolean;
}

export interface UpdateUserRequest {
  username?: string;
  email?: string;
  password?: string;
  roles?: string[];
  enabled?: boolean;
}

// Role types
export interface Role {
  id: string;
  name: string;
  description: string;
  permissions: string[];
  created_at: string;
  updated_at: string;
}

export interface RolesResponse {
  roles: Role[];
  count: number;
}

export interface CreateRoleRequest {
  name: string;
  description: string;
  permissions: string[];
}

export interface UpdateRoleRequest {
  name?: string;
  description?: string;
  permissions?: string[];
}

// Available permissions (for role creation)
export interface PermissionsResponse {
  permissions: string[];
}

// User API functions
export async function listUsers(): Promise<UsersResponse> {
  return fetchAPI<UsersResponse>('/identity/users');
}

export async function getUser(id: string): Promise<User> {
  return fetchAPI<User>(`/identity/users/${encodeURIComponent(id)}`);
}

export async function createUser(req: CreateUserRequest): Promise<User> {
  return fetchAPI<User>('/identity/users', {
    method: 'POST',
    body: JSON.stringify(req),
  });
}

export async function updateUser(id: string, req: UpdateUserRequest): Promise<User> {
  return fetchAPI<User>(`/identity/users/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(req),
  });
}

export async function deleteUser(id: string): Promise<{ status: string; id: string }> {
  return fetchAPI<{ status: string; id: string }>(`/identity/users/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
}

// Role API functions
export async function listRoles(): Promise<RolesResponse> {
  return fetchAPI<RolesResponse>('/identity/roles');
}

export async function getRole(id: string): Promise<Role> {
  return fetchAPI<Role>(`/identity/roles/${encodeURIComponent(id)}`);
}

export async function createRole(req: CreateRoleRequest): Promise<Role> {
  return fetchAPI<Role>('/identity/roles', {
    method: 'POST',
    body: JSON.stringify(req),
  });
}

export async function updateRole(id: string, req: UpdateRoleRequest): Promise<Role> {
  return fetchAPI<Role>(`/identity/roles/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(req),
  });
}

export async function deleteRole(id: string): Promise<{ status: string; id: string }> {
  return fetchAPI<{ status: string; id: string }>(`/identity/roles/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
}

// Get available permissions for role assignment
export async function listPermissions(): Promise<PermissionsResponse> {
  return fetchAPI<PermissionsResponse>('/identity/permissions');
}

// Role assignment
export async function assignRole(userId: string, roleId: string): Promise<{ status: string }> {
  return fetchAPI<{ status: string }>(
    `/identity/users/${encodeURIComponent(userId)}/roles/${encodeURIComponent(roleId)}`,
    { method: 'PUT' }
  );
}

export async function removeRole(userId: string, roleId: string): Promise<{ status: string }> {
  return fetchAPI<{ status: string }>(
    `/identity/users/${encodeURIComponent(userId)}/roles/${encodeURIComponent(roleId)}`,
    { method: 'DELETE' }
  );
}

// Auth types
export interface LoginRequest {
  username: string;
  password: string;
}

export interface LoginResponse {
  token: string;
  user: User;
  expires_at: string;
}

// Auth functions
export async function login(req: LoginRequest): Promise<LoginResponse> {
  return fetchAPI<LoginResponse>('/identity/login', {
    method: 'POST',
    body: JSON.stringify(req),
  });
}

export async function logout(): Promise<{ status: string }> {
  return fetchAPI<{ status: string }>('/identity/logout', {
    method: 'POST',
  });
}

export async function getCurrentUser(): Promise<User> {
  return fetchAPI<User>('/identity/me');
}

// Helper function for user status badge
export function getUserStatusBadgeClass(enabled: boolean): string {
  return enabled
    ? 'bg-green-100 text-green-800'
    : 'bg-gray-100 text-gray-800';
}

// Helper function to format last login
export function formatLastLogin(lastLogin?: string): string {
  if (!lastLogin) return 'Never';
  const date = new Date(lastLogin);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMins = Math.floor(diffMs / (1000 * 60));
  const diffHours = Math.floor(diffMs / (1000 * 60 * 60));
  const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));

  if (diffMins < 1) return 'Just now';
  if (diffMins < 60) return `${diffMins}m ago`;
  if (diffHours < 24) return `${diffHours}h ago`;
  if (diffDays < 7) return `${diffDays}d ago`;
  return date.toLocaleDateString();
}

// Helper: Get permission badge class
export function getPermissionBadgeClass(permission: string): string {
  switch (permission) {
    case 'admin':
      return 'bg-red-100 text-red-800';
    case 'write':
    case 'delete':
      return 'bg-yellow-100 text-yellow-800';
    case 'deploy':
    case 'migrate':
      return 'bg-blue-100 text-blue-800';
    default:
      return 'bg-gray-100 text-gray-800';
  }
}
