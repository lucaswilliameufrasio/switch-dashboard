<script>
	import { onMount } from 'svelte';
	import { apiFetch } from '$lib/api.js';
	import { dashboardState, showToast, formatBytes } from '$lib/stores.js';

	let backups = $state([]);
	let loading = $state(true);
	let backingUpIp = $state('');

	async function loadBackups() {
		loading = true;
		try {
			const res = await apiFetch('/api/backups');
			backups = Array.isArray(res) ? res : res.backups || [];
		} catch (e) {
			showToast(e.message, 'error');
		} finally {
			loading = false;
		}
	}

	async function triggerBackup(sw) {
		backingUpIp = sw.ip;
		try {
			const res = await apiFetch(`/api/switches/${encodeURIComponent(sw.ip)}/backup`, {
				method: 'POST'
			});
			showToast(`Backup salvo: ${res.filename} (${formatBytes(res.size)}).`, 'success');
			await loadBackups();
		} catch (e) {
			showToast(e.message, 'error');
		} finally {
			backingUpIp = '';
		}
	}

	async function deleteBackup(filename) {
		try {
			await apiFetch(`/api/backups/${encodeURIComponent(filename)}`, {
				method: 'DELETE'
			});
			backups = backups.filter((b) => b.filename !== filename);
			showToast('Backup removido.', 'success');
		} catch (e) {
			showToast(e.message, 'error');
		}
	}

	onMount(loadBackups);
</script>

<div class="page-header">
	<div>
		<h1 class="page-title">Cofre de Backups de Configuração</h1>
		<p class="page-subtitle">
			Extraia e armazene imagens binárias (`.bin`) de configuração dos switches Horaco / KeepLink
		</p>
	</div>

	<div class="toolbar">
		<button type="button" onclick={loadBackups} class="btn btn-secondary btn-sm" data-testid="backups-refresh-btn">
			Atualizar Lista
		</button>
	</div>
</div>

{#if ($dashboardState.switches || []).length > 0}
	<div class="grid-3col" style="margin-bottom: var(--space-5);">
		{#each $dashboardState.switches || [] as sw}
			<div class="kpi-card" style="flex-direction: row; align-items: center; justify-content: space-between;">
				<div>
					<div style="display: flex; align-items: center; gap: 8px;">
						<span class="status-dot {sw.is_online ? 'dot-online' : 'dot-offline'}"></span>
						<span style="font-weight: 600; font-size: 13.5px;">{sw.name}</span>
					</div>
					<div class="mono" style="font-size: 11.5px; color: var(--text-secondary); margin-top: 2px;">
						{sw.ip} &middot; {sw.model || 'Switch'}
					</div>
				</div>

				<button
					type="button"
					onclick={() => triggerBackup(sw)}
					disabled={backingUpIp === sw.ip || !sw.supports_backup}
					class="btn btn-primary btn-sm"
					data-testid="trigger-backup-{sw.ip}"
				>
					{backingUpIp === sw.ip ? 'Extraindo...' : 'Gerar Backup'}
				</button>
			</div>
		{/each}
	</div>
{/if}

<section class="table-card" data-testid="backups-table-card">
	<div class="table-card-header">
		<h2 class="page-title" style="font-size: 14px;">Arquivos Salvos ({backups.length})</h2>
	</div>

	{#if loading}
		<div class="empty-state">
			<p style="color: var(--text-secondary);">Carregando arquivos de backup...</p>
		</div>
	{:else if backups.length === 0}
		<div class="empty-state" data-testid="empty-backups-state">
			<p style="color: var(--text-secondary);">
				Nenhum backup armazenado ainda. Use o botão "Gerar Backup" em um switch ativo acima.
			</p>
		</div>
	{:else}
		<div class="table-scroll">
			<table class="data-table">
				<thead>
					<tr>
						<th>Arquivo</th>
						<th>Switch IP</th>
						<th>Tamanho</th>
						<th>Data de Criação</th>
						<th style="text-align: right;">Ações</th>
					</tr>
				</thead>
				<tbody>
					{#each backups as b}
						<tr data-testid="backup-row-{b.filename}">
							<td class="mono" style="font-weight: 600;">{b.filename}</td>
							<td class="mono" style="color: var(--text-secondary);">{b.ip || '—'}</td>
							<td class="mono" style="color: var(--text-secondary);">{formatBytes(b.size)}</td>
							<td class="mono" style="color: var(--text-secondary);">{b.datetime || b.created || ''}</td>
							<td style="text-align: right;">
								<div style="display: inline-flex; gap: 6px;">
									<a
										href="/api/backups/{encodeURIComponent(b.filename)}/download"
										class="btn btn-secondary btn-sm"
										data-testid="backup-download-{b.filename}"
									>
										Download
									</a>
									<button
										type="button"
										onclick={() => deleteBackup(b.filename)}
										class="btn btn-danger btn-sm"
										data-testid="backup-delete-{b.filename}"
									>
										Excluir
									</button>
								</div>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</section>
