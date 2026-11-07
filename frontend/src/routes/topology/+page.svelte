<script>
  import { onMount } from 'svelte';
  import { apiFetch } from '$lib/api.js';
  import { showToast } from '$lib/stores.js';

  let loading = $state(true);
  let rawNodes = $state([]);
  let rawEdges = $state([]);
  let savedPositions = $state({});

  // Filters
  let showClients = $state(true);
  let showOffline = $state(false);
  let selectedNode = $state(null);

  // Pan & Zoom state
  let zoom = $state(1);
  let panX = $state(0);
  let panY = $state(0);
  let isPanning = $state(false);
  let panStart = { x: 0, y: 0 };

  // Node dragging state
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

    const centerX = 640;

    switches.forEach((sw, i) => {
      if (!positions[sw.id]) {
        const spacing = 260;
        const startX = centerX - ((switches.length - 1) * spacing) / 2;
        positions[sw.id] = { x: startX + i * spacing, y: 140 };
      }
    });

    infra.forEach((inf, i) => {
      if (!positions[inf.id]) {
        const spacing = 200;
        const startX = centerX - ((infra.length - 1) * spacing) / 2;
        positions[inf.id] = { x: startX + i * spacing, y: 310 };
      }
    });

    clients.forEach((cl, i) => {
      if (!positions[cl.id]) {
        const cols = Math.max(6, Math.ceil(Math.sqrt(clients.length * 2)));
        const col = i % cols;
        const row = Math.floor(i / cols);
        const spacingX = 165;
        const startX = centerX - ((cols - 1) * spacingX) / 2;
        positions[cl.id] = { x: startX + col * spacingX, y: 470 + row * 110 };
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
      showToast('Posições da topologia salvas', 'success');
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

  function getEdgeColor(edge) {
    if (edge.type === 'uplink') return '#006fff';
    if (edge.type === 'infra') return '#a855f7';
    return '#2e3646';
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

<div class="flex flex-wrap items-center justify-between gap-3 mb-4">
  <div>
    <h2 class="text-base font-bold text-white">Mapa de Topologia L2</h2>
    <p class="text-xs text-[#8c96a8]">
      Descoberta passiva via tabelas MAC FDB e uplinks configurados — arraste os nós para organizar
    </p>
  </div>

  <div class="flex flex-wrap items-center gap-2">
    <label class="flex items-center gap-2 text-xs text-[#8c96a8] bg-[#151820] px-3 py-1.5 rounded-lg border border-[#232936] cursor-pointer">
      <input type="checkbox" bind:checked={showClients} class="accent-[#006fff]" />
      <span>Exibir Clientes ({rawNodes.filter((n) => n.type === 'client').length})</span>
    </label>

    <label class="flex items-center gap-2 text-xs text-[#8c96a8] bg-[#151820] px-3 py-1.5 rounded-lg border border-[#232936] cursor-pointer">
      <input type="checkbox" bind:checked={showOffline} class="accent-[#006fff]" />
      <span>Incluir Offline</span>
    </label>

    <button onclick={() => (zoom = Math.min(2.2, zoom + 0.15))} class="unifi-btn unifi-btn-secondary py-1.5 px-2.5" title="Zoom In">
      +
    </button>
    <button onclick={() => (zoom = Math.max(0.4, zoom - 0.15))} class="unifi-btn unifi-btn-secondary py-1.5 px-2.5" title="Zoom Out">
      −
    </button>
    <button
      onclick={() => {
        zoom = 1;
        panX = 0;
        panY = 0;
      }}
      class="unifi-btn unifi-btn-secondary py-1.5 px-2.5"
    >
      Centralizar
    </button>
    <button onclick={resetLayout} class="unifi-btn unifi-btn-secondary py-1.5">
      Auto-Layout
    </button>
    <button onclick={saveLayout} class="unifi-btn unifi-btn-primary py-1.5">
      Salvar Layout
    </button>
  </div>
</div>

<!-- Topology Canvas -->
<div class="unifi-card relative overflow-hidden h-[72vh] bg-[#090b10] select-none">
  {#if loading}
    <div class="absolute inset-0 flex items-center justify-center text-xs text-[#8c96a8]">
      Construindo grafo de topologia L2...
    </div>
  {:else if visibleNodes.length === 0}
    <div class="absolute inset-0 flex items-center justify-center text-xs text-[#8c96a8]">
      Nenhum nó de topologia disponível.
    </div>
  {:else}
    <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
    <svg
      class="w-full h-full cursor-grab active:cursor-grabbing"
      onmousedown={handleSvgMouseDown}
      onmousemove={handleSvgMouseMove}
      onmouseup={handleSvgMouseUp}
      onwheel={handleWheel}
      role="img"
      aria-label="Mapa de Topologia da Rede"
    >
      <defs>
        <pattern id="unifiGrid" width="32" height="32" patternUnits="userSpaceOnUse">
          <circle cx="1" cy="1" r="1" fill="#1e2433" />
        </pattern>
      </defs>
      <rect width="100%" height="100%" fill="url(#unifiGrid)" />

      <g transform="translate({panX}, {panY}) scale({zoom})">
        <!-- Edges -->
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
              stroke={getEdgeColor(edge)}
              stroke-width={edge.type === 'uplink' ? 2.5 : 1.5}
            />
            {#if edge.label}
              <rect
                x={midX - 34}
                y={midY - 10}
                width="68"
                height="18"
                rx="4"
                fill="#111318"
                stroke="#232936"
              />
              <text
                x={midX}
                y={midY + 2}
                text-anchor="middle"
                fill="#8c96a8"
                font-size="9"
                font-family="JetBrains Mono, monospace"
              >
                {edge.label}
              </text>
            {/if}
          </g>
        {/each}

        <!-- Nodes -->
        {#each visibleNodes as node}
          {@const pos = nodePositions[node.id] || { x: 300, y: 200 }}
          {@const isSwitch = node.type === 'switch'}
          {@const isInfra = node.type !== 'switch' && node.type !== 'client'}
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <g
            class="topo-node cursor-pointer"
            transform="translate({pos.x}, {pos.y})"
            onmousedown={(e) => {
              e.stopPropagation();
              draggingNodeId = node.id;
              selectedNode = node;
            }}
          >
            <rect
              x={isSwitch ? -78 : -64}
              y={isSwitch ? -30 : -24}
              width={isSwitch ? 156 : 128}
              height={isSwitch ? 60 : 48}
              rx="10"
              fill={isSwitch ? '#161b26' : '#12151e'}
              stroke={selectedNode?.id === node.id
                ? '#006fff'
                : isSwitch
                  ? '#006fff'
                  : isInfra
                    ? '#a855f7'
                    : '#262d3d'}
              stroke-width={isSwitch || selectedNode?.id === node.id ? '2' : '1.2'}
            />

            <circle
              cx={isSwitch ? -62 : -50}
              cy={isSwitch ? -12 : -8}
              r="4"
              fill={node.status === 'Offline' || node.status === 'offline' ? '#ff3d71' : '#00d68f'}
            />

            <text
              x={isSwitch ? -50 : -38}
              y={isSwitch ? -8 : -4}
              fill="#f0f3f8"
              font-size={isSwitch ? '11' : '10'}
              font-weight="600"
            >
              {(node.label || node.id).slice(0, 18)}
            </text>

            <text
              x={isSwitch ? -62 : -50}
              y={isSwitch ? 12 : 12}
              fill="#8c96a8"
              font-size="9"
              font-family="JetBrains Mono, monospace"
            >
              {(node.ip || node.mac || node.vendor || node.type).slice(0, 20)}
            </text>
          </g>
        {/each}
      </g>
    </svg>

    <!-- Legend Bottom Left -->
    <div class="absolute bottom-4 left-4 bg-[#111318]/90 backdrop-blur border border-[#232936] rounded-lg px-3.5 py-2.5 flex items-center gap-4 text-[11px] text-[#8c96a8]">
      <span class="flex items-center gap-1.5">
        <span class="w-2.5 h-2.5 rounded-sm bg-[#006fff]"></span> Switch Gerenciado
      </span>
      <span class="flex items-center gap-1.5">
        <span class="w-2.5 h-2.5 rounded-sm bg-[#a855f7]"></span> Infra / Hub
      </span>
      <span class="flex items-center gap-1.5">
        <span class="w-2.5 h-2.5 rounded-sm bg-[#00d68f]"></span> Cliente Ativo
      </span>
    </div>

    <!-- Selected Node Inspector Drawer -->
    {#if selectedNode}
      <div class="absolute top-4 right-4 w-72 bg-[#111318]/95 backdrop-blur border border-[#2e3646] rounded-xl p-4 shadow-2xl">
        <div class="flex items-center justify-between pb-2.5 mb-3 border-b border-[#232936]">
          <span class="text-xs font-bold uppercase tracking-wider text-[#4da3ff]">
            {selectedNode.type}
          </span>
          <button onclick={() => (selectedNode = null)} class="text-xs text-[#8c96a8] hover:text-white">✕</button>
        </div>
        <div class="space-y-2 text-xs">
          <div>
            <div class="text-[#8c96a8]">Nome</div>
            <div class="font-semibold text-white">{selectedNode.label}</div>
          </div>
          {#if selectedNode.ip}
            <div>
              <div class="text-[#8c96a8]">Endereço IP</div>
              <div class="mono text-white">{selectedNode.ip}</div>
            </div>
          {/if}
          {#if selectedNode.mac}
            <div>
              <div class="text-[#8c96a8]">Endereço MAC</div>
              <div class="mono text-[#4da3ff]">{selectedNode.mac}</div>
            </div>
          {/if}
          {#if selectedNode.vendor}
            <div>
              <div class="text-[#8c96a8]">Fabricante</div>
              <div class="text-white">{selectedNode.vendor}</div>
            </div>
          {/if}
          {#if selectedNode.model}
            <div>
              <div class="text-[#8c96a8]">Modelo</div>
              <div class="mono text-white">{selectedNode.model}</div>
            </div>
          {/if}
          {#if selectedNode.last_seen_ip}
            <div>
              <div class="text-[#8c96a8]">Conectado em</div>
              <div class="mono text-[#00d68f]">{selectedNode.last_seen_ip} — Porta #{selectedNode.last_seen_port}</div>
            </div>
          {/if}
        </div>
      </div>
    {/if}
  {/if}
</div>
