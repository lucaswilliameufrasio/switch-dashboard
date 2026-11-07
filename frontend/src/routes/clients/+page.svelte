<script>
	import { onMount } from 'svelte';
	import { apiFetch } from '$lib/api.js';
	import { showToast } from '$lib/stores.js';

	let clients = $state([]);
	let loading = $state(true);
	let search = $state('');
	let statusFilter = $state('all');

	let editingMac = $state('');
	let editHostname = $state('');
	let editNote = $state('');

	let showCsvModal = $state(false);
	let csvText = $state('');

	const deviceTypes = [
		{ id: 'laptop', label: 'Notebook / Workstation' },
		{ id: 'pc', label: 'Desktop PC' },
		{ id: 'server', label: 'Servidor / Hypervisor' },
		{ id: 'nas', label: 'NAS / Storage' },
		{ id: 'router', label: 'Roteador / Firewall' },
		{ id: 'switch', label: 'Switch' },
		{ id: 'ap', label: 'Access Point Wi-Fi' },
		{ id: 'camera', label: 'Câmera IP / NVR' },
		{ id: 'iot', label: 'IoT / Smart Home' },
		{ id: 'phone', label: 'Smartphone / Tablet' },
		{ id: 'printer', label: 'Impressora' }
	];

	async function loadClients() {
		loading = true;
		try {
			const res = await apiFetch('/api/clients');
			const list = Array.isArray(res) ? res : res.clients || [];
			clients = list.map((c) => ({
				...c,
				hostname: c.host || c.hostname || '',
				is_online: c.status === 'online' || c.is_online === true
			}));
		} catch (e) {
			showToast(e.message, 'error');
		} finally {
			loading = false;
		}
	}

	function startEdit(client) {
		editingMac = client.mac;
		editHostname = client.hostname || '';
		editNote = client.note || '';
	}

	async function saveClientMeta(mac) {
		try {
			await apiFetch('/api/clients/update_host', {
				method: 'POST',
				body: JSON.stringify({ mac, host: editHostname, note: editNote })
			});
			clients = clients.map((c) =>
				c.mac === mac ? { ...c, hostname: editHostname, host: editHostname, note: editNote } : c
			);
			editingMac = '';
			showToast('Cliente atualizado.', 'success');
		} catch (e) {
			showToast(e.message, 'error');
		}
	}

	async function changeDeviceType(mac, device_type) {
		try {
			await apiFetch('/api/clients/update_type', {
				method: 'POST',
				body: JSON.stringify({ mac, type: device_type })
			});
			clients = clients.map((c) => (c.mac === mac ? { ...c, device_type } : c));
			showToast('Categoria atualizada.', 'success');
		} catch (e) {
			showToast(e.message, 'error');
		}
	}

	async function deleteClient(mac) {
		try {
			await apiFetch('/api/clients/delete', {
				method: 'POST',
				body: JSON.stringify({ mac })
			});
			clients = clients.filter((c) => c.mac !== mac);
			showToast('Registro removido.', 'success');
		} catch (e) {
			showToast(e.message, 'error');
		}
	}

	async function importCsv() {
		const trimmed = csvText.trim();
		if (!trimmed) {
			showToast('Insira as linhas CSV (Hostname,MAC).', 'error');
			return;
		}
		try {
			const formData = new FormData();
			const blob = new Blob([trimmed], { type: 'text/csv' });
			formData.append('file', blob, 'clients.csv');
			const res = await apiFetch('/api/clients/import_csv', {
				method: 'POST',
				body: formData
			});
			showToast(`${res.imported || 0} clientes importados.`, 'success');
			showCsvModal = false;
			csvText = '';
			await loadClients();
		} catch (e) {
			showToast(e.message, 'error');
		}
	}

	let filteredClients = $derived.by(() => {
		const q = search.trim().toLowerCase();
		return clients.filter((c) => {
			if (statusFilter === 'online' && !c.is_online) return false;
			if (statusFilter === 'offline' && c.is_online) return false;
			if (!q) return true;
			return (
				(c.mac || '').toLowerCase().includes(q) ||
				(c.hostname || '').toLowerCase().includes(q) ||
				(c.vendor || '').toLowerCase().includes(q) ||
				(c.ip || '').toLowerCase().includes(q) ||
				(c.note || '').toLowerCase().includes(q)
			);
		});
	});

	onMount(loadClients);
</script>

<div class="page-header">
	<div>
		<h1 class="page-title">Dispositivos Clientes (L2 FDB)</h1>
		<p class="page-subtitle">
			Inventário passivo de dispositivos identificados nas tabelas MAC dos switches
		</p>
	</div>

	<div class="toolbar">
		<label class="field-group" style="margin: 0;">
			<span class="field-label" style="display: none;">Buscar cliente</span>
			<input
				type="text"
				bind:value={search}
				placeholder="Buscar MAC, nome, fabricante, switch..."
				class="input input-sm"
				style="width: 240px;"
				data-testid="clients-search-input"
			/>
		</label>

		<div class="segmented-control">
			{#each [{ id: 'all', label: 'Todos' }, { id: 'online', label: 'Online' }, { id: 'offline', label: 'Offline' }] as f}
				<button
					type="button"
					onclick={() => (statusFilter = f.id)}
					class="segment-btn"
					class:active={statusFilter === f.id}
					data-testid="clients-filter-{f.id}"
				>
					{f.label}
				</button>
			{/each}
		</div>

		<button type="button" onclick={() => (showCsvModal = true)} class="btn btn-secondary btn-sm" data-testid="clients-import-csv-btn">
			Importar CSV
		</button>
		<button type="button" onclick={loadClients} class="btn btn-primary btn-sm" data-testid="clients-refresh-btn">
			Atualizar
		</button>
	</div>
</div>

<section class="table-card" data-testid="clients-table-card">
	{#if loading}
		<div class="empty-state">
			<p style="color: var(--text-secondary);">Carregando inventário de clientes...</p>
		</div>
	{:else if filteredClients.length === 0}
		<div class="empty-state">
			<p style="color: var(--text-secondary);">Nenhum cliente encontrado para o filtro selecionado.</p>
		</div>
	{:else}
		<div class="table-scroll">
			<table class="data-table">
				<thead>
					<tr>
						<th>Status</th>
						<th>Hostname / Nota</th>
						<th>Endereço MAC</th>
						<th>Fabricante</th>
						<th>Categoria</th>
						<th>Switch / Porta</th>
						<th></th>
					</tr>
				</thead>
				<tbody>
					{#each filteredClients as cl}
						<tr data-testid="client-row-{cl.mac}">
							<td>
								<span class="badge {cl.is_online ? 'badge-online' : 'badge-neutral'}">
									<span class="status-dot {cl.is_online ? 'dot-online' : ''}"></span>
									<span>{cl.is_online ? 'Online' : 'Offline'}</span>
								</span>
							</td>

							<td>
								{#if editingMac === cl.mac}
									<div style="display: flex; flex-direction: column; gap: 6px; max-width: 240px;">
										<label class="field-group" style="margin: 0;">
											<span class="field-label" style="display: none;">Hostname</span>
											<input
												type="text"
												bind:value={editHostname}
												placeholder="Hostname"
												class="input input-sm"
												data-testid="client-edit-hostname"
											/>
										</label>
										<label class="field-group" style="margin: 0;">
											<span class="field-label" style="display: none;">Nota</span>
											<input
												type="text"
												bind:value={editNote}
												placeholder="Nota / Localização"
												class="input input-sm"
												data-testid="client-edit-note"
											/>
										</label>
										<div style="display: flex; gap: 6px;">
											<button
												type="button"
												onclick={() => saveClientMeta(cl.mac)}
												class="btn btn-primary btn-sm"
												data-testid="client-save-meta-btn"
											>
												Salvar
											</button>
											<button
												type="button"
												onclick={() => (editingMac = '')}
												class="btn btn-secondary btn-sm"
											>
												Cancelar
											</button>
										</div>
									</div>
								{:else}
									<button
										type="button"
										onclick={() => startEdit(cl)}
										class="btn btn-ghost btn-sm"
										style="flex-direction: column; align-items: flex-start; text-align: left;"
										data-testid="client-edit-btn-{cl.mac}"
									>
										<span style="font-weight: 600; color: var(--text-primary);">
											{cl.hostname || cl.mac}
										</span>
										<span style="font-size: 11px; color: var(--text-secondary);">
											{cl.note || 'Clique para nomear'}
										</span>
									</button>
								{/if}
							</td>

							<td class="mono" style="color: var(--accent-hover);">{cl.mac}</td>
							<td>{cl.vendor || 'Unknown Vendor'}</td>

							<td>
								<label class="field-group" style="margin: 0;">
									<span class="field-label" style="display: none;">Categoria</span>
									<select
										value={cl.device_type || 'laptop'}
										onchange={(e) => changeDeviceType(cl.mac, e.currentTarget.value)}
										class="select input-sm"
										style="width: 170px;"
										data-testid="client-type-select-{cl.mac}"
									>
										{#each deviceTypes as dt}
											<option value={dt.id}>{dt.label}</option>
										{/each}
									</select>
								</label>
							</td>

							<td>
								{#if cl.ip}
									<div class="mono" style="font-weight: 600;">{cl.ip}</div>
									<div class="mono" style="font-size: 11px; color: var(--text-secondary);">
										Porta #{cl.port} {cl.vlan ? `• VLAN ${cl.vlan}` : ''}
									</div>
								{:else}
									<span style="color: var(--text-muted);">—</span>
								{/if}
							</td>

							<td style="text-align: right;">
								<button
									type="button"
									onclick={() => deleteClient(cl.mac)}
									class="btn btn-danger btn-sm"
									title="Remover cliente"
									data-testid="client-delete-btn-{cl.mac}"
								>
									Remover
								</button>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</section>

{#if showCsvModal}
	<div class="modal-backdrop" role="dialog" aria-modal="true" aria-label="Importar Clientes via CSV" data-testid="csv-modal">
		<div class="modal-panel" style="max-width: 520px;">
			<div class="modal-header">
				<div>
					<h3 class="page-title" style="font-size: 15px;">Importar Clientes via CSV</h3>
					<p class="page-subtitle">
						Um registro por linha no formato: <code class="mono" style="color: var(--accent-hover);">Hostname,AA:BB:CC:DD:EE:FF</code>
					</p>
				</div>
				<button type="button" onclick={() => (showCsvModal = false)} class="btn btn-secondary btn-sm">✕</button>
			</div>

			<div class="modal-body">
				<label class="field-group" style="margin-bottom: 14px;">
					<span class="field-label">Conteúdo CSV</span>
					<textarea
						bind:value={csvText}
						rows="6"
						placeholder="Proxmox-VE,00:11:22:33:44:55"
						class="textarea mono"
						data-testid="csv-textarea"
					></textarea>
				</label>

				<div style="display: flex; justify-content: flex-end; gap: 8px;">
					<button type="button" onclick={() => (showCsvModal = false)} class="btn btn-secondary">Cancelar</button>
					<button type="button" onclick={importCsv} class="btn btn-primary" data-testid="csv-submit-btn">Importar</button>
				</div>
			</div>
		</div>
	</div>
{/if}
