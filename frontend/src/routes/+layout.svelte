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
	let hydrated = $state(false);

	onMount(() => {
		hydrated = true;
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
			authError = 'A senha deve ter pelo menos 8 caracteres.';
			return;
		}
		if (passwordInput !== confirmPasswordInput) {
			authError = 'As senhas informadas não coincidem.';
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
			showToast('Conta de administrador configurada com sucesso.', 'success');
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
		showToast('Telemetria sincronizada.', 'success');
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
			label: 'Switches & Portas',
			testId: 'nav-switches',
			icon: 'M4 6h16a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2zm2 4v4h2v-4H6zm4 0v4h2v-4h-2zm4 0v4h2v-4h-2zm4 1a1 1 0 1 0 0 2 1 1 0 0 0 0-2z'
		},
		{
			href: '/topology',
			label: 'Topologia L2',
			testId: 'nav-topology',
			icon: 'M12 3a3 3 0 1 0 0 6 3 3 0 0 0 0-6zm-7 12a3 3 0 1 0 0 6 3 3 0 0 0 0-6zm14 0a3 3 0 1 0 0 6 3 3 0 0 0 0-6zM11 8.83V12l-4.2 3.15 1.2 1.6L12 13.75l4 3 1.2-1.6L13 12V8.83h-2z'
		},
		{
			href: '/clients',
			label: 'Clientes FDB',
			testId: 'nav-clients',
			icon: 'M4 5a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V5zm2 0v9h12V5H6zm-4 13h20v2H2v-2z'
		},
		{
			href: '/backups',
			label: 'Backups',
			testId: 'nav-backups',
			icon: 'M19 3H5c-1.1 0-2 .9-2 2v14a2 2 0 0 0 2 2h14c1.1 0 2-.9 2-2V7l-4-4zm-7 16a3 3 0 1 1 0-6 3 3 0 0 1 0 6zm3-10H5V5h10v4z'
		},
		{
			href: '/logs',
			label: 'Logs',
			testId: 'nav-logs',
			icon: 'M4 4h16a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2zm2 4v2h12V8H6zm0 4v2h12v-2H6zm0 4v2h8v-2H6z'
		},
		{
			href: '/settings',
			label: 'Configurações',
			testId: 'nav-settings',
			icon: 'M19.14 12.94c.04-.3.06-.61.06-.94 0-.32-.02-.64-.07-.94l2.03-1.58a.49.49 0 0 0 .12-.61l-1.92-3.32a.49.49 0 0 0-.59-.22l-2.39.96c-.5-.38-1.03-.7-1.62-.94l-.36-2.54a.484.484 0 0 0-.48-.41h-3.84c-.24 0-.43.17-.47.41l-.36 2.54c-.59.24-1.13.57-1.62.94l-2.39-.96c-.22-.08-.47 0-.59.22L2.74 8.87c-.12.21-.08.47.12.61l2.03 1.58c-.05.3-.09.63-.09.94s.02.64.07.94l-2.03 1.58a.49.49 0 0 0-.12.61l1.92 3.32c.12.22.37.29.59.22l2.39-.96c.5.38 1.03.7 1.62.94l.36 2.54c.05.24.24.41.48.41h3.84c.24 0 .44-.17.47-.41l.36-2.54c.59-.24 1.13-.56 1.62-.94l2.39.96c.22.08.47 0 .59-.22l1.92-3.32c.12-.22.07-.47-.12-.61l-2.01-1.58zM12 15.6A3.6 3.6 0 1 1 12 8.4a3.6 3.6 0 0 1 0 7.2z'
		}
	];
</script>

<svelte:head>
	<title>{$authState.title || 'Switch Dashboard'}</title>
</svelte:head>

<div data-hydrated={hydrated ? 'true' : 'false'}>
	{#if $authState.loading}
		<div class="auth-screen" data-testid="auth-loading">
			<div class="auth-card" style="text-align: center;">
				<p style="color: var(--text-secondary);">Carregando Switch Dashboard...</p>
			</div>
		</div>
	{:else if $authState.setupRequired}
		<div class="auth-screen" data-testid="auth-setup-screen">
			<div class="auth-card">
				<div class="auth-brand">
					<div class="rail-brand-mark">
						<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
							<rect x="2" y="6" width="20" height="12" rx="2" />
							<path d="M6 12h2M11 12h2M16 12h2" />
						</svg>
					</div>
					<div>
						<h1 class="page-title">Switch Dashboard</h1>
						<p class="page-subtitle">Configuração Inicial de Segurança</p>
					</div>
				</div>

				<div class="auth-notice">
					As senhas dos switches são protegidas em repouso com <strong>AES-256-GCM</strong>. Defina a conta de administrador local para liberar o console.
				</div>

				{#if authError}
					<div class="auth-alert" role="alert">{authError}</div>
				{/if}

				<form onsubmit={handleSetup} class="auth-form" data-testid="setup-form">
					<label class="field-group">
						<span class="field-label">Usuário Administrador</span>
						<input
							type="text"
							class="input"
							bind:value={usernameInput}
							required
							autocomplete="username"
							data-testid="setup-username"
						/>
					</label>

					<label class="field-group">
						<span class="field-label">Nova Senha (mín. 8 caracteres)</span>
						<input
							type="password"
							class="input"
							bind:value={passwordInput}
							required
							minlength="8"
							autocomplete="new-password"
							data-testid="setup-password"
						/>
					</label>

					<label class="field-group">
						<span class="field-label">Confirmar Senha</span>
						<input
							type="password"
							class="input"
							bind:value={confirmPasswordInput}
							required
							minlength="8"
							autocomplete="new-password"
							data-testid="setup-confirm-password"
						/>
					</label>

					<button
						type="submit"
						class="btn btn-primary"
						style="width: 100%; padding: 9px;"
						disabled={submittingAuth}
						data-testid="setup-submit"
					>
						{submittingAuth ? 'Configurando...' : 'Concluir Configuração Inicial'}
					</button>
				</form>
			</div>
		</div>
	{:else if !$authState.authenticated}
		<div class="auth-screen" data-testid="auth-login-screen">
			<div class="auth-card">
				<div class="auth-brand">
					<div class="rail-brand-mark">
						<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
							<rect x="2" y="6" width="20" height="12" rx="2" />
							<path d="M6 12h2M11 12h2M16 12h2" />
						</svg>
					</div>
					<div>
						<h1 class="page-title">{$authState.title || 'Switch Dashboard'}</h1>
						<p class="page-subtitle">Console de Operações L2</p>
					</div>
				</div>

				{#if authError}
					<div class="auth-alert" role="alert">{authError}</div>
				{/if}

				<form onsubmit={handleLogin} class="auth-form" data-testid="login-form">
					<label class="field-group">
						<span class="field-label">Usuário</span>
						<input
							type="text"
							class="input"
							bind:value={usernameInput}
							required
							autocomplete="username"
							data-testid="login-username"
						/>
					</label>

					<label class="field-group">
						<span class="field-label">Senha</span>
						<input
							type="password"
							class="input"
							bind:value={passwordInput}
							required
							autocomplete="current-password"
							data-testid="login-password"
						/>
					</label>

					<button
						type="submit"
						class="btn btn-primary"
						style="width: 100%; padding: 9px;"
						disabled={submittingAuth}
						data-testid="login-submit"
					>
						{submittingAuth ? 'Autenticando...' : 'Entrar no Console'}
					</button>
				</form>

				<div class="auth-footer">
					Zero Telemetria &middot; Cofre AES-256-GCM &middot; v{$authState.version || '2.1.0'}
				</div>
			</div>
		</div>
	{:else}
		<div class="app-shell" data-testid="app-shell">
			<aside class="nav-rail" aria-label="Navegação Principal">
				<a href="/" class="rail-brand" title="Switch Dashboard">
					<div class="rail-brand-mark">
						<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
							<rect x="2" y="6" width="20" height="12" rx="2" />
							<path d="M6 12h2M11 12h2M16 12h2" />
						</svg>
					</div>
					<div class="rail-brand-text">
						<span class="rail-brand-title">{$authState.title || 'Switch Dashboard'}</span>
						<span class="rail-brand-sub">Horaco &middot; KeepLink &middot; RTL</span>
					</div>
				</a>

				<nav class="rail-nav">
					{#each navItems as item}
						{@const active = $page.url.pathname === item.href}
						<a
							href={item.href}
							class="rail-link"
							class:active
							title={item.label}
							data-testid={item.testId}
						>
							<svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
								<path d={item.icon} />
							</svg>
							<span class="rail-link-text">{item.label}</span>
						</a>
					{/each}
				</nav>

				<div class="rail-footer">
					<button
						type="button"
						class="rail-link"
						onclick={handleLogout}
						title="Encerrar Sessão ({$authState.username})"
						data-testid="logout-btn"
					>
						<svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
							<path d="M16 13v-2H7V8l-5 4 5 4v-3h9zM20 3h-9c-1.1 0-2 .9-2 2v4h2V5h9v14h-9v-4H9v4c0 1.1.9 2 2 2h9c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2z" />
						</svg>
						<span class="rail-link-text">Sair</span>
					</button>
				</div>
			</aside>

			<div class="shell-main">
				<header class="top-bar">
					<div class="top-bar-group">
						<span class="top-bar-title" data-testid="console-title">{$authState.title || 'Switch Dashboard'}</span>
						<span class="badge badge-online mono" data-testid="switches-online-badge">
							<span class="status-dot dot-online"></span>
							{onlineSwitches}/{totalSwitches} Switches Online
						</span>
						<span class="badge badge-accent mono" data-testid="active-ports-badge">
							{activePorts} Portas Ativas
						</span>
					</div>

					<div class="top-bar-group">
						<div class="throughput-pill mono" data-testid="aggregate-throughput">
							<span style="color: var(--port-10g);">▲ TX {formatBps(totalTxBps)}</span>
							<span style="color: var(--border-strong);">|</span>
							<span style="color: var(--status-online);">▼ RX {formatBps(totalRxBps)}</span>
						</div>

						<button
							type="button"
							class="btn btn-secondary btn-sm"
							onclick={manualSync}
							disabled={syncing}
							title="Sincronizar telemetria agora"
							data-testid="sync-btn"
						>
							<svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
								<path d="M17.65 6.35A7.958 7.958 0 0 0 12 4c-4.42 0-7.99 3.58-7.99 8s3.57 8 7.99 8c3.73 0 6.84-2.55 7.73-6h-2.08A5.99 5.99 0 0 1 12 18c-3.31 0-6-2.69-6-6s2.69-6 6-6c1.66 0 3.14.69 4.22 1.78L13 11h7V4l-2.35 2.35z" />
							</svg>
							<span>{syncing ? 'Sincronizando...' : 'Sincronizar'}</span>
						</button>

						<div class="user-pill" data-testid="current-user-pill">
							<span class="status-dot dot-online"></span>
							<span>{$authState.username}</span>
						</div>
					</div>
				</header>

				{#if $toastStore}
					<div
						class="toast-banner"
						class:toast-error={$toastStore.type === 'error'}
						class:toast-success={$toastStore.type === 'success'}
						role="status"
						data-testid="toast-banner"
					>
						<span>{$toastStore.message}</span>
					</div>
				{/if}

				<main class="main-content">
					{@render children()}
				</main>
			</div>
		</div>
	{/if}
</div>
