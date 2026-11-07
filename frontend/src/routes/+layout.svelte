<script>
	import '../app.css';
	import { onMount } from 'svelte';
	import { page } from '$app/stores';
	import { authState, switchesStore, toastStore, showToast, formatBps } from '$lib/stores.js';
	import { apiFetch, checkAuthStatus, refreshDashboardData } from '$lib/api.js';

	let { children } = $props();

	let usernameInput = $state('admin');
	let passwordInput = $state('');
	let confirmPasswordInput = $state('');
	let authError = $state('');
	let submittingAuth = $state(false);
	let syncing = $state(false);

	onMount(() => {
		let timer;
		(async () => {
			const st = await checkAuthStatus();
			if (st && st.authenticated) {
				await refreshDashboardData();
			}
			timer = setInterval(async () => {
				if ($authState.authenticated) {
					await refreshDashboardData();
				}
			}, 15000);
		})();
		return () => {
			if (timer) clearInterval(timer);
		};
	});

	async function handleSetup(e) {
		e.preventDefault();
		authError = '';
		if (passwordInput.length < 8) {
			authError = 'Password must be at least 8 characters.';
			return;
		}
		if (passwordInput !== confirmPasswordInput) {
			authError = 'Passwords do not match.';
			return;
		}
		submittingAuth = true;
		try {
			const res = await apiFetch('/api/auth/setup', {
				method: 'POST',
				body: JSON.stringify({ username: usernameInput, password: passwordInput })
			});
			authState.update((s) => ({
				...s,
				setupRequired: false,
				authenticated: true,
				username: res.username,
				csrfToken: res.csrf_token
			}));
			passwordInput = '';
			confirmPasswordInput = '';
			await refreshDashboardData();
			showToast('UniFi Switch OS Admin account created.', 'success');
		} catch (err) {
			authError = err.message;
		} finally {
			submittingAuth = false;
		}
	}

	async function handleLogin(e) {
		e.preventDefault();
		authError = '';
		submittingAuth = true;
		try {
			const res = await apiFetch('/api/auth/login', {
				method: 'POST',
				body: JSON.stringify({ username: usernameInput, password: passwordInput })
			});
			authState.update((s) => ({
				...s,
				authenticated: true,
				username: res.username,
				csrfToken: res.csrf_token
			}));
			passwordInput = '';
			await refreshDashboardData();
		} catch (err) {
			authError = err.message;
		} finally {
			submittingAuth = false;
		}
	}

	async function handleLogout() {
		try {
			await apiFetch('/api/auth/logout', { method: 'POST' });
		} catch {
			// ignore
		}
		authState.update((s) => ({ ...s, authenticated: false, csrfToken: '' }));
	}

	async function manualSync() {
		syncing = true;
		await refreshDashboardData();
		syncing = false;
		showToast('Telemetry synchronized.', 'info');
	}

	let totalSwitches = $derived($switchesStore.length);
	let onlineSwitches = $derived($switchesStore.filter((s) => !s.error).length);
	let activePorts = $derived(
		$switchesStore.reduce(
			(acc, sw) => acc + (sw.ports || []).filter((p) => p.status === 'up').length,
			0
		)
	);
	let totalTxBps = $derived(
		$switchesStore.reduce(
			(acc, sw) => acc + (sw.ports || []).reduce((pa, p) => pa + (p.speed_tx_bps || 0), 0),
			0
		)
	);
	let totalRxBps = $derived(
		$switchesStore.reduce(
			(acc, sw) => acc + (sw.ports || []).reduce((pa, p) => pa + (p.speed_rx_bps || 0), 0),
			0
		)
	);

	const navItems = [
		{
			href: '/',
			label: 'UniFi Devices',
			icon: 'M4 6h16a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2zm2 4v4h2v-4H6zm4 0v4h2v-4h-2zm4 0v4h2v-4h-2zm4 1a1 1 0 1 0 0 2 1 1 0 0 0 0-2z'
		},
		{
			href: '/topology',
			label: 'Topology',
			icon: 'M12 3a3 3 0 1 0 0 6 3 3 0 0 0 0-6zm-7 12a3 3 0 1 0 0 6 3 3 0 0 0 0-6zm14 0a3 3 0 1 0 0 6 3 3 0 0 0 0-6zM11 8.83V12l-4.2 3.15 1.2 1.6L12 13.75l4 3 1.2-1.6L13 12V8.83h-2z'
		},
		{
			href: '/clients',
			label: 'Client Devices',
			icon: 'M4 5a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V5zm2 0v9h12V5H6zm-4 13h20v2H2v-2z'
		},
		{
			href: '/backups',
			label: 'Config Vault',
			icon: 'M19 3H5c-1.1 0-2 .9-2 2v14a2 2 0 0 0 2 2h14c1.1 0 2-.9 2-2V7l-4-4zm-7 16a3 3 0 1 1 0-6 3 3 0 0 1 0 6zm3-10H5V5h10v4z'
		},
		{
			href: '/logs',
			label: 'System Logs',
			icon: 'M4 4h16a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2zm2 4v2h12V8H6zm0 4v2h12v-2H6zm0 4v2h8v-2H6z'
		},
		{
			href: '/settings',
			label: 'Settings',
			icon: 'M19.14 12.94c.04-.3.06-.61.06-.94 0-.32-.02-.64-.07-.94l2.03-1.58a.49.49 0 0 0 .12-.61l-1.92-3.32a.49.49 0 0 0-.59-.22l-2.39.96c-.5-.38-1.03-.7-1.62-.94l-.36-2.54a.484.484 0 0 0-.48-.41h-3.84c-.24 0-.43.17-.47.41l-.36 2.54c-.59.24-1.13.57-1.62.94l-2.39-.96c-.22-.08-.47 0-.59.22L2.74 8.87c-.12.21-.08.47.12.61l2.03 1.58c-.05.3-.09.63-.09.94s.02.64.07.94l-2.03 1.58a.49.49 0 0 0-.12.61l1.92 3.32c.12.22.37.29.59.22l2.39-.96c.5.38 1.03.7 1.62.94l.36 2.54c.05.24.24.41.48.41h3.84c.24 0 .44-.17.47-.41l.36-2.54c.59-.24 1.13-.56 1.62-.94l2.39.96c.22.08.47 0 .59-.22l1.92-3.32c.12-.22.07-.47-.12-.61l-2.01-1.58zM12 15.6A3.6 3.6 0 1 1 12 8.4a3.6 3.6 0 0 1 0 7.2z'
		}
	];
</script>

<svelte:head>
	<title>{$authState.title || 'UniFi Switch OS'}</title>
</svelte:head>

{#if $authState.loading}
	<div class="auth-screen">
		<div class="auth-card">
			<div class="unifi-ring"></div>
			<p style="color: var(--text-secondary); margin-top: 14px;">Initializing UniFi Switch OS...</p>
		</div>
	</div>
{:else if $authState.setupRequired}
	<div class="auth-screen">
		<div class="auth-card">
			<div class="auth-brand">
				<div class="brand-orb"></div>
				<div>
					<h1>UniFi Switch OS</h1>
					<p>Initial Security & Administrator Setup</p>
				</div>
			</div>
			<div class="setup-notice">
				Switch passwords are encrypted at rest with <strong>AES-256-GCM</strong>. Create your local administrator account to secure this console.
			</div>
			{#if authError}
				<div class="auth-alert">{authError}</div>
			{/if}
			<form onsubmit={handleSetup} class="auth-form">
				<label>
					<span>Administrator Username</span>
					<input type="text" bind:value={usernameInput} required autocomplete="username" />
				</label>
				<label>
					<span>New Password (min. 8 characters)</span>
					<input type="password" bind:value={passwordInput} required autocomplete="new-password" />
				</label>
				<label>
					<span>Confirm Password</span>
					<input type="password" bind:value={confirmPasswordInput} required autocomplete="new-password" />
				</label>
				<button type="submit" class="unifi-btn unifi-btn-primary" style="width:100%; padding: 10px;" disabled={submittingAuth}>
					{submittingAuth ? 'Configuring Console...' : 'Complete Console Setup'}
				</button>
			</form>
		</div>
	</div>
{:else if !$authState.authenticated}
	<div class="auth-screen">
		<div class="auth-card">
			<div class="auth-brand">
				<div class="brand-orb"></div>
				<div>
					<h1>{$authState.title || 'UniFi Switch OS'}</h1>
					<p>Network Management Console</p>
				</div>
			</div>
			{#if authError}
				<div class="auth-alert">{authError}</div>
			{/if}
			<form onsubmit={handleLogin} class="auth-form">
				<label>
					<span>Username</span>
					<input type="text" bind:value={usernameInput} required autocomplete="username" />
				</label>
				<label>
					<span>Password</span>
					<input type="password" bind:value={passwordInput} required autocomplete="current-password" />
				</label>
				<button type="submit" class="unifi-btn unifi-btn-primary" style="width:100%; padding: 10px;" disabled={submittingAuth}>
					{submittingAuth ? 'Signing In...' : 'Sign In to Console'}
				</button>
			</form>
			<div class="auth-footer">
				Zero Telemetry &middot; AES-256-GCM Vault &middot; v{$authState.version}
			</div>
		</div>
	</div>
{:else}
	<div class="os-shell">
		<!-- Left UniFi OS Navigation Rail -->
		<aside class="os-rail">
			<a href="/" class="rail-logo" title="UniFi Switch OS">
				<div class="brand-orb-sm"></div>
			</a>
			<nav class="rail-nav">
				{#each navItems as item}
					{@const active = $page.url.pathname === item.href}
					<a href={item.href} class="rail-item" class:active title={item.label}>
						<svg viewBox="0 0 24 24" width="20" height="20" fill="currentColor">
							<path d={item.icon} />
						</svg>
						<span class="rail-label">{item.label}</span>
					</a>
				{/each}
			</nav>
			<div class="rail-bottom">
				<button class="rail-item" onclick={handleLogout} title="Sign Out ({$authState.username})">
					<svg viewBox="0 0 24 24" width="19" height="19" fill="currentColor">
						<path d="M16 13v-2H7V8l-5 4 5 4v-3h9zM20 3h-9c-1.1 0-2 .9-2 2v4h2V5h9v14h-9v-4H9v4c0 1.1.9 2 2 2h9c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2z" />
					</svg>
					<span class="rail-label">Sign Out</span>
				</button>
			</div>
		</aside>

		<!-- Main Content Column -->
		<div class="os-main">
			<!-- Top UniFi OS Bar -->
			<header class="os-topbar">
				<div class="topbar-left">
					<span class="console-name">{$authState.title || 'UniFi Switch OS'}</span>
					<span class="unifi-badge unifi-badge-online">
						<span class="pulse-dot"></span>
						{onlineSwitches}/{totalSwitches} Switches Online
					</span>
					<span class="unifi-badge unifi-badge-blue">{activePorts} Active Ports</span>
				</div>
				<div class="topbar-right">
					<div class="throughput-pill mono">
						<span style="color: var(--status-10g);">▲ TX {formatBps(totalTxBps)}</span>
						<span style="color: var(--border-strong);">|</span>
						<span style="color: var(--status-1g);">▼ RX {formatBps(totalRxBps)}</span>
					</div>
					<button class="unifi-btn unifi-btn-secondary" onclick={manualSync} disabled={syncing} title="Synchronize Now">
						<svg viewBox="0 0 24 24" width="14" height="14" fill="currentColor" class:spin={syncing}>
							<path d="M17.65 6.35A7.958 7.958 0 0 0 12 4c-4.42 0-7.99 3.58-7.99 8s3.57 8 7.99 8c3.73 0 6.84-2.55 7.73-6h-2.08A5.99 5.99 0 0 1 12 18c-3.31 0-6-2.69-6-6s2.69-6 6-6c1.66 0 3.14.69 4.22 1.78L13 11h7V4l-2.35 2.35z" />
						</svg>
						Sync
					</button>
					<div class="admin-chip">
						<span class="admin-dot"></span>
						<span>{$authState.username}</span>
					</div>
				</div>
			</header>

			{#if $toastStore}
				<div class="toast-banner" class:toast-error={$toastStore.type === 'error'} class:toast-success={$toastStore.type === 'success'}>
					{$toastStore.message}
				</div>
			{/if}

			<main class="os-content">
				{@render children()}
			</main>
		</div>
	</div>
{/if}

<style>
	.auth-screen {
		min-height: 100vh;
		display: flex;
		align-items: center;
		justify-content: center;
		background: radial-gradient(circle at 50% 20%, #131c31 0%, #0b0d12 70%);
		padding: 20px;
	}
	.auth-card {
		background: var(--bg-surface);
		border: 1px solid var(--border-strong);
		border-radius: 14px;
		padding: 32px;
		width: 100%;
		max-width: 410px;
		box-shadow: 0 24px 64px rgba(0, 0, 0, 0.55);
	}
	.auth-brand {
		display: flex;
		align-items: center;
		gap: 14px;
		margin-bottom: 24px;
	}
	.auth-brand h1 {
		font-size: 19px;
		font-weight: 600;
	}
	.auth-brand p {
		font-size: 12px;
		color: var(--text-secondary);
	}
	.brand-orb {
		width: 40px;
		height: 40px;
		border-radius: 10px;
		background: linear-gradient(135deg, #006fff 0%, #00c6ff 100%);
		box-shadow: 0 0 20px rgba(0, 111, 255, 0.45);
		flex-shrink: 0;
	}
	.brand-orb-sm {
		width: 30px;
		height: 30px;
		border-radius: 8px;
		background: linear-gradient(135deg, #006fff 0%, #00c6ff 100%);
		box-shadow: 0 0 14px rgba(0, 111, 255, 0.4);
	}
	.setup-notice {
		background: rgba(0, 111, 255, 0.1);
		border: 1px solid rgba(0, 111, 255, 0.28);
		border-radius: 8px;
		padding: 10px 12px;
		font-size: 12px;
		color: #93c5fd;
		margin-bottom: 16px;
	}
	.auth-alert {
		background: rgba(239, 68, 68, 0.14);
		border: 1px solid rgba(239, 68, 68, 0.35);
		color: #fca5a5;
		padding: 10px 12px;
		border-radius: 8px;
		font-size: 12px;
		margin-bottom: 16px;
	}
	.auth-form {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	.auth-form label {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	.auth-form label span {
		font-size: 12px;
		color: var(--text-secondary);
		font-weight: 500;
	}
	.auth-footer {
		margin-top: 22px;
		padding-top: 14px;
		border-top: 1px solid var(--border-subtle);
		text-align: center;
		font-size: 11px;
		color: var(--text-muted);
	}
	.os-shell {
		display: flex;
		min-height: 100vh;
	}
	.os-rail {
		width: 210px;
		background: var(--bg-surface);
		border-right: 1px solid var(--border-subtle);
		display: flex;
		flex-direction: column;
		padding: 14px 10px;
		position: sticky;
		top: 0;
		height: 100vh;
		flex-shrink: 0;
	}
	@media (max-width: 900px) {
		.os-rail {
			width: 64px;
		}
		.rail-label {
			display: none;
		}
	}
	.rail-logo {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 4px 10px 18px 10px;
		border-bottom: 1px solid var(--border-subtle);
		margin-bottom: 12px;
		text-decoration: none;
	}
	.rail-nav {
		display: flex;
		flex-direction: column;
		gap: 4px;
		flex: 1;
	}
	.rail-item {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 10px 12px;
		border-radius: 8px;
		color: var(--text-secondary);
		text-decoration: none;
		font-weight: 500;
		font-size: 13px;
		transition: all 0.15s ease;
		position: relative;
	}
	.rail-item:hover {
		background: var(--bg-elevated);
		color: var(--text-primary);
	}
	.rail-item.active {
		background: rgba(0, 111, 255, 0.14);
		color: #fff;
	}
	.rail-item.active::before {
		content: '';
		position: absolute;
		left: 0;
		top: 6px;
		bottom: 6px;
		width: 3px;
		border-radius: 0 3px 3px 0;
		background: var(--unifi-blue);
	}
	.rail-bottom {
		padding-top: 10px;
		border-top: 1px solid var(--border-subtle);
	}
	.os-main {
		flex: 1;
		display: flex;
		flex-direction: column;
		min-width: 0;
	}
	.os-topbar {
		height: 56px;
		background: rgba(18, 22, 31, 0.85);
		backdrop-filter: blur(10px);
		border-bottom: 1px solid var(--border-subtle);
		padding: 0 24px;
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 16px;
		position: sticky;
		top: 0;
		z-index: 50;
	}
	.topbar-left, .topbar-right {
		display: flex;
		align-items: center;
		gap: 12px;
		flex-wrap: wrap;
	}
	.console-name {
		font-size: 15px;
		font-weight: 600;
		color: var(--text-primary);
	}
	.pulse-dot {
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: var(--success);
		box-shadow: 0 0 6px var(--success);
	}
	.throughput-pill {
		background: var(--bg-elevated);
		border: 1px solid var(--border-subtle);
		padding: 5px 12px;
		border-radius: 6px;
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 11.5px;
	}
	.admin-chip {
		display: flex;
		align-items: center;
		gap: 6px;
		padding: 5px 10px;
		border-radius: 6px;
		background: var(--bg-elevated);
		border: 1px solid var(--border-subtle);
		font-size: 12px;
		color: var(--text-secondary);
	}
	.admin-dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: var(--unifi-blue);
	}
	.os-content {
		padding: 24px;
		flex: 1;
	}
	.toast-banner {
		margin: 12px 24px 0 24px;
		padding: 10px 16px;
		border-radius: 8px;
		background: rgba(0, 111, 255, 0.16);
		border: 1px solid rgba(0, 111, 255, 0.35);
		color: #93c5fd;
		font-size: 12.5px;
	}
	.toast-banner.toast-error {
		background: rgba(239, 68, 68, 0.16);
		border-color: rgba(239, 68, 68, 0.4);
		color: #fca5a5;
	}
	.toast-banner.toast-success {
		background: rgba(0, 214, 143, 0.14);
		border-color: rgba(0, 214, 143, 0.35);
		color: #6ee7b7;
	}
	.spin {
		animation: spin 1s linear infinite;
	}
	@keyframes spin {
		to { transform: rotate(360deg); }
	}
</style>
