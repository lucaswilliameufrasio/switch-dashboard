<script>
	import { onMount } from 'svelte';
	import { apiFetch, refreshDashboardData } from '$lib/api.js';
	import { authState, showToast } from '$lib/stores.js';

	let activeTab = $state('switches');
	let loading = $state(true);

	let switches = $state([]);
	let dashboardTitle = $state('Switch Dashboard');
	let pollingInterval = $state(15);
	let macRefreshInterval = $state(5);
	let macIgnoreList = $state('');
	let infraDevices = $state([]);
	let unmanagedSwitches = $state([]);

	let templates = $state([]);
	let selectedTemplateFile = $state('');
	let templateContent = $state('');
	let vendorsContent = $state('');
	let updatingOui = $state(false);

	let currentPassword = $state('');
	let newUsername = $state('');
	let newPassword = $state('');

	const builtInModels = [
		{ id: 'Horaco', label: 'Horaco / KeepLink (CGI Nativo 2.5G + 10G)' },
		{ id: '2.5Gbe-RTL8372-5P', label: 'RTL8372 5-Port 2.5G + 10G SFP+' },
		{ id: '2.5Gbe-RTL8373-8P', label: 'RTL8373 8-Port 2.5G + 10G SFP+' }
	];

	async function loadConfig() {
		loading = true;
		try {
			const [cfg, tmplRes, vendRes] = await Promise.all([
				apiFetch('/api/config'),
				apiFetch('/api/templates'),
				apiFetch('/api/vendors')
			]);

			switches = (cfg.switches || []).map((sw) => ({
				...sw,
				enabled: sw.enabled !== false,
				port_count: sw.port_count || 9,
				password: '',
				has_password: !!sw.has_password,
				parent_ip: sw.parent_ip || '',
				parent_port: sw.parent_port || '',
				uplink_port: sw.uplink_port || ''
			}));

			dashboardTitle = cfg.title || 'Switch Dashboard';
			pollingInterval = cfg.refresh_interval || 15;
			macRefreshInterval = cfg.mac_refresh_multiplier || 5;
			macIgnoreList = (cfg.settings?.ignored_macs || []).join('\n');
			infraDevices = cfg.infrastructure_devices || [];
			unmanagedSwitches = cfg.unmanaged_switches || [];

			const rawTemplates = tmplRes.templates || [];
			templates = rawTemplates.map((t) =>
				typeof t === 'string'
					? { filename: t, id: t.replace(/\.yaml$/i, ''), name: t.replace(/\.yaml$/i, '') }
					: t
			);

			vendorsContent = vendRes.content || '';
			newUsername = $authState.username || 'admin';

			if (templates.length > 0 && !selectedTemplateFile) {
				await selectTemplate(templates[0].filename);
			}
		} catch (e) {
			showToast(e.message, 'error');
		} finally {
			loading = false;
		}
	}

	function addSwitchRow() {
		switches = [
			...switches,
			{
				name: `Switch-${switches.length + 1}`,
				ip: '192.168.1.10',
				username: 'admin',
				password: '',
				has_password: false,
				model: 'Horaco',
				port_count: 6,
				enabled: true,
				parent_ip: '',
				parent_port: '',
				uplink_port: ''
			}
		];
	}

	function removeSwitchRow(idx) {
		switches = switches.filter((_, i) => i !== idx);
	}

	async function saveFullConfig(successMsg) {
		const ignores = macIgnoreList
			.split('\n')
			.map((s) => s.trim())
			.filter(Boolean);

		const switchPayload = switches.map((sw) => ({
			name: (sw.name || '').trim(),
			ip: (sw.ip || '').trim(),
			username: (sw.username || '').trim() || 'admin',
			password: sw.password || '',
			model: sw.model || 'Horaco',
			port_count: Number(sw.port_count) || 9,
			enabled: sw.enabled !== false,
			parent_ip: (sw.parent_ip || '').trim(),
			parent_port: String(sw.parent_port || '').trim(),
			uplink_port: String(sw.uplink_port || '').trim()
		}));

		try {
			await apiFetch('/api/config', {
				method: 'POST',
				body: JSON.stringify({
					title: dashboardTitle,
					refresh_interval: Number(pollingInterval),
					mac_refresh_multiplier: Number(macRefreshInterval),
					switches: switchPayload,
					infrastructure_devices: infraDevices,
					unmanaged_switches: unmanagedSwitches,
					settings: {
						ignored_macs: ignores
					}
				})
			});
			showToast(successMsg, 'success');
			await loadConfig();
			await refreshDashboardData();
		} catch (e) {
			showToast(e.message, 'error');
		}
	}

	async function rebootSwitch(ip, name) {
		if (!confirm(`Deseja realmente reiniciar o switch ${name} (${ip})?`)) return;
		try {
			await apiFetch(`/api/switches/${encodeURIComponent(ip)}/reboot`, {
				method: 'POST'
			});
			showToast(`Comando de reboot enviado para ${name}.`, 'success');
		} catch (e) {
			showToast(e.message, 'error');
		}
	}

	async function resetCounters() {
		if (!confirm('Zerar os contadores acumulados e o histórico de tráfego?')) return;
		try {
			await apiFetch('/api/reset', { method: 'POST' });
			showToast('Contadores de tráfego reiniciados.', 'success');
		} catch (e) {
			showToast(e.message, 'error');
		}
	}

	async function selectTemplate(filename) {
		selectedTemplateFile = filename;
		try {
			const res = await apiFetch(`/api/templates/${encodeURIComponent(filename)}`);
			templateContent = res.content || '';
		} catch (e) {
			showToast(e.message, 'error');
		}
	}

	async function saveTemplate() {
		if (!selectedTemplateFile) return;
		try {
			await apiFetch(`/api/templates/${encodeURIComponent(selectedTemplateFile)}`, {
				method: 'POST',
				body: JSON.stringify({ content: templateContent })
			});
			showToast(`Template ${selectedTemplateFile} atualizado.`, 'success');
		} catch (e) {
			showToast(e.message, 'error');
		}
	}

	async function saveVendors() {
		try {
			await apiFetch('/api/vendors', {
				method: 'POST',
				body: JSON.stringify({ content: vendorsContent })
			});
			showToast('Lista de fabricantes MAC salva.', 'success');
		} catch (e) {
			showToast(e.message, 'error');
		}
	}

	async function updateOuiFromIeee() {
		updatingOui = true;
		try {
			const res = await apiFetch('/api/vendors/update_oui', { method: 'POST' });
			showToast(res.message || 'Base IEEE OUI atualizada.', 'success');
		} catch (e) {
			showToast(e.message, 'error');
		} finally {
			updatingOui = false;
		}
	}

	async function updateAdminPassword(e) {
		e.preventDefault();
		try {
			await apiFetch('/api/auth/password', {
				method: 'POST',
				body: JSON.stringify({
					username: newUsername,
					current_password: currentPassword,
					new_password: newPassword
				})
			});
			currentPassword = '';
			newPassword = '';
			showToast('Credenciais de administrador atualizadas.', 'success');
		} catch (err) {
			showToast(err.message, 'error');
		}
	}

	onMount(loadConfig);
</script>

<div class="page-header">
	<div>
		<h1 class="page-title">Configurações do Switch Dashboard</h1>
		<p class="page-subtitle">
			Gerencie switches Horaco/KeepLink/RTL, topologia L2, templates YAML e segurança do console
		</p>
	</div>
</div>

<!-- Settings Sub-Navigation Tabs -->
<div class="tab-strip" style="margin-bottom: var(--space-5);" role="tablist" aria-label="Abas de Configuração">
	{#each [
		{ id: 'switches', label: 'Switches Gerenciados' },
		{ id: 'general', label: 'Geral & Topologia L2' },
		{ id: 'templates', label: 'Templates YAML & OUI' },
		{ id: 'security', label: 'Segurança & Administrador' }
	] as tab}
		<button
			type="button"
			role="tab"
			aria-selected={activeTab === tab.id}
			onclick={() => (activeTab = tab.id)}
			class="tab-btn"
			class:active={activeTab === tab.id}
			data-testid="settings-tab-{tab.id}"
		>
			{tab.label}
		</button>
	{/each}
</div>

{#if loading}
	<div class="card empty-state">
		<p style="color: var(--text-secondary);">Carregando configurações...</p>
	</div>
{:else if activeTab === 'switches'}
	<section class="card" style="padding: var(--space-5);" data-testid="settings-switches-panel">
		<div class="page-header" style="margin-bottom: var(--space-4);">
			<div>
				<h2 class="page-title" style="font-size: 15px;">Switches da Rede</h2>
				<p class="page-subtitle">
					As senhas dos switches são criptografadas em repouso com AES-256-GCM e nunca retornam em texto puro.
				</p>
			</div>
			<button type="button" onclick={addSwitchRow} class="btn btn-secondary" data-testid="add-switch-row-btn">
				+ Adicionar Switch
			</button>
		</div>

		<div style="display: flex; flex-direction: column; gap: var(--space-4);">
			{#each switches as sw, idx}
				<div
					style="background-color: var(--bg-inset); border: 1px solid var(--border-subtle); border-radius: var(--radius-md); padding: var(--space-4);"
					data-testid="switch-config-row-{idx}"
				>
					<div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(170px, 1fr)); gap: var(--space-3); align-items: end;">
						<label class="field-group">
							<span class="field-label">Nome do Switch</span>
							<input
								type="text"
								bind:value={sw.name}
								class="input input-sm"
								placeholder="Core-Switch"
								data-testid="switch-name-input-{idx}"
							/>
						</label>

						<label class="field-group">
							<span class="field-label">Endereço IP / Host:Porta</span>
							<input
								type="text"
								bind:value={sw.ip}
								class="input input-sm mono"
								placeholder="192.168.1.2"
								data-testid="switch-ip-input-{idx}"
							/>
						</label>

						<label class="field-group">
							<span class="field-label">Usuário</span>
							<input
								type="text"
								bind:value={sw.username}
								class="input input-sm"
								placeholder="admin"
								data-testid="switch-user-input-{idx}"
							/>
						</label>

						<label class="field-group">
							<span class="field-label">
								Senha {sw.has_password ? '(Salva em AES-GCM)' : '(AES-256-GCM)'}
							</span>
							<input
								type="password"
								bind:value={sw.password}
								class="input input-sm"
								placeholder={sw.has_password ? '•••••••• (manter atual)' : 'Senha do switch'}
								data-testid="switch-pass-input-{idx}"
							/>
						</label>

						<label class="field-group">
							<span class="field-label">Driver / Template</span>
							<select bind:value={sw.model} class="select input-sm" data-testid="switch-model-select-{idx}">
								{#each builtInModels as bm}
									<option value={bm.id}>{bm.label}</option>
								{/each}
								{#each templates as t}
									<option value={t.id}>YAML: {t.name || t.id}</option>
								{/each}
							</select>
						</label>

						<label class="field-group">
							<span class="field-label">IP Switch Pai (Uplink)</span>
							<input
								type="text"
								bind:value={sw.parent_ip}
								class="input input-sm mono"
								placeholder="Opcional: 192.168.1.1"
							/>
						</label>
					</div>

					<div style="display: flex; align-items: center; justify-content: flex-end; gap: 8px; margin-top: 12px; padding-top: 12px; border-top: 1px solid var(--border-subtle);">
						<button
							type="button"
							onclick={() => rebootSwitch(sw.ip, sw.name)}
							class="btn btn-warning btn-sm"
						>
							Reiniciar Switch
						</button>
						<button
							type="button"
							onclick={() => removeSwitchRow(idx)}
							class="btn btn-danger btn-sm"
							data-testid="remove-switch-row-{idx}"
						>
							Remover
						</button>
					</div>
				</div>
			{/each}
		</div>

		<div style="margin-top: var(--space-5); display: flex; justify-content: flex-end;">
			<button
				type="button"
				onclick={() => saveFullConfig('Switches salvos (senhas criptografadas com AES-256-GCM).')}
				class="btn btn-primary"
				data-testid="save-switches-btn"
			>
				Salvar Switches
			</button>
		</div>
	</section>
{:else if activeTab === 'general'}
	<div class="grid-2col" data-testid="settings-general-panel">
		<section class="card" style="padding: var(--space-5); display: flex; flex-direction: column; gap: var(--space-4);">
			<h2 class="page-title" style="font-size: 15px;">Parâmetros de Polling</h2>

			<label class="field-group">
				<span class="field-label">Título do Console</span>
				<input type="text" bind:value={dashboardTitle} class="input" data-testid="settings-title-input" />
			</label>

			<div class="grid-2col" style="gap: var(--space-3);">
				<label class="field-group">
					<span class="field-label">Intervalo de Polling (segundos)</span>
					<input type="number" min="1" max="300" bind:value={pollingInterval} class="input mono" />
				</label>
				<label class="field-group">
					<span class="field-label">Multiplicador de Refresh MAC</span>
					<input type="number" min="1" max="100" bind:value={macRefreshInterval} class="input mono" />
				</label>
			</div>

			<label class="field-group">
				<span class="field-label">
					Filtro de MACs Ignorados (1 por linha, suporta wildcard <code class="mono">*</code>)
				</span>
				<textarea
					bind:value={macIgnoreList}
					rows="4"
					placeholder="01:00:5E:*&#10;33:33:*"
					class="textarea mono"
				></textarea>
			</label>

			<div style="display: flex; align-items: center; justify-content: space-between; padding-top: 8px;">
				<button type="button" onclick={resetCounters} class="btn btn-danger">
					Zerar Contadores de Tráfego
				</button>
				<button
					type="button"
					onclick={() => saveFullConfig('Configurações gerais salvas.')}
					class="btn btn-primary"
					data-testid="save-general-btn"
				>
					Salvar Configurações
				</button>
			</div>
		</section>

		<section class="card" style="padding: var(--space-5); display: flex; flex-direction: column; gap: var(--space-5);">
			<div>
				<div style="display: flex; align-items: center; justify-content: space-between; margin-bottom: 10px;">
					<h3 class="page-title" style="font-size: 14px;">Switches Não-Gerenciáveis (Hubs L2)</h3>
					<button
						type="button"
						onclick={() =>
							(unmanagedSwitches = [
								...unmanagedSwitches,
								{ name: 'Mini-Switch', parent_ip: '', parent_port: '1' }
							])}
						class="btn btn-secondary btn-sm"
					>
						+ Adicionar Hub
					</button>
				</div>
				<div style="display: flex; flex-direction: column; gap: 8px;">
					{#each unmanagedSwitches as u, idx}
						<div style="display: grid; grid-template-columns: 1fr 1fr 80px auto; gap: 8px; align-items: center; background-color: var(--bg-inset); padding: 8px; border-radius: var(--radius-md); border: 1px solid var(--border-subtle);">
							<label class="field-group" style="margin: 0;">
								<span class="field-label" style="display: none;">Nome do Hub</span>
								<input type="text" bind:value={u.name} placeholder="Nome" class="input input-sm" />
							</label>
							<label class="field-group" style="margin: 0;">
								<span class="field-label" style="display: none;">IP Switch Pai</span>
								<input type="text" bind:value={u.parent_ip} placeholder="IP Switch Pai" class="input input-sm mono" />
							</label>
							<label class="field-group" style="margin: 0;">
								<span class="field-label" style="display: none;">Porta</span>
								<input type="text" bind:value={u.parent_port} placeholder="Porta" class="input input-sm mono" />
							</label>
							<button
								type="button"
								onclick={() => (unmanagedSwitches = unmanagedSwitches.filter((_, i) => i !== idx))}
								class="btn btn-danger btn-sm"
							>
								Remover
							</button>
						</div>
					{/each}
				</div>
			</div>

			<div style="padding-top: 14px; border-top: 1px solid var(--border-subtle);">
				<div style="display: flex; align-items: center; justify-content: space-between; margin-bottom: 10px;">
					<h3 class="page-title" style="font-size: 14px;">Nós de Infraestrutura (Gateways / APs por MAC)</h3>
					<button
						type="button"
						onclick={() =>
							(infraDevices = [
								...infraDevices,
								{ name: 'Gateway', mac: '', type: 'router' }
							])}
						class="btn btn-secondary btn-sm"
					>
						+ Adicionar Nó
					</button>
				</div>
				<div style="display: flex; flex-direction: column; gap: 8px;">
					{#each infraDevices as inf, idx}
						<div style="display: grid; grid-template-columns: 1fr 1fr 100px auto; gap: 8px; align-items: center; background-color: var(--bg-inset); padding: 8px; border-radius: var(--radius-md); border: 1px solid var(--border-subtle);">
							<label class="field-group" style="margin: 0;">
								<span class="field-label" style="display: none;">Nome</span>
								<input type="text" bind:value={inf.name} placeholder="Nome" class="input input-sm" />
							</label>
							<label class="field-group" style="margin: 0;">
								<span class="field-label" style="display: none;">MAC</span>
								<input type="text" bind:value={inf.mac} placeholder="AA:BB:CC:DD:EE:FF" class="input input-sm mono" />
							</label>
							<label class="field-group" style="margin: 0;">
								<span class="field-label" style="display: none;">Tipo</span>
								<input type="text" bind:value={inf.type} placeholder="router / ap" class="input input-sm" />
							</label>
							<button
								type="button"
								onclick={() => (infraDevices = infraDevices.filter((_, i) => i !== idx))}
								class="btn btn-danger btn-sm"
							>
								✕
							</button>
						</div>
					{/each}
				</div>
			</div>
		</section>
	</div>
{:else if activeTab === 'templates'}
	<div class="grid-2col" data-testid="settings-templates-panel">
		<section class="card" style="padding: var(--space-5); display: flex; flex-direction: column; gap: var(--space-3);">
			<div style="display: flex; align-items: center; justify-content: space-between; gap: 12px;">
				<div>
					<h2 class="page-title" style="font-size: 15px;">Templates YAML de Switches</h2>
					<p class="page-subtitle">Suporte declarativo para modelos Realtek / Horaco / KeepLink</p>
				</div>
				<label class="field-group" style="margin: 0;">
					<span class="field-label" style="display: none;">Selecionar Template</span>
					<select
						value={selectedTemplateFile}
						onchange={(e) => selectTemplate(e.currentTarget.value)}
						class="select input-sm mono"
						style="width: 220px;"
					>
						{#each templates as t}
							<option value={t.filename}>{t.filename}</option>
						{/each}
					</select>
				</label>
			</div>

			<label class="field-group">
				<span class="field-label" style="display: none;">Conteúdo YAML</span>
				<textarea bind:value={templateContent} rows="16" class="textarea mono"></textarea>
			</label>

			<div style="display: flex; justify-content: flex-end;">
				<button type="button" onclick={saveTemplate} class="btn btn-primary">
					Salvar Template YAML
				</button>
			</div>
		</section>

		<section class="card" style="padding: var(--space-5); display: flex; flex-direction: column; gap: var(--space-3);">
			<div style="display: flex; align-items: center; justify-content: space-between; gap: 12px;">
				<div>
					<h2 class="page-title" style="font-size: 15px;">Prefixos MAC OUI (`mac_vendors.txt`)</h2>
					<p class="page-subtitle">Mapeamento local sem consultas automáticas externas</p>
				</div>
				<button
					type="button"
					onclick={updateOuiFromIeee}
					disabled={updatingOui}
					class="btn btn-secondary btn-sm"
				>
					{updatingOui ? 'Baixando IEEE OUI...' : 'Atualizar OUI (IEEE)'}
				</button>
			</div>

			<label class="field-group">
				<span class="field-label" style="display: none;">Lista de Fabricantes</span>
				<textarea bind:value={vendorsContent} rows="16" class="textarea mono"></textarea>
			</label>

			<div style="display: flex; justify-content: flex-end;">
				<button type="button" onclick={saveVendors} class="btn btn-primary">
					Salvar mac_vendors.txt
				</button>
			</div>
		</section>
	</div>
{:else if activeTab === 'security'}
	<div class="grid-2col" data-testid="settings-security-panel">
		<section class="card" style="padding: var(--space-5);">
			<h2 class="page-title" style="font-size: 15px;">Credenciais do Administrador</h2>
			<p class="page-subtitle" style="margin-bottom: var(--space-4);">
				Senhas de administrador são armazenadas com hash <code class="mono" style="color: var(--accent-hover);">bcrypt</code>.
			</p>

			<form onsubmit={updateAdminPassword} class="auth-form">
				<label class="field-group">
					<span class="field-label">Novo Usuário Administrador</span>
					<input type="text" bind:value={newUsername} required class="input" />
				</label>

				<label class="field-group">
					<span class="field-label">Senha Atual</span>
					<input type="password" bind:value={currentPassword} required class="input" />
				</label>

				<label class="field-group">
					<span class="field-label">Nova Senha (mínimo 8 caracteres)</span>
					<input type="password" bind:value={newPassword} required minlength="8" class="input" />
				</label>

				<button type="submit" class="btn btn-primary">
					Atualizar Senha do Console
				</button>
			</form>
		</section>

		<section class="card" style="padding: var(--space-5);">
			<h2 class="page-title" style="font-size: 15px; margin-bottom: var(--space-3);">Blindagem de Segurança Ativa</h2>
			<ul style="list-style: none; display: flex; flex-direction: column; gap: 12px; color: var(--text-secondary);">
				<li style="display: flex; align-items: flex-start; gap: 10px;">
					<span class="status-dot dot-online" style="margin-top: 5px;"></span>
					<span><strong style="color: var(--text-primary);">Zero Telemetria Externa:</strong> Nenhuma conexão automática para domínios externos ou CDNs de terceiros.</span>
				</li>
				<li style="display: flex; align-items: flex-start; gap: 10px;">
					<span class="status-dot dot-online" style="margin-top: 5px;"></span>
					<span><strong style="color: var(--text-primary);">Cofre AES-256-GCM:</strong> As senhas dos switches em <code class="mono">config.json</code> são criptografadas com chave mestre de 256 bits (<code class="mono">.master.key</code> perm 0600).</span>
				</li>
				<li style="display: flex; align-items: flex-start; gap: 10px;">
					<span class="status-dot dot-online" style="margin-top: 5px;"></span>
					<span><strong style="color: var(--text-primary);">Proteção Anti-CSRF &amp; SameSite=Strict:</strong> Todas as rotas de mutação exigem token CSRF sincronizado, verificação de cabeçalho <code class="mono">Origin</code> e <code class="mono">Content-Type: application/json</code>.</span>
				</li>
				<li style="display: flex; align-items: flex-start; gap: 10px;">
					<span class="status-dot dot-online" style="margin-top: 5px;"></span>
					<span><strong style="color: var(--text-primary);">Superfície de Ataque Reduzida:</strong> Módulos legados vulneráveis (SSH/OVS <code class="mono">shell=True</code>, SOAP TR-064 e scanner ARP root) foram permanentemente eliminados.</span>
				</li>
			</ul>
		</section>
	</div>
{/if}
