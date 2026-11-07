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

  // Inspector & Modal States
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

  // Derived aggregates across all switches
  let summary = $derived.by(() => {
    const switches = $dashboardState.switches || [];
    let onlineSwitches = 0;
    let totalPorts = 0;
    let activePorts = 0;
    let totalRxRate = 0;
    let totalTxRate = 0;
    let totalMacs = 0;

    for (const sw of switches) {
      if (sw.status === 'Online') onlineSwitches++;
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
    // Horaco 2.5G + 10G switches usually have SFP+ on the last 1-2 ports (e.g. port 6 or ports 5-6 / 9-10)
    if (totalPorts === 6 && port.port >= 5) return true;
    if (totalPorts === 9 && port.port >= 9) return true;
    if (totalPorts === 10 && port.port >= 9) return true;
    return false;
  }

  function getPortNote(sw, portNum) {
    if (!sw) return '';
    const notes = $dashboardState.notes || {};
    return notes[`${sw.name}_${portNum}`] || notes[`${sw.ip}_${portNum}`] || '';
  }

  function startEditNote(sw, portNum) {
    const key = `${sw.name}_${portNum}`;
    editingNoteKey = key;
    editingNoteVal = getPortNote(sw, portNum);
  }

  async function savePortNote(sw, portNum) {
    const key = `${sw.name}_${portNum}`;
    try {
      await apiFetch('/api/notes', {
        method: 'POST',
        body: JSON.stringify({ key, note: editingNoteVal })
      });
      await refreshDashboardData();
      editingNoteKey = '';
      showToast('Nota da porta salva', 'success');
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
    } catch (e) {
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
      showToast(`Tabela MAC atualizada (${(res.mac_table || []).length} entradas)`, 'success');
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
      showToast('Hostname atualizado', 'success');
    } catch (e) {
      showToast(e.message, 'error');
    }
  }

  // SVG Chart helper for Port Inspector
  let chartGeometry = $derived.by(() => {
    const pts = historyPoints || [];
    if (pts.length < 2) return null;
    const width = 620;
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
        String(m.port).includes(q)
    );
  });
</script>

<!-- Top KPI Cards -->
<div class="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-6">
  <div class="unifi-card p-4">
    <div class="flex items-center justify-between mb-2">
      <span class="text-xs font-medium uppercase tracking-wider text-[#8c96a8]">Switches Online</span>
      <span class="w-2 h-2 rounded-full {summary.onlineSwitches === summary.totalSwitches && summary.totalSwitches > 0 ? 'bg-[#00d68f]' : 'bg-[#ffaa00]'}"></span>
    </div>
    <div class="text-2xl font-bold text-white mono">
      {summary.onlineSwitches} <span class="text-sm font-normal text-[#8c96a8]">/ {summary.totalSwitches}</span>
    </div>
    <div class="text-xs text-[#596375] mt-1">Switches gerenciados via HTTP/CGI</div>
  </div>

  <div class="unifi-card p-4">
    <div class="flex items-center justify-between mb-2">
      <span class="text-xs font-medium uppercase tracking-wider text-[#8c96a8]">Portas Ativas</span>
      <span class="text-xs font-semibold text-[#006fff] mono">
        {summary.totalPorts ? Math.round((summary.activePorts / summary.totalPorts) * 100) : 0}%
      </span>
    </div>
    <div class="text-2xl font-bold text-white mono">
      {summary.activePorts} <span class="text-sm font-normal text-[#8c96a8]">/ {summary.totalPorts}</span>
    </div>
    <div class="text-xs text-[#596375] mt-1">Links RJ45 2.5GbE & SFP+ 10GbE</div>
  </div>

  <div class="unifi-card p-4">
    <div class="flex items-center justify-between mb-2">
      <span class="text-xs font-medium uppercase tracking-wider text-[#8c96a8]">Throughput Agregado</span>
      <span class="text-[11px] font-semibold text-[#00d68f] mono">LIVE</span>
    </div>
    <div class="flex items-baseline gap-3">
      <div>
        <span class="text-[11px] text-[#006fff] mr-1">↓</span>
        <span class="text-lg font-bold text-white mono">{formatRate(summary.totalRxRate)}</span>
      </div>
      <div>
        <span class="text-[11px] text-[#00d68f] mr-1">↑</span>
        <span class="text-lg font-bold text-white mono">{formatRate(summary.totalTxRate)}</span>
      </div>
    </div>
    <div class="text-xs text-[#596375] mt-1">Tráfego em tempo real nas portas</div>
  </div>

  <div class="unifi-card p-4">
    <div class="flex items-center justify-between mb-2">
      <span class="text-xs font-medium uppercase tracking-wider text-[#8c96a8]">Endereços MAC FDB</span>
      <span class="text-xs text-[#8c96a8] mono">L2</span>
    </div>
    <div class="text-2xl font-bold text-white mono">{summary.totalMacs}</div>
    <div class="text-xs text-[#596375] mt-1">Dispositivos aprendidos nas tabelas</div>
  </div>
</div>

{#if ($dashboardState.switches || []).length === 0}
  <div class="unifi-card p-10 text-center">
    <div class="w-12 h-12 rounded-full bg-[#006fff]/15 text-[#006fff] flex items-center justify-center mx-auto mb-3">
      <svg class="w-6 h-6" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <rect x="2" y="6" width="20" height="12" rx="2" />
        <path d="M6 10h.01M10 10h.01M14 10h.01M18 10h.01" />
      </svg>
    </div>
    <h2 class="text-base font-semibold text-white">Nenhum Switch Configurado</h2>
    <p class="text-xs text-[#8c96a8] mt-1 max-w-md mx-auto">
      Adicione seus switches Horaco, KeepLink ou RTLPlayground na aba Settings para visualizar o chassi virtual, telemetria de portas e topologia.
    </p>
    <a href="/settings" class="unifi-btn unifi-btn-primary mt-4 inline-flex">Configurar Switches</a>
  </div>
{:else}
  <!-- Switch Selector Tabs -->
  <div class="flex flex-wrap items-center justify-between gap-3 mb-5">
    <div class="flex flex-wrap items-center gap-2">
      {#each $dashboardState.switches || [] as sw, idx}
        <button
          onclick={() => {
            activeSwitchIdx.set(idx);
            inspectedPort = null;
          }}
          class="px-3.5 py-2 rounded-lg text-xs font-medium transition-all flex items-center gap-2.5 border {$activeSwitchIdx === idx
            ? 'bg-[#006fff]/15 text-white border-[#006fff]'
            : 'bg-[#151820] text-[#8c96a8] border-[#232936] hover:text-white hover:border-[#343d52]'}"
        >
          <span class="w-2 h-2 rounded-full {sw.status === 'Online' ? 'bg-[#00d68f]' : 'bg-[#ff3d71]'}"></span>
          <span>{sw.name}</span>
          <span class="text-[11px] text-[#596375] mono">{sw.ip}</span>
        </button>
      {/each}
    </div>

    {#if $selectedSwitch}
      <div class="flex items-center gap-2">
        {#if $selectedSwitch.supports_mac}
          <button onclick={() => (showMacModal = true)} class="unifi-btn unifi-btn-secondary">
            <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M4 6h16M4 12h16M4 18h10" />
            </svg>
            Tabela MAC ({($selectedSwitch.mac_table || []).length})
          </button>
        {/if}
        {#if $selectedSwitch.supports_ddmi}
          <button
            onclick={() => {
              if ($selectedSwitch.ports?.length) {
                openPortInspector($selectedSwitch.ports[$selectedSwitch.ports.length - 1]);
              }
            }}
            class="unifi-btn unifi-btn-secondary"
          >
            Diagnóstico SFP+ DDMI
          </button>
        {/if}
        <a
          href="http://{$selectedSwitch.ip}"
          target="_blank"
          rel="noopener noreferrer"
          class="unifi-btn unifi-btn-secondary"
        >
          Web UI ↗
        </a>
      </div>
    {/if}
  </div>

  {#if $selectedSwitch}
    <!-- UniFi Virtual Rack-Mount Switch Chassis -->
    <div class="switch-chassis p-5 mb-6">
      <div class="flex flex-wrap items-center justify-between gap-4 mb-4 pb-3 border-b border-[#232936]">
        <div class="flex items-center gap-3">
          <div class="w-2.5 h-2.5 rounded-full {$selectedSwitch.status === 'Online' ? 'bg-[#00d68f] shadow-[0_0_8px_#00d68f]' : 'bg-[#ff3d71]'}"></div>
          <div>
            <div class="flex items-center gap-2">
              <h2 class="text-sm font-bold text-white tracking-wide">{$selectedSwitch.name}</h2>
              {#if $selectedSwitch.model}
                <span class="unifi-badge bg-[#006fff]/15 text-[#4da3ff] border border-[#006fff]/30 mono">
                  {$selectedSwitch.model}
                </span>
              {/if}
              {#if $selectedSwitch.Status !== 'Online' && $selectedSwitch.status !== 'Online'}
                <span class="unifi-badge bg-[#ff3d71]/15 text-[#ff3d71]">OFFLINE</span>
              {/if}
            </div>
            <div class="text-[11px] text-[#8c96a8] mono mt-0.5 flex flex-wrap items-center gap-3">
              <span>IP: {$selectedSwitch.ip}</span>
              {#if $selectedSwitch.mac_address}
                <span>MAC: {$selectedSwitch.mac_address}</span>
              {/if}
              {#if $selectedSwitch.firmware}
                <span>FW: {$selectedSwitch.firmware}</span>
              {/if}
              {#if $selectedSwitch.response_time_ms}
                <span>RTT: {$selectedSwitch.response_time_ms} ms</span>
              {/if}
            </div>
          </div>
        </div>

        <!-- Speed Legend -->
        <div class="flex flex-wrap items-center gap-4 text-[11px] text-[#8c96a8]">
          <div class="flex items-center gap-1.5">
            <span class="port-led led-10g"></span>
            <span>10 GbE SFP+</span>
          </div>
          <div class="flex items-center gap-1.5">
            <span class="port-led led-25g"></span>
            <span>2.5 GbE</span>
          </div>
          <div class="flex items-center gap-1.5">
            <span class="port-led led-1g"></span>
            <span>1 GbE</span>
          </div>
          <div class="flex items-center gap-1.5">
            <span class="port-led led-100m"></span>
            <span>100M / 10M</span>
          </div>
          <div class="flex items-center gap-1.5">
            <span class="port-led led-down"></span>
            <span>Sem Link</span>
          </div>
        </div>
      </div>

      <!-- Physical Port Faceplate -->
      <div class="flex flex-wrap items-center gap-3 py-2">
        {#each $selectedSwitch.ports || [] as port}
          {@const sfp = isSfpPort(port, ($selectedSwitch.ports || []).length)}
          {@const note = getPortNote($selectedSwitch, port.port)}
          <button
            onclick={() => openPortInspector(port)}
            title="Porta {port.port}: {port.speed} {note ? `(${note})` : ''} — Clique para inspecionar tráfego"
            class="port-socket {sfp ? 'sfp-cage' : ''} {port.is_up ? 'active-port' : ''} {inspectedPort?.port === port.port ? 'ring-2 ring-[#006fff]' : ''} w-20 p-2.5 flex flex-col items-center gap-1.5 text-left group"
          >
            <div class="w-full flex items-center justify-between">
              <span class="text-[10px] font-bold text-[#8c96a8] mono">#{port.port}</span>
              <span class="port-led {getPortLedClass(port)}"></span>
            </div>

            <!-- Metallic Port Cutout -->
            <div class="w-12 h-8 rounded bg-[#07080c] border border-[#232936] flex flex-col items-center justify-center relative">
              <span class="text-[9px] font-bold uppercase tracking-tighter {port.is_up ? 'text-white' : 'text-[#596375]'} mono">
                {sfp ? 'SFP+' : 'RJ45'}
              </span>
              {#if port.is_up}
                <span class="text-[8px] text-[#00d68f] mono leading-none mt-0.5">
                  {(port.speed || '').replace('Full', '').replace('Half', '').trim()}
                </span>
              {/if}
            </div>

            <div class="w-full text-center truncate text-[10px] text-[#8c96a8] group-hover:text-white">
              {note || (port.is_up ? port.speed : 'Down')}
            </div>
          </button>
        {/each}
      </div>
    </div>

    <!-- Port Telemetry Table -->
    <div class="unifi-card overflow-hidden">
      <div class="px-5 py-3.5 border-b border-[#232936] flex items-center justify-between">
        <div>
          <h3 class="text-sm font-semibold text-white">Telemetria de Portas — {$selectedSwitch.name}</h3>
          <p class="text-xs text-[#8c96a8]">Clique em qualquer porta para abrir o gráfico histórico e diagnóstico óptico</p>
        </div>
      </div>

      <div class="overflow-x-auto">
        <table class="unifi-table">
          <thead>
            <tr>
              <th>Porta</th>
              <th>Descrição / Nota</th>
              <th>Status / Velocidade</th>
              <th>Download (Switch TX)</th>
              <th>Upload (Switch RX)</th>
              <th>Total Transferido</th>
              <th>Erros (TX/RX)</th>
              <th>Dispositivos Conectados</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {#each $selectedSwitch.ports || [] as port}
              {@const noteKey = `${$selectedSwitch.name}_${port.port}`}
              {@const noteVal = getPortNote($selectedSwitch, port.port)}
              {@const portMacs = ($selectedSwitch.mac_table || []).filter((m) => m.port === port.port)}
              <tr class="hover:bg-[#1b202b]/60 transition-colors">
                <td class="mono font-semibold text-white">
                  <div class="flex items-center gap-2">
                    <span class="port-led {getPortLedClass(port)}"></span>
                    <span>Porta {port.port}</span>
                    {#if $selectedSwitch.uplink_ports?.[String(port.port)]}
                      <span class="unifi-badge bg-[#a855f7]/15 text-[#c084fc] border border-[#a855f7]/30">UPLINK</span>
                    {/if}
                  </div>
                </td>

                <!-- Inline Editable Port Note -->
                <td>
                  {#if editingNoteKey === noteKey}
                    <div class="flex items-center gap-1.5">
                      <input
                        type="text"
                        bind:value={editingNoteVal}
                        class="unifi-input py-1 px-2 text-xs w-40"
                        placeholder="Ex: Proxmox Node 1"
                        onkeydown={(e) => e.key === 'Enter' && savePortNote($selectedSwitch, port.port)}
                      />
                      <button onclick={() => savePortNote($selectedSwitch, port.port)} class="unifi-btn unifi-btn-primary py-1 px-2 text-xs">
                        Salvar
                      </button>
                      <button onclick={() => (editingNoteKey = '')} class="unifi-btn unifi-btn-secondary py-1 px-2 text-xs">
                        ✕
                      </button>
                    </div>
                  {:else}
                    <button
                      onclick={() => startEditNote($selectedSwitch, port.port)}
                      class="text-left text-xs {noteVal ? 'text-[#f0f3f8]' : 'text-[#596375] italic'} hover:text-[#4da3ff] flex items-center gap-1.5"
                    >
                      <span>{noteVal || 'Adicionar rótulo...'}</span>
                      <svg class="w-3 h-3 opacity-50" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <path d="M12 20h9M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z" />
                      </svg>
                    </button>
                  {/if}
                </td>

                <td>
                  {#if port.is_up}
                    <span class="unifi-badge bg-[#00d68f]/15 text-[#00d68f] mono">{port.speed}</span>
                  {:else}
                    <span class="unifi-badge bg-[#232936] text-[#8c96a8] mono">Link Down</span>
                  {/if}
                </td>

                <td class="mono text-xs text-[#4da3ff] font-medium">
                  ↓ {formatRate(port.tx_rate || 0)}
                </td>

                <td class="mono text-xs text-[#00d68f] font-medium">
                  ↑ {formatRate(port.rx_rate || 0)}
                </td>

                <td class="mono text-xs text-[#8c96a8]">
                  <div>↓ {formatBytes(port.tx_bytes || 0)}</div>
                  <div>↑ {formatBytes(port.rx_bytes || 0)}</div>
                </td>

                <td class="mono text-xs {(port.tx_bad || 0) + (port.rx_bad || 0) > 0 ? 'text-[#ff3d71] font-bold' : 'text-[#596375]'}">
                  {port.tx_bad || 0} / {port.rx_bad || 0}
                </td>

                <td>
                  {#if portMacs.length > 0}
                    <div class="flex flex-wrap gap-1">
                      {#each portMacs.slice(0, 2) as m}
                        <span class="unifi-badge bg-[#1a1e29] border border-[#232936] text-[#f0f3f8] mono">
                          {m.hostname || m.mac}
                        </span>
                      {/each}
                      {#if portMacs.length > 2}
                        <span class="unifi-badge bg-[#006fff]/15 text-[#4da3ff] mono">
                          +{portMacs.length - 2}
                        </span>
                      {/if}
                    </div>
                  {:else}
                    <span class="text-xs text-[#596375]">—</span>
                  {/if}
                </td>

                <td class="text-right">
                  <button
                    onclick={() => openPortInspector(port)}
                    class="unifi-btn unifi-btn-secondary py-1 px-2.5 text-xs"
                  >
                    Gráfico
                  </button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </div>
  {/if}
{/if}

<!-- Port Inspector & Bandwidth History Modal -->
{#if inspectedPort && $selectedSwitch}
  <div class="fixed inset-0 z-50 bg-black/70 backdrop-blur-sm flex items-center justify-center p-4">
    <div class="unifi-card w-full max-w-3xl p-6 shadow-2xl border-[#2e3646]">
      <div class="flex items-center justify-between pb-4 border-b border-[#232936]">
        <div class="flex items-center gap-3">
          <span class="port-led {getPortLedClass(inspectedPort)}"></span>
          <div>
            <h3 class="text-base font-bold text-white">
              {$selectedSwitch.name} — Porta #{inspectedPort.port}
              {#if getPortNote($selectedSwitch, inspectedPort.port)}
                <span class="text-[#4da3ff] font-normal">({getPortNote($selectedSwitch, inspectedPort.port)})</span>
              {/if}
            </h3>
            <p class="text-xs text-[#8c96a8] mono">
              Status: {inspectedPort.speed} • Download: {formatRate(inspectedPort.tx_rate)} • Upload: {formatRate(inspectedPort.rx_rate)}
            </p>
          </div>
        </div>

        <button onclick={() => (inspectedPort = null)} class="unifi-btn unifi-btn-secondary py-1 px-2.5">
          Fechar ✕
        </button>
      </div>

      <!-- Time Period Selector -->
      <div class="flex items-center justify-between my-4">
        <div class="flex items-center gap-4 text-xs">
          <span class="flex items-center gap-1.5 text-[#4da3ff] font-medium">
            <span class="w-2.5 h-2.5 rounded-sm bg-[#006fff]"></span>
            Download (TX)
          </span>
          <span class="flex items-center gap-1.5 text-[#00d68f] font-medium">
            <span class="w-2.5 h-2.5 rounded-sm bg-[#00d68f]"></span>
            Upload (RX)
          </span>
        </div>

        <div class="flex items-center gap-1 bg-[#0b0d12] p-1 rounded-lg border border-[#232936]">
          {#each [{ id: 'live', label: 'Tempo Real' }, { id: '1h', label: '1 Hora' }, { id: '24h', label: '24 Horas' }] as tab}
            <button
              onclick={() => loadPortHistory(inspectedPort.port, tab.id)}
              class="px-3 py-1 rounded text-xs font-medium transition-colors {historyPeriod === tab.id
                ? 'bg-[#006fff] text-white'
                : 'text-[#8c96a8] hover:text-white'}"
            >
              {tab.label}
            </button>
          {/each}
        </div>
      </div>

      <!-- SVG Bandwidth Area Chart -->
      <div class="bg-[#0b0d12] border border-[#232936] rounded-lg p-4 mb-4">
        {#if loadingHistory}
          <div class="h-44 flex items-center justify-center text-xs text-[#8c96a8]">
            Carregando telemetria histórica...
          </div>
        {:else if !chartGeometry}
          <div class="h-44 flex items-center justify-center text-xs text-[#8c96a8]">
            Aguardando amostras de tráfego suficientes para plotar o gráfico...
          </div>
        {:else}
          <div class="flex justify-between text-[11px] text-[#8c96a8] mono mb-1">
            <span>Pico da Escala: {formatRate(chartGeometry.maxVal)}</span>
            <span>{historyPoints.length} amostras</span>
          </div>
          <svg viewBox="0 0 {chartGeometry.width} {chartGeometry.height}" class="w-full h-44 overflow-visible">
            <defs>
              <linearGradient id="dlGrad" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stop-color="#006fff" stop-opacity="0.4" />
                <stop offset="100%" stop-color="#006fff" stop-opacity="0.0" />
              </linearGradient>
              <linearGradient id="ulGrad" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stop-color="#00d68f" stop-opacity="0.35" />
                <stop offset="100%" stop-color="#00d68f" stop-opacity="0.0" />
              </linearGradient>
            </defs>
            <polygon points={chartGeometry.dlArea} fill="url(#dlGrad)" />
            <polygon points={chartGeometry.ulArea} fill="url(#ulGrad)" />
            <polyline fill="none" stroke="#006fff" stroke-width="2" points={chartGeometry.dlPoints} />
            <polyline fill="none" stroke="#00d68f" stroke-width="2" points={chartGeometry.ulPoints} />
          </svg>
        {/if}
      </div>

      <!-- Optical SFP+ DDMI Diagnostics (if supported) -->
      {#if $selectedSwitch.supports_ddmi}
        <div class="mt-4 pt-4 border-t border-[#232936]">
          <div class="flex items-center justify-between mb-3">
            <h4 class="text-xs font-semibold uppercase tracking-wider text-[#8c96a8]">
              Diagnóstico Óptico SFP+ (DDMI)
            </h4>
            <button onclick={loadTransceiverDiag} class="text-xs text-[#4da3ff] hover:underline">
              {loadingTransceiver ? 'Consultando...' : 'Atualizar DDMI'}
            </button>
          </div>

          {#if transceiverData.length === 0}
            <p class="text-xs text-[#596375]">Nenhum módulo SFP+ DDMI reportado pelo switch.</p>
          {:else}
            <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
              {#each transceiverData as d}
                <div class="bg-[#0b0d12] border border-[#232936] rounded-lg p-3 text-xs">
                  <div class="font-bold text-white mb-2 mono">{d.port}</div>
                  <div class="grid grid-cols-2 gap-2 text-[#8c96a8] mono">
                    <div>Temp: <span class="text-white">{d.temperature || 'N/A'} °C</span></div>
                    <div>Tensão: <span class="text-white">{d.voltage || 'N/A'} V</span></div>
                    <div>Bias: <span class="text-white">{d.bias_current || 'N/A'} mA</span></div>
                    <div>TX Power: <span class="text-[#00d68f]">{d.tx_power || 'N/A'} dBm</span></div>
                    <div>RX Power: <span class="text-[#4da3ff]">{d.rx_power || 'N/A'} dBm</span></div>
                  </div>
                </div>
              {/each}
            </div>
          {/if}
        </div>
      {/if}
    </div>
  </div>
{/if}

<!-- Switch MAC Address Table Modal -->
{#if showMacModal && $selectedSwitch}
  <div class="fixed inset-0 z-50 bg-black/70 backdrop-blur-sm flex items-center justify-center p-4">
    <div class="unifi-card w-full max-w-4xl p-6 shadow-2xl border-[#2e3646] max-h-[85vh] flex flex-col">
      <div class="flex flex-wrap items-center justify-between gap-3 pb-4 border-b border-[#232936]">
        <div>
          <h3 class="text-base font-bold text-white">Tabela MAC FDB — {$selectedSwitch.name}</h3>
          <p class="text-xs text-[#8c96a8]">
            {($selectedSwitch.mac_table || []).length} endereços aprendidos no switch ({$selectedSwitch.ip})
          </p>
        </div>

        <div class="flex items-center gap-2">
          <input
            type="text"
            bind:value={macSearch}
            placeholder="Filtrar MAC, fabricante ou porta..."
            class="unifi-input py-1.5 px-3 text-xs w-60"
          />
          <button onclick={refreshMacTable} disabled={refreshingMac} class="unifi-btn unifi-btn-primary">
            {refreshingMac ? 'Consultando...' : 'Atualizar Agora'}
          </button>
          <button onclick={() => (showMacModal = false)} class="unifi-btn unifi-btn-secondary">
            Fechar ✕
          </button>
        </div>
      </div>

      <div class="overflow-y-auto mt-4 flex-1">
        <table class="unifi-table">
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
                <td class="mono font-bold text-white">Porta #{entry.port}</td>
                <td class="mono text-xs text-[#8c96a8]">{entry.vlan}</td>
                <td class="mono text-xs text-[#4da3ff]">{entry.mac}</td>
                <td class="text-xs text-[#f0f3f8]">{entry.vendor || 'Unknown Vendor'}</td>
                <td>
                  {#if editingHostMac === entry.mac}
                    <div class="flex items-center gap-1.5">
                      <input
                        type="text"
                        bind:value={editingHostName}
                        class="unifi-input py-1 px-2 text-xs w-36"
                        onkeydown={(e) => e.key === 'Enter' && saveMacHostname(entry.mac)}
                      />
                      <button onclick={() => saveMacHostname(entry.mac)} class="unifi-btn unifi-btn-primary py-1 px-2 text-xs">
                        OK
                      </button>
                    </div>
                  {:else}
                    <button
                      onclick={() => {
                        editingHostMac = entry.mac;
                        editingHostName = entry.hostname || '';
                      }}
                      class="text-xs {entry.hostname ? 'text-white font-medium' : 'text-[#596375] italic'} hover:text-[#4da3ff]"
                    >
                      {entry.hostname || 'Definir nome...'}
                    </button>
                  {/if}
                </td>
                <td>
                  <span class="unifi-badge bg-[#1a1e29] text-[#8c96a8] mono">{entry.type || 'Dynamic'}</span>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </div>
  </div>
{/if}
