export interface AuthStatus {
  authenticated: boolean;
  onboarding_required: boolean;
  username?: string;
}

async function authFetch<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers || {}),
    },
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
  if (response.status === 204) {
    return undefined as T;
  }
  return response.json();
}

export function fetchAuthStatus(): Promise<AuthStatus> {
  return authFetch<AuthStatus>('/api/v1/auth/me');
}

export function completeOnboarding(input: {
  token: string;
  username: string;
  password: string;
  confirm_password: string;
}): Promise<void> {
  return authFetch<void>('/auth/onboarding/complete', {
    method: 'POST',
    body: JSON.stringify(input),
  });
}

export function login(input: { username: string; password: string }): Promise<void> {
  return authFetch<void>('/auth/login', {
    method: 'POST',
    body: JSON.stringify(input),
  });
}

export function logout(): Promise<void> {
  return authFetch<void>('/auth/logout', { method: 'POST' });
}
