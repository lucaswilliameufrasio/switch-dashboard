<script>
	import { onMount } from 'svelte';
	import { apiFetch } from '$lib/api.js';
	import { showToast } from '$lib/stores.js';

	let loading = $state(true);
	let rawNodes = $state([]);
	let rawEdges = $state([]);
	let savedPositions = $state({});

	let showClients = $state(true);
	let showOffline = $state(false);
	let selectedNode = $state(null);

	let zoom = $state(1);
	let panX = $state(0);
	let panY = $state(0);
	let isPanning = $state(false);
	let panStart = { x: 0, y: 0 };

	let draggingNodeId = $state(null);
	let nodePositions = $state({});

	async function loadTopology() {
		loading = true;
		try {
			const [topo, cfg] = await Promise.all([
				apiFetch('/api/topology'),
				apiFetch('/api/config/settings')
			]);
			rawNodes = (topo.nodes || []).map((n) => ({
				...n,
				label: n.name || n.label || n.id
			}));
			rawEdges = (topo.links || topo.edges || []).map((l) => ({
				...l,
				from: l.source || l.from,
				to: l.target || l.to,
				label: l.speed && l.speed !== 'Unknown' ? l.speed : l.source_port || ''
			}));
			savedPositions = cfg.map_positions || {};
			computeLayout();
		} catch (e) {
			showToast(e.message, 'error');
		} finally {
			loading = false;
		}
	}

	function computeLayout(forceReset = false) {
		const positions = forceReset ? {} : { ...savedPositions };
		const switches = rawNodes.filter((n) => n.type === 'switch' || n.type === 'internet');
		const infra = rawNodes.filter((n) => n.type !== 'switch' && n.type !== 'internet' && n.type !== 'client');
		const clients = rawNodes.filter((n) => n.type === 'client');

		const centerX = 560;

		switches.forEach((sw, i) => {
			if (!positions[sw.id]) {
				const spacing = 260;
				const startX = centerX - ((switches.length - 1) * spacing) / 2;
				positions[sw.id] = { x: startX + i * spacing, y: 120 };
			}
		});

		infra.forEach((inf, i) => {
			if (!positions[inf.id]) {
				const spacing = 200;
				const startX = centerX - ((infra.length - 1) * spacing) / 2;
				positions[inf.id] = { x: startX + i * spacing, y: 270 };
			}
		});

		clients.forEach((cl, i) => {
			if (!positions[cl.id]) {
				const cols = Math.max(5, Math.ceil(Math.sqrt(clients.length * 2)));
				const col = i % cols;
				const row = Math.floor(i / cols);
				const spacingX = 165;
				const startX = centerX - ((cols - 1) * spacingX) / 2;
				positions[cl.id] = { x: startX + col * spacingX, y: 420 + row * 105 };
			}
		});

		nodePositions = positions;
	}

	async function saveLayout() {
		try {
			await apiFetch('/api/config/settings', {
				method: 'POST',
				body: JSON.stringify({ map_positions: nodePositions })
			});
			savedPositions = { ...nodePositions };
			showToast('Posições da topologia salvas.', 'success');
		} catch (e) {
			showToast(e.message, 'error');
		}
	}

	async function resetLayout() {
		computeLayout(true);
		await saveLayout();
	}

	let visibleNodes = $derived.by(() => {
		return rawNodes.filter((n) => {
			if (!showClients && n.type === 'client') return false;
			if (!showOffline && (n.status === 'Offline' || n.status === 'offline')) return false;
			return true;
		});
	});

	let visibleNodeIds = $derived(new Set(visibleNodes.map((n) => n.id)));

	let visibleEdges = $derived.by(() => {
		return rawEdges.filter((e) => visibleNodeIds.has(e.from) && visibleNodeIds.has(e.to));
	});

	function getEdgeStroke(edge) {
		if (edge.type === 'uplink') return 'var(--accent-hover)';
		if (edge.type === 'infra') return 'var(--status-purple)';
		return 'var(--border-strong)';
	}

	function handleSvgMouseDown(e) {
		if (e.target.closest('.topo-node')) return;
		isPanning = true;
		panStart = { x: e.clientX - panX, y: e.clientY - panY };
	}

	function handleSvgMouseMove(e) {
		if (draggingNodeId) {
			const svgRect = e.currentTarget.getBoundingClientRect();
			const x = (e.clientX - svgRect.left - panX) / zoom;
			const y = (e.clientY - svgRect.top - panY) / zoom;
			nodePositions = {
				...nodePositions,
				[draggingNodeId]: { x: Math.round(x), y: Math.round(y) }
			};
			return;
		}
		if (isPanning) {
			panX = e.clientX - panStart.x;
			panY = e.clientY - panStart.y;
		}
	}

	function handleSvgMouseUp() {
		isPanning = false;
		draggingNodeId = null;
	}

	function handleWheel(e) {
		e.preventDefault();
		const delta = e.deltaY < 0 ? 0.1 : -0.1;
		zoom = Math.min(2.2, Math.max(0.4, Number((zoom + delta).toFixed(2))));
	}

	onMount(loadTopology);
</script>

<div class="page-header">
	<div>
		<h1 class="page-title">Mapa de Topologia L2</h1>
		<p class="page-subtitle">
			Descoberta passiva via tabelas MAC FDB e uplinks configurados — arraste os nós para organizar
		</p>
	</div>

	<div class="toolbar">
		<label class="btn btn-secondary btn-sm" style="cursor: pointer;">
			<input type="checkbox" bind:checked={showClients} data-testid="topo-toggle-clients" />
			<span>Exibir Clientes ({rawNodes.filter((n) => n.type === 'client').length})</span>
		</label>

		<label class="btn btn-secondary btn-sm" style="cursor: pointer;">
			<input type="checkbox" bind:checked={showOffline} data-testid="topo-toggle-offline" />
			<span>Incluir Offline</span>
		</label>

		<button type="button" onclick={() => (zoom = Math.min(2.2, zoom + 0.15))} class="btn btn-secondary btn-sm" title="Aumentar Zoom">
			+
		</button>
		<button type="button" onclick={() => (zoom = Math.max(0.4, zoom - 0.15))} class="btn btn-secondary btn-sm" title="Reduzir Zoom">
			−
		</button>
		<button
			type="button"
			onclick={() => {
				zoom = 1;
				panX = 0;
				panY = 0;
			}}
			class="btn btn-secondary btn-sm"
		>
			Centralizar
		</button>
		<button type="button" onclick={resetLayout} class="btn btn-secondary btn-sm" data-testid="topo-autolayout-btn">
			Auto-Layout
		</button>
		<button type="button" onclick={saveLayout} class="btn btn-primary btn-sm" data-testid="topo-save-btn">
			Salvar Layout
		</button>
	</div>
</div>

<div
	class="card"
	style="position: relative; overflow: hidden; height: 70vh; background-color: var(--bg-inset); user-select: none;"
	data-testid="topology-canvas-card"
>
	{#if loading}
		<div class="empty-state">
			<p style="color: var(--text-secondary);">Construindo grafo de topologia L2...</p>
		</div>
	{:else if visibleNodes.length === 0}
		<div class="empty-state">
			<p style="color: var(--text-secondary);">Nenhum nó de topologia disponível.</p>
		</div>
	{:else}
		<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
		<svg
			width="1200"
			height="700"
			viewBox="0 0 1200 700"
			class="topology-svg"
			style="width: 100%; height: 100%; display: block; cursor: grab;"
			onmousedown={handleSvgMouseDown}
			onmousemove={handleSvgMouseMove}
			onmouseup={handleSvgMouseUp}
			onwheel={handleWheel}
			role="img"
			aria-label="Mapa de Topologia da Rede"
			data-testid="topology-svg"
		>
			<defs>
				<pattern id="topoGrid" width="32" height="32" patternUnits="userSpaceOnUse">
					<circle cx="1" cy="1" r="1" fill="var(--border-subtle)" />
				</pattern>
			</defs>
			<rect width="100%" height="100%" fill="url(#topoGrid)" />

			<g transform="translate({panX}, {panY}) scale({zoom})">
				{#each visibleEdges as edge}
					{@const fromPos = nodePositions[edge.from] || { x: 0, y: 0 }}
					{@const toPos = nodePositions[edge.to] || { x: 0, y: 0 }}
					{@const midX = (fromPos.x + toPos.x) / 2}
					{@const midY = (fromPos.y + toPos.y) / 2}
					<g>
						<line
							x1={fromPos.x}
							y1={fromPos.y}
							x2={toPos.x}
							y2={toPos.y}
							stroke={getEdgeStroke(edge)}
							stroke-width={edge.type === 'uplink' ? 2.5 : 1.5}
						/>
						{#if edge.label}
							<rect
								x={midX - 36}
								y={midY - 10}
								width="72"
								height="18"
								rx="4"
								fill="var(--bg-surface)"
								stroke="var(--border-subtle)"
							/>
							<text
								x={midX}
								y={midY + 3}
								text-anchor="middle"
								fill="var(--text-secondary)"
								font-size="9"
								font-family="var(--font-mono)"
							>
								{edge.label}
							</text>
						{/if}
					</g>
				{/each}

				{#each visibleNodes as node}
					{@const pos = nodePositions[node.id] || { x: 300, y: 200 }}
					{@const isSwitch = node.type === 'switch'}
					{@const isInfra = node.type !== 'switch' && node.type !== 'client'}
					<!-- svelte-ignore a11y_no_static_element_interactions -->
					<g
						class="topo-node"
						style="cursor: pointer;"
						transform="translate({pos.x}, {pos.y})"
						data-testid="topo-node-{node.id}"
						onmousedown={(e) => {
							e.stopPropagation();
							draggingNodeId = node.id;
							selectedNode = node;
						}}
					>
						<rect
							x={isSwitch ? -80 : -66}
							y={isSwitch ? -30 : -24}
							width={isSwitch ? 160 : 132}
							height={isSwitch ? 60 : 48}
							rx="8"
							fill={isSwitch ? 'var(--bg-elevated)' : 'var(--bg-surface)'}
							stroke={selectedNode?.id === node.id
								? 'var(--accent-hover)'
								: isSwitch
									? 'var(--accent-primary)'
									: isInfra
										? 'var(--status-purple)'
										: 'var(--border-strong)'}
							stroke-width={isSwitch || selectedNode?.id === node.id ? '2' : '1.2'}
						/>

						<circle
							cx={isSwitch ? -64 : -52}
							cy={isSwitch ? -10 : -7}
							r="4"
							fill={node.status === 'Offline' || node.status === 'offline'
								? 'var(--status-danger)'
								: 'var(--status-online)'}
						/>

						<text
							x={isSwitch ? -52 : -40}
							y={isSwitch ? -6 : -3}
							fill="var(--text-primary)"
							font-size={isSwitch ? '11' : '10'}
							font-weight="600"
						>
							{(node.label || node.id).slice(0, 18)}
						</text>

						<text
							x={isSwitch ? -64 : -52}
							y={isSwitch ? 13 : 12}
							fill="var(--text-secondary)"
							font-size="9"
							font-family="var(--font-mono)"
						>
							{(node.ip || node.mac || node.vendor || node.type).slice(0, 20)}
						</text>
					</g>
				{/each}
			</g>
		</svg>

		<div
			style="position: absolute; bottom: 14px; left: 14px; background-color: var(--bg-glass); border: 1px solid var(--border-subtle); border-radius: var(--radius-md); padding: 8px 14px; display: flex; gap: 16px; font-size: 11.5px; color: var(--text-secondary);"
		>
			<span class="legend-item"><span class="port-led led-10g"></span> Switch Gerenciado</span>
			<span class="legend-item"><span class="port-led led-25g"></span> Cliente Ativo</span>
		</div>

		{#if selectedNode}
			<div
				class="card"
				style="position: absolute; top: 14px; right: 14px; width: 270px; padding: 14px;"
				data-testid="topo-node-inspector"
			>
				<div style="display: flex; align-items: center; justify-content: space-between; padding-bottom: 8px; margin-bottom: 10px; border-bottom: 1px solid var(--border-subtle);">
					<span class="badge badge-accent mono">{selectedNode.type}</span>
					<button type="button" onclick={() => (selectedNode = null)} class="btn btn-ghost btn-sm">✕</button>
				</div>
				<div style="display: flex; flex-direction: column; gap: 8px; font-size: 12px;">
					<div>
						<div style="color: var(--text-secondary);">Nome</div>
						<div style="font-weight: 600;">{selectedNode.label}</div>
					</div>
					{#if selectedNode.ip}
						<div>
							<div style="color: var(--text-secondary);">Endereço IP</div>
							<div class="mono">{selectedNode.ip}</div>
						</div>
					{/if}
					{#if selectedNode.mac}
						<div>
							<div style="color: var(--text-secondary);">Endereço MAC</div>
							<div class="mono" style="color: var(--accent-hover);">{selectedNode.mac}</div>
						</div>
					{/if}
					{#if selectedNode.vendor}
						<div>
							<div style="color: var(--text-secondary);">Fabricante</div>
							<div>{selectedNode.vendor}</div>
						</div>
					{/if}
					{#if selectedNode.last_seen_ip}
						<div>
							<div style="color: var(--text-secondary);">Conectado em</div>
							<div class="mono" style="color: var(--status-online);">
								{selectedNode.last_seen_ip} — Porta #{selectedNode.last_seen_port}
							</div>
						</div>
					{/if}
				</div>
			</div>
		{/if}
	{/if}
</div>
