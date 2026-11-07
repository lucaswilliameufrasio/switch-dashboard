import { get } from 'svelte/store';
import { authState, switchesStore, configStore } from './stores.js';

export async function apiFetch(path, options = {}) {
	const state = get(authState);
	const headers = new Headers(options.headers || {});

	if (options.body && !(options.body instanceof FormData) && !headers.has('Content-Type')) {
		headers.set('Content-Type', 'application/json');
	}
	if (state.csrfToken && options.method && options.method !== 'GET') {
		headers.set('X-CSRF-Token', state.csrfToken);
	}

	const res = await fetch(path, {
		...options,
		headers,
		credentials: 'same-origin'
	});

	if (res.status === 401 && !path.startsWith('/api/auth/')) {
		authState.update((s) => ({ ...s, authenticated: false, csrfToken: '' }));
		throw new Error('Session expired. Please sign in again.');
	}

	const contentType = res.headers.get('content-type') || '';
	if (contentType.includes('application/json')) {
		const data = await res.json();
		if (!res.ok) {
			throw new Error(data.error || data.message || `HTTP ${res.status}`);
		}
		return data;
	}
	if (!res.ok) {
		throw new Error(`HTTP ${res.status}`);
	}
	return res;
}

export async function checkAuthStatus() {
	try {
		const data = await apiFetch('/api/auth/status');
		authState.set({
			loading: false,
			setupRequired: !!data.setup_required,
			authenticated: !!data.authenticated,
			username: data.username || 'admin',
			csrfToken: data.csrf_token || '',
			version: data.version || '2.1.0',
			title: data.title || 'Switch Dashboard'
		});
		return data;
	} catch {
		authState.update((s) => ({ ...s, loading: false }));
		return null;
	}
}

export async function refreshDashboardData() {
	try {
		const [switches, cfg] = await Promise.all([
			apiFetch('/api/switches'),
			apiFetch('/api/config')
		]);
		switchesStore.set(switches || []);
		configStore.set(cfg || null);
		if (cfg && cfg.title) {
			authState.update((s) => ({ ...s, title: cfg.title }));
		}
	} catch (e) {
		console.error('Failed to refresh dashboard data:', e);
	}
}
