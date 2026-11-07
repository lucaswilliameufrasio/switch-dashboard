<script>
	import {
		dashboardState,
		activeSwitchIdx,
		selectedSwitch,
		showToast,
		formatBytes,
		formatRate
	} from '$lib/stores.js';
	import { apiFetch, refreshDashboardData } from '$lib/api.js';

	let inspectedPort = $state(null);
	let historyPeriod = $state('live');
	let historyPoints = $state([]);
	let loadingHistory = $state(false);
	let transceiverData = $state([]);
	let loadingTransceiver = $state(false);

	let showMacModal = $state(false);
	let refreshingMac = $state(false);
	let macSearch = $state('');
	let editingHostMac = $state('');
	let editingHostName = $state('');

	let editingNoteKey = $state('');
	let editingNoteVal = $state('');

	let summary = $derived.by(() => {
		const switches = $dashboardState.switches || [];
		let onlineSwitches = 0;
		let totalPorts = 0;
		let activePorts = 0;
		let totalRxRate = 0;
		let totalTxRate = 0;
		let totalMacs = 0;

		for (const sw of switches) {
			if (sw.is_online) onlineSwitches++;
			for (const p of sw.ports || []) {
				totalPorts++;
				if (p.is_up) activePorts++;
				totalRxRate += p.tx_rate || 0;
				totalTxRate += p.rx_rate || 0;
			}
			totalMacs += (sw.mac_table || []).length;
		}
		return {
			totalSwitches: switches.length,
			onlineSwitches,
			totalPorts,
			activePorts,
			totalRxRate,
			totalTxRate,
			totalMacs
		};
	});

	function getPortLedClass(port) {
		if (!port || !port.is_up) return 'led-down';
		const spd = (port.speed || '').toLowerCase();
		if (spd.includes('10g') || spd.includes('10000')) return 'led-10g';
		if (spd.includes('2.5g') || spd.includes('2500') || spd.includes('5g')) return 'led-25g';
		if (spd.includes('1000') || spd.includes('1g')) return 'led-1g';
		return 'led-100m';
	}

	function isSfpPort(port, totalPorts) {
		if (!port) return false;
		const spd = (port.speed || '').toLowerCase();
		if (spd.includes('10g') || spd.includes('sfp') || spd.includes('dac')) return true;
		const pNum = Number(port.port) || 0;
		if (totalPorts === 6 && pNum >= 5) return true;
		if (totalPorts === 9 && pNum >= 9) return true;
		if (totalPorts === 10 && pNum >= 9) return true;
		return false;
	}

	function getPortNote(sw, portNum) {
		if (!sw) return '';
		const notes = $dashboardState.notes || {};
		return notes[`${sw.ip}:${portNum}`] || notes[`${sw.name}_${portNum}`] || notes[`${sw.ip}_${portNum}`] || '';
	}

	function startEditNote(sw, portNum) {
		const key = `${sw.ip}:${portNum}`;
		editingNoteKey = key;
		editingNoteVal = getPortNote(sw, portNum);
	}

	async function savePortNote(sw, portNum) {
		const key = `${sw.ip}:${portNum}`;
		try {
			await apiFetch('/api/notes', {
				method: 'POST',
				body: JSON.stringify({ key, note: editingNoteVal })
			});
			await refreshDashboardData();
			editingNoteKey = '';
			showToast('Nota da porta salva.', 'success');
		} catch (e) {
			showToast(e.message, 'error');
		}
	}

	async function openPortInspector(port) {
		inspectedPort = port;
		await loadPortHistory(port.port, historyPeriod);
		if ($selectedSwitch?.supports_ddmi) {
			loadTransceiverDiag();
		}
	}

	let lastSyncedInspectorTs = 0;
	$effect(() => {
		const sw = $selectedSwitch;
		if (sw && inspectedPort && sw.timestamp && sw.timestamp !== lastSyncedInspectorTs) {
			lastSyncedInspectorTs = sw.timestamp;
			const updated = (sw.ports || []).find((p) => String(p.port) === String(inspectedPort.port));
			if (updated) {
				inspectedPort = updated;
			}
			if (historyPeriod === 'live') {
				loadPortHistory(inspectedPort.port, 'live');
			}
		}
	});

	async function loadPortHistory(portNum, period) {
		if (!$selectedSwitch) return;
		historyPeriod = period;
		loadingHistory = true;
		try {
			const res = await apiFetch(
				`/api/history?ip=${encodeURIComponent($selectedSwitch.ip)}&port=${encodeURIComponent(portNum)}&range=${period}`
			);
			const txArr = res.tx || [];
			const rxArr = res.rx || [];
			const len = Math.max(txArr.length, rxArr.length);
			const pts = [];
			for (let i = 0; i < len; i++) {
				pts.push({
					tx_rate: (txArr[i] || 0) / 8,
					rx_rate: (rxArr[i] || 0) / 8
				});
			}
			historyPoints = pts;
		} catch {
			historyPoints = [];
		} finally {
			loadingHistory = false;
		}
	}

	async function loadTransceiverDiag() {
		if (!$selectedSwitch) return;
		loadingTransceiver = true;
		try {
			const res = await apiFetch(`/api/switches/${encodeURIComponent($selectedSwitch.ip)}/transceiver`);
			transceiverData = Array.isArray(res) ? res : res.ports || [];
		} catch {
			transceiverData = [];
		} finally {
			loadingTransceiver = false;
		}
	}

	async function refreshMacTable() {
		if (!$selectedSwitch) return;
		refreshingMac = true;
		try {
			const res = await apiFetch(`/api/switches/${encodeURIComponent($selectedSwitch.ip)}/refresh_mac`, {
				method: 'POST'
			});
			await refreshDashboardData();
			showToast(`Tabela MAC atualizada (${(res.mac_table || []).length} entradas).`, 'success');
		} catch (e) {
			showToast(e.message, 'error');
		} finally {
			refreshingMac = false;
		}
	}

	async function saveMacHostname(mac) {
		try {
			await apiFetch('/api/clients/update_host', {
				method: 'POST',
				body: JSON.stringify({ mac, host: editingHostName })
			});
			await refreshDashboardData();
			editingHostMac = '';
			showToast('Hostname atualizado.', 'success');
		} catch (e) {
			showToast(e.message, 'error');
		}
	}

	let chartGeometry = $derived.by(() => {
		const pts = historyPoints || [];
		if (pts.length < 2) return null;
		const width = 640;
		const height = 180;
		let maxVal = 1024;
		for (const p of pts) {
			if ((p.tx_rate || 0) > maxVal) maxVal = p.tx_rate;
			if ((p.rx_rate || 0) > maxVal) maxVal = p.rx_rate;
		}
		maxVal *= 1.15;

		const toX = (i) => (i / (pts.length - 1)) * width;
		const toY = (v) => height - Math.min(height, Math.max(0, (v / maxVal) * height));

		const dlPoints = pts.map((p, i) => `${toX(i).toFixed(1)},${toY(p.tx_rate || 0).toFixed(1)}`).join(' ');
		const ulPoints = pts.map((p, i) => `${toX(i).toFixed(1)},${toY(p.rx_rate || 0).toFixed(1)}`).join(' ');

		const dlArea = `0,${height} ${dlPoints} ${width},${height}`;
		const ulArea = `0,${height} ${ulPoints} ${width},${height}`;

		return { width, height, maxVal, dlPoints, ulPoints, dlArea, ulArea };
	});

	let filteredMacs = $derived.by(() => {
		const table = $selectedSwitch?.mac_table || [];
		const q = macSearch.trim().toLowerCase();
		if (!q) return table;
		return table.filter(
			(m) =>
				(m.mac || '').toLowerCase().includes(q) ||
				(m.vendor || '').toLowerCase().includes(q) ||
				(m.hostname || '').toLowerCase().includes(q) ||
				String(m.port).toLowerCase().includes(q)
		);
	});
</script>

<svelte:window
	onkeydown={(e) => {
		if (e.key === 'Escape') {
			inspectedPort = null;
			showMacModal = false;
		}
	}}
/>

<!-- KPI Summary Strip -->
<section class="kpi-grid" aria-label="Resumo da Rede" data-testid="kpi-grid">
	<div class="kpi-card">
		<div class="kpi-header">
			<span class="kpi-label">Switches Online</span>
			<span
				class="status-dot {summary.onlineSwitches === summary.totalSwitches && summary.totalSwitches > 0
					? 'dot-online'
					: 'dot-warning'}"
			></span>
		</div>
		<div class="kpi-value mono">
			{summary.onlineSwitches} <span class="kpi-value-sub">/ {summary.totalSwitches}</span>
		</div>
		<div class="kpi-foot">Switches gerenciados via HTTP/CGI</div>
	</div>

	<div class="kpi-card">
		<div class="kpi-header">
			<span class="kpi-label">Portas Ativas</span>
			<span class="badge badge-accent mono">
				{summary.totalPorts ? Math.round((summary.activePorts / summary.totalPorts) * 100) : 0}%
			</span>
		</div>
		<div class="kpi-value mono">
			{summary.activePorts} <span class="kpi-value-sub">/ {summary.totalPorts}</span>
		</div>
		<div class="kpi-foot">Interfaces RJ45 2.5GbE &amp; SFP+ 10GbE</div>
	</div>

	<div class="kpi-card">
		<div class="kpi-header">
			<span class="kpi-label">Throughput Agregado</span>
			<span class="badge badge-online mono">LIVE</span>
		</div>
		<div class="kpi-value mono" style="font-size: 16px; display: flex; gap: 14px; align-items: baseline;">
			<span><strong style="color: var(--accent-hover);">↓</strong> {formatRate(summary.totalRxRate)}</span>
			<span><strong style="color: var(--status-online);">↑</strong> {formatRate(summary.totalTxRate)}</span>
		</div>
		<div class="kpi-foot">Taxa instantânea somada nas portas</div>
	</div>

	<div class="kpi-card">
		<div class="kpi-header">
			<span class="kpi-label">Endereços MAC FDB</span>
			<span class="badge badge-neutral mono">L2</span>
		</div>
		<div class="kpi-value mono">{summary.totalMacs}</div>
		<div class="kpi-foot">Dispositivos aprendidos nas tabelas FDB</div>
	</div>
</section>

{#if ($dashboardState.switches || []).length === 0}
	<div class="card empty-state" data-testid="empty-switches-state">
		<div class="empty-icon">
			<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
				<rect x="2" y="6" width="20" height="12" rx="2" />
				<path d="M6 12h.01M10 12h.01M14 12h.01M18 12h.01" />
			</svg>
		</div>
		<h2 class="page-title">Nenhum Switch Configurado</h2>
		<p class="page-subtitle" style="max-width: 460px;">
			Adicione seus switches Horaco, KeepLink ou RTLPlayground em Configurações para visualizar o chassi 1U, telemetria de portas, diagnósticos SFP+ DDMI e topologia L2.
		</p>
		<a href="/settings" class="btn btn-primary" data-testid="empty-configure-btn">Configurar Switches</a>
	</div>
{:else}
	<!-- Switch Selector & Actions Toolbar -->
	<div class="page-header">
		<div class="tab-strip" role="tablist" aria-label="Selecionar Switch">
			{#each $dashboardState.switches || [] as sw, idx}
				<button
					type="button"
					role="tab"
					aria-selected={$activeSwitchIdx === idx}
					onclick={() => {
						activeSwitchIdx.set(idx);
						inspectedPort = null;
					}}
					class="tab-btn"
					class:active={$activeSwitchIdx === idx}
					data-testid="switch-tab-{idx}"
				>
					<span class="status-dot {sw.is_online ? 'dot-online' : 'dot-offline'}"></span>
					<span>{sw.name}</span>
					<span class="mono" style="color: var(--text-muted); font-size: 11px;">{sw.ip}</span>
				</button>
			{/each}
		</div>

		{#if $selectedSwitch}
			<div class="toolbar">
				{#if $selectedSwitch.supports_mac}
					<button
						type="button"
						onclick={() => (showMacModal = true)}
						class="btn btn-secondary"
						data-testid="open-mac-modal-btn"
					>
						<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
							<path d="M4 6h16M4 12h16M4 18h10" />
						</svg>
						<span>Tabela MAC ({($selectedSwitch.mac_table || []).length})</span>
					</button>
				{/if}

				{#if $selectedSwitch.supports_ddmi}
					<button
						type="button"
						onclick={() => {
							if ($selectedSwitch.ports?.length) {
								openPortInspector($selectedSwitch.ports[$selectedSwitch.ports.length - 1]);
							}
						}}
						class="btn btn-secondary"
						data-testid="open-ddmi-btn"
					>
						<span>Diagnóstico SFP+ DDMI</span>
					</button>
				{/if}

				<a
					href="http://{$selectedSwitch.ip}"
					target="_blank"
					rel="noopener noreferrer"
					class="btn btn-secondary"
				>
					<span>Web CGI ↗</span>
				</a>
			</div>
		{/if}
	</div>

	{#if $selectedSwitch}
		<!-- 1U Rack-Mount Switch Chassis Faceplate -->
		<section class="chassis-card" aria-label="Chassi do Switch {$selectedSwitch.name}" data-testid="switch-chassis">
			<div class="chassis-header">
				<div class="chassis-identity">
					<span class="status-dot {$selectedSwitch.is_online ? 'dot-online' : 'dot-offline'}"></span>
					<div>
						<div class="chassis-title-row">
							<h2 class="chassis-name">{$selectedSwitch.name}</h2>
							{#if $selectedSwitch.model}
								<span class="badge badge-accent mono">{$selectedSwitch.model}</span>
							{/if}
							{#if $selectedSwitch.is_online}
								<span class="badge badge-online" data-testid="switch-status-badge">ONLINE</span>
							{:else}
								<span class="badge badge-danger" data-testid="switch-status-badge">OFFLINE</span>
							{/if}
							{#if $selectedSwitch.jumbo_frame?.enabled}
								<span class="badge badge-neutral mono">Jumbo {$selectedSwitch.jumbo_frame.size || 'ON'}</span>
							{/if}
						</div>

						<div class="chassis-meta mono">
							<span>IP: {$selectedSwitch.ip}</span>
							{#if $selectedSwitch.mac_address}
								<span>MAC: {$selectedSwitch.mac_address}</span>
							{/if}
							{#if $selectedSwitch.firmware}
								<span>FW: {$selectedSwitch.firmware}</span>
							{/if}
							{#if $selectedSwitch.uptime}
								<span>Uptime: {$selectedSwitch.uptime}</span>
							{/if}
						</div>
					</div>
				</div>

				<div class="speed-legend" aria-label="Legenda de Velocidades">
					<span class="legend-item"><span class="port-led led-10g"></span> 10G SFP+</span>
					<span class="legend-item"><span class="port-led led-25g"></span> 2.5G</span>
					<span class="legend-item"><span class="port-led led-1g"></span> 1G</span>
					<span class="legend-item"><span class="port-led led-100m"></span> 100M/10M</span>
					<span class="legend-item"><span class="port-led led-down"></span> Sem Link</span>
				</div>
			</div>

			<div class="chassis-faceplate">
				{#each $selectedSwitch.ports || [] as port}
					{@const sfp = isSfpPort(port, ($selectedSwitch.ports || []).length)}
					{@const note = getPortNote($selectedSwitch, port.port)}
					<button
						type="button"
						onclick={() => openPortInspector(port)}
						title="Porta {port.port}: {port.speed || 'Down'} {note ? `(${note})` : ''} — Clique para inspecionar"
						class="port-bay"
						class:sfp-bay={sfp}
						class:port-up={port.is_up}
						class:port-selected={inspectedPort?.port === port.port}
						data-testid="port-bay-{port.port}"
					>
						<div class="port-bay-top mono">
							<span>#{port.port}</span>
							<span class="port-led {getPortLedClass(port)}"></span>
						</div>

						<div class="port-cutout mono">
							<span class="port-Cutout-type">{sfp ? 'SFP+' : 'RJ45'}</span>
							{#if port.is_up}
								<span class="port-cutout-speed">
									{(port.speed || '').replace('Full', '').replace('Half', '').trim()}
								</span>
							{/if}
						</div>

						<div class="port-bay-label">
							{note || (port.is_up ? port.speed : 'Down')}
						</div>
					</button>
				{/each}
			</div>
		</section>

		<!-- Port Telemetry Data Table -->
		<section class="table-card" aria-label="Tabela de Telemetria de Portas" data-testid="ports-table-card">
			<div class="table-card-header">
				<div>
					<h3 class="page-title" style="font-size: 14.5px;">Telemetria de Portas — {$selectedSwitch.name}</h3>
					<p class="page-subtitle">Clique em qualquer porta para abrir o gráfico de tráfego e diagnóstico SFP+ DDMI</p>
				</div>
			</div>

			<div class="table-scroll">
				<table class="data-table">
					<thead>
						<tr>
							<th>Porta</th>
							<th>Rótulo / Nota</th>
							<th>Status / Link</th>
							<th>Download (TX)</th>
							<th>Upload (RX)</th>
							<th>Acumulado</th>
							<th>Pacotes (TX/RX)</th>
							<th>Clientes FDB</th>
							<th></th>
						</tr>
					</thead>
					<tbody>
						{#each $selectedSwitch.ports || [] as port}
							{@const noteKey = `${$selectedSwitch.ip}:${port.port}`}
							{@const noteVal = getPortNote($selectedSwitch, port.port)}
							{@const portMacs = ($selectedSwitch.mac_table || []).filter((m) => String(m.port) === String(port.port) || String(m.port) === `Port ${port.port}`)}
							<tr data-testid="port-row-{port.port}">
								<td class="mono" style="font-weight: 600;">
									<div style="display: flex; align-items: center; gap: 8px;">
										<span class="port-led {getPortLedClass(port)}"></span>
										<span>Porta #{port.port}</span>
									</div>
								</td>

								<td>
									{#if editingNoteKey === noteKey}
										<div style="display: flex; align-items: center; gap: 6px;">
											<label class="field-group" style="margin: 0;">
												<span class="field-label" style="display: none;">Nota da Porta {port.port}</span>
												<input
													type="text"
													bind:value={editingNoteVal}
													class="input input-sm"
													style="width: 150px;"
													placeholder="Ex: Servidor NAS"
													onkeydown={(e) => e.key === 'Enter' && savePortNote($selectedSwitch, port.port)}
													data-testid="port-note-input-{port.port}"
												/>
											</label>
											<button
												type="button"
												onclick={() => savePortNote($selectedSwitch, port.port)}
												class="btn btn-primary btn-sm"
												data-testid="port-note-save-{port.port}"
											>
												Salvar
											</button>
											<button
												type="button"
												onclick={() => (editingNoteKey = '')}
												class="btn btn-secondary btn-sm"
											>
												✕
											</button>
										</div>
									{:else}
										<button
											type="button"
											onclick={() => startEditNote($selectedSwitch, port.port)}
											class="btn btn-ghost btn-sm"
											style="color: {noteVal ? 'var(--text-primary)' : 'var(--text-muted)'};"
											data-testid="port-note-edit-{port.port}"
										>
											<span>{noteVal || 'Adicionar rótulo...'}</span>
										</button>
									{/if}
								</td>

								<td>
									{#if port.is_up}
										<span class="badge badge-online mono">{port.speed}</span>
									{:else}
										<span class="badge badge-neutral mono">Link Down</span>
									{/if}
								</td>

								<td class="mono" style="color: var(--accent-hover); font-weight: 600;">
									↓ {formatRate(port.tx_rate || 0)}
								</td>

								<td class="mono" style="color: var(--status-online); font-weight: 600;">
									↑ {formatRate(port.rx_rate || 0)}
								</td>

								<td class="mono" style="color: var(--text-secondary); font-size: 11.5px;">
									<div>↓ {formatBytes(port.tx_bytes || 0)}</div>
									<div>↑ {formatBytes(port.rx_bytes || 0)}</div>
								</td>

								<td class="mono" style="color: var(--text-secondary); font-size: 11.5px;">
									{port.tx_packets || 0} / {port.rx_packets || 0}
								</td>

								<td>
									{#if portMacs.length > 0}
										<div style="display: flex; flex-wrap: wrap; gap: 4px;">
											{#each portMacs.slice(0, 2) as m}
												<span class="badge badge-neutral mono">
													{m.hostname || m.mac}
												</span>
											{/each}
											{#if portMacs.length > 2}
												<span class="badge badge-accent mono">+{portMacs.length - 2}</span>
											{/if}
										</div>
									{:else}
										<span style="color: var(--text-muted);">—</span>
									{/if}
								</td>

								<td style="text-align: right;">
									<button
										type="button"
										onclick={() => openPortInspector(port)}
										class="btn btn-secondary btn-sm"
										data-testid="inspect-port-btn-{port.port}"
									>
										Inspecionar
									</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		</section>
	{/if}
{/if}

<!-- Port Inspector & Bandwidth History Modal -->
{#if inspectedPort && $selectedSwitch}
	<div class="modal-backdrop" role="dialog" aria-modal="true" aria-label="Inspetor da Porta #{inspectedPort.port}" data-testid="port-inspector-modal">
		<div class="modal-panel">
			<div class="modal-header">
				<div style="display: flex; align-items: center; gap: 10px;">
					<span class="port-led {getPortLedClass(inspectedPort)}"></span>
					<div>
						<h3 class="page-title" style="font-size: 15px;">
							{$selectedSwitch.name} — Porta #{inspectedPort.port}
							{#if getPortNote($selectedSwitch, inspectedPort.port)}
								<span style="color: var(--accent-hover); font-weight: 400;">
									({getPortNote($selectedSwitch, inspectedPort.port)})
								</span>
							{/if}
						</h3>
						<p class="page-subtitle mono">
							Status: {inspectedPort.speed || 'Down'} &middot; TX: {formatRate(inspectedPort.tx_rate)} &middot; RX: {formatRate(inspectedPort.rx_rate)}
						</p>
					</div>
				</div>

				<button
					type="button"
					onclick={() => (inspectedPort = null)}
					class="btn btn-secondary btn-sm"
					data-testid="close-inspector-btn"
				>
					Fechar ✕
				</button>
			</div>

			<div class="modal-body">
				<div style="display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; flex-wrap: wrap; gap: 8px;">
					<div style="display: flex; align-items: center; gap: 16px; font-size: 12px;">
						<span style="color: var(--accent-hover); font-weight: 600;">■ Download (Switch TX)</span>
						<span style="color: var(--status-online); font-weight: 600;">■ Upload (Switch RX)</span>
					</div>

					<div class="segmented-control">
						{#each [{ id: 'live', label: 'Tempo Real' }, { id: '1h', label: '1 Hora' }, { id: '24h', label: '24 Horas' }] as tab}
							<button
								type="button"
								onclick={() => loadPortHistory(inspectedPort.port, tab.id)}
								class="segment-btn"
								class:active={historyPeriod === tab.id}
							>
								{tab.label}
							</button>
						{/each}
					</div>
				</div>

				<div style="background-color: var(--bg-inset); border: 1px solid var(--border-subtle); border-radius: var(--radius-md); padding: 16px; margin-bottom: 16px;">
					{#if loadingHistory}
						<div style="height: 160px; display: flex; align-items: center; justify-content: center; color: var(--text-secondary);">
							Carregando telemetria histórica...
						</div>
					{:else if !chartGeometry}
						<div style="height: 160px; display: flex; align-items: center; justify-content: center; color: var(--text-secondary);">
							Aguardando amostras de tráfego adicionais para plotar a série...
						</div>
					{:else}
						<div class="mono" style="display: flex; justify-content: space-between; font-size: 11px; color: var(--text-secondary); margin-bottom: 6px;">
							<span>Topo da escala: {formatRate(chartGeometry.maxVal)}</span>
							<span>{historyPoints.length} amostras</span>
						</div>
						<svg
							viewBox="0 0 {chartGeometry.width} {chartGeometry.height}"
							class="chart-svg"
							style="width: 100%; height: 170px; display: block;"
							role="img"
							aria-label="Gráfico de banda da porta"
						>
							<defs>
								<linearGradient id="dlGrad" x1="0" y1="0" x2="0" y2="1">
									<stop offset="0%" style="stop-color: var(--accent-primary); stop-opacity: 0.38;" />
									<stop offset="100%" style="stop-color: var(--accent-primary); stop-opacity: 0;" />
								</linearGradient>
								<linearGradient id="ulGrad" x1="0" y1="0" x2="0" y2="1">
									<stop offset="0%" style="stop-color: var(--status-1g); stop-opacity: 0.34;" />
									<stop offset="100%" style="stop-color: var(--status-1g); stop-opacity: 0;" />
								</linearGradient>
							</defs>
							<polygon points={chartGeometry.dlArea} fill="url(#dlGrad)" />
							<polygon points={chartGeometry.ulArea} fill="url(#ulGrad)" />
							<polyline fill="none" style="stroke: var(--accent-primary);" stroke-width="2" points={chartGeometry.dlPoints} />
							<polyline fill="none" style="stroke: var(--status-1g);" stroke-width="2" points={chartGeometry.ulPoints} />
						</svg>
					{/if}
				</div>

				{#if $selectedSwitch.supports_ddmi}
					<div style="padding-top: 12px; border-top: 1px solid var(--border-subtle);" data-testid="ddmi-section">
						<div style="display: flex; align-items: center; justify-content: space-between; margin-bottom: 10px;">
							<h4 class="kpi-label">Diagnóstico Óptico SFP+ (DDMI)</h4>
							<button type="button" onclick={loadTransceiverDiag} class="btn btn-secondary btn-sm">
								{loadingTransceiver ? 'Consultando...' : 'Atualizar DDMI'}
							</button>
						</div>

						{#if transceiverData.length === 0}
							<p style="color: var(--text-muted); font-size: 12px;">
								Nenhum transceptor óptico SFP+ DDMI ativo reportado pelo switch.
							</p>
						{:else}
							<div class="grid-2col">
								{#each transceiverData as d}
									<div style="background-color: var(--bg-inset); border: 1px solid var(--border-subtle); border-radius: var(--radius-md); padding: 12px;">
										<div class="mono" style="font-weight: 700; margin-bottom: 8px; color: var(--text-primary);">
											{d.port || 'SFP+'} {d.vendor ? `— ${d.vendor}` : ''}
										</div>
										<div class="mono" style="display: grid; grid-template-columns: 1fr 1fr; gap: 6px; font-size: 12px; color: var(--text-secondary);">
											<div>Temp: <strong style="color: var(--text-primary);">{d.temperature || 'N/A'}</strong></div>
											<div>Tensão: <strong style="color: var(--text-primary);">{d.voltage || 'N/A'}</strong></div>
											<div>Bias: <strong style="color: var(--text-primary);">{d.bias_current || d.bias || 'N/A'}</strong></div>
											<div>TX Power: <strong style="color: var(--status-online);">{d.tx_power || 'N/A'}</strong></div>
											<div>RX Power: <strong style="color: var(--accent-hover);">{d.rx_power || 'N/A'}</strong></div>
										</div>
									</div>
								{/each}
							</div>
						{/if}
					</div>
				{/if}
			</div>
		</div>
	</div>
{/if}

<!-- Switch MAC Address Table Modal -->
{#if showMacModal && $selectedSwitch}
	<div class="modal-backdrop" role="dialog" aria-modal="true" aria-label="Tabela MAC FDB" data-testid="mac-table-modal">
		<div class="modal-panel" style="max-width: 900px;">
			<div class="modal-header">
				<div>
					<h3 class="page-title" style="font-size: 15px;">Tabela MAC FDB — {$selectedSwitch.name}</h3>
					<p class="page-subtitle">
						{($selectedSwitch.mac_table || []).length} endereços aprendidos ({$selectedSwitch.ip})
					</p>
				</div>

				<div class="toolbar">
					<label class="field-group" style="margin: 0;">
						<span class="field-label" style="display: none;">Filtrar MAC</span>
						<input
							type="text"
							bind:value={macSearch}
							placeholder="Filtrar MAC, fabricante ou porta..."
							class="input input-sm"
							style="width: 220px;"
							data-testid="mac-modal-search"
						/>
					</label>
					<button
						type="button"
						onclick={refreshMacTable}
						disabled={refreshingMac}
						class="btn btn-primary btn-sm"
						data-testid="refresh-mac-btn"
					>
						{refreshingMac ? 'Consultando...' : 'Atualizar Agora'}
					</button>
					<button
						type="button"
						onclick={() => (showMacModal = false)}
						class="btn btn-secondary btn-sm"
						data-testid="close-mac-modal-btn"
					>
						Fechar ✕
					</button>
				</div>
			</div>

			<div class="modal-body">
				<div class="table-scroll">
					<table class="data-table">
						<thead>
							<tr>
								<th>Porta</th>
								<th>VLAN</th>
								<th>Endereço MAC</th>
								<th>Fabricante (OUI)</th>
								<th>Hostname</th>
								<th>Tipo</th>
							</tr>
						</thead>
						<tbody>
							{#each filteredMacs as entry}
								<tr>
									<td class="mono" style="font-weight: 600;">Porta #{entry.port}</td>
									<td class="mono" style="color: var(--text-secondary);">{entry.vlan}</td>
									<td class="mono" style="color: var(--accent-hover);">{entry.mac}</td>
									<td>{entry.vendor || 'Unknown Vendor'}</td>
									<td>
										{#if editingHostMac === entry.mac}
											<div style="display: flex; align-items: center; gap: 6px;">
												<label class="field-group" style="margin: 0;">
													<span class="field-label" style="display: none;">Hostname</span>
													<input
														type="text"
														bind:value={editingHostName}
														class="input input-sm"
														style="width: 140px;"
														onkeydown={(e) => e.key === 'Enter' && saveMacHostname(entry.mac)}
													/>
												</label>
												<button type="button" onclick={() => saveMacHostname(entry.mac)} class="btn btn-primary btn-sm">
													OK
												</button>
											</div>
										{:else}
											<button
												type="button"
												onclick={() => {
													editingHostMac = entry.mac;
													editingHostName = entry.hostname || '';
												}}
												class="btn btn-ghost btn-sm"
											>
												{entry.hostname || 'Definir nome...'}
											</button>
										{/if}
									</td>
									<td><span class="badge badge-neutral mono">{entry.type || 'Dynamic'}</span></td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</div>
		</div>
	</div>
{/if}
