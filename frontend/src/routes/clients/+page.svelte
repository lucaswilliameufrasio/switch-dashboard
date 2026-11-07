<script>
  import { onMount } from 'svelte';
  import { apiFetch } from '$lib/api.js';
  import { showToast } from '$lib/stores.js';

  let clients = $state([]);
  let loading = $state(true);
  let search = $state('');
  let statusFilter = $state('all'); // 'all' | 'online' | 'offline'

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
      showToast('Cliente atualizado', 'success');
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
      showToast('Tipo de dispositivo atualizado', 'success');
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
      showToast('Registro removido', 'success');
    } catch (e) {
      showToast(e.message, 'error');
    }
  }

  async function importCsv() {
    const trimmed = csvText.trim();
    if (!trimmed) {
      showToast('Insira as linhas CSV (Hostname,MAC)', 'error');
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
      showToast(`${res.imported || 0} clientes importados`, 'success');
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

<div class="flex flex-wrap items-center justify-between gap-3 mb-5">
  <div>
    <h2 class="text-base font-bold text-white">Dispositivos Clientes (L2 FDB)</h2>
    <p class="text-xs text-[#8c96a8]">
      Inventário passivo de dispositivos identificados nas tabelas MAC dos switches
    </p>
  </div>

  <div class="flex flex-wrap items-center gap-2">
    <input
      type="text"
      bind:value={search}
      placeholder="Buscar MAC, nome, fabricante, switch..."
      class="unifi-input py-1.5 px-3 text-xs w-64"
    />

    <div class="flex items-center bg-[#111318] p-1 rounded-lg border border-[#232936] text-xs">
      {#each [{ id: 'all', label: 'Todos' }, { id: 'online', label: 'Online' }, { id: 'offline', label: 'Offline' }] as f}
        <button
          onclick={() => (statusFilter = f.id)}
          class="px-2.5 py-1 rounded transition-colors {statusFilter === f.id
            ? 'bg-[#006fff] text-white'
            : 'text-[#8c96a8] hover:text-white'}"
        >
          {f.label}
        </button>
      {/each}
    </div>

    <button onclick={() => (showCsvModal = true)} class="unifi-btn unifi-btn-secondary">
      Importar CSV
    </button>
    <button onclick={loadClients} class="unifi-btn unifi-btn-primary">
      Atualizar
    </button>
  </div>
</div>

<div class="unifi-card overflow-hidden">
  {#if loading}
    <div class="p-8 text-center text-xs text-[#8c96a8]">Carregando inventário de clientes...</div>
  {:else if filteredClients.length === 0}
    <div class="p-8 text-center text-xs text-[#8c96a8]">Nenhum cliente encontrado para o filtro selecionado.</div>
  {:else}
    <div class="overflow-x-auto">
      <table class="unifi-table">
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
            <tr class="hover:bg-[#1b202b]/60">
              <td>
                <span class="inline-flex items-center gap-1.5 text-xs">
                  <span class="w-2 h-2 rounded-full {cl.is_online ? 'bg-[#00d68f]' : 'bg-[#596375]'}"></span>
                  <span class={cl.is_online ? 'text-[#00d68f] font-medium' : 'text-[#8c96a8]'}>
                    {cl.is_online ? 'Online' : 'Offline'}
                  </span>
                </span>
              </td>

              <td>
                {#if editingMac === cl.mac}
                  <div class="flex flex-col gap-1.5 max-w-xs">
                    <input
                      type="text"
                      bind:value={editHostname}
                      placeholder="Hostname"
                      class="unifi-input py-1 px-2 text-xs"
                    />
                    <input
                      type="text"
                      bind:value={editNote}
                      placeholder="Nota / Localização"
                      class="unifi-input py-1 px-2 text-xs"
                    />
                    <div class="flex gap-1.5">
                      <button onclick={() => saveClientMeta(cl.mac)} class="unifi-btn unifi-btn-primary py-1 px-2 text-xs">
                        Salvar
                      </button>
                      <button onclick={() => (editingMac = '')} class="unifi-btn unifi-btn-secondary py-1 px-2 text-xs">
                        Cancelar
                      </button>
                    </div>
                  </div>
                {:else}
                  <button onclick={() => startEdit(cl)} class="text-left group">
                    <div class="text-xs font-semibold text-white group-hover:text-[#4da3ff]">
                      {cl.hostname || cl.mac}
                    </div>
                    {#if cl.note}
                      <div class="text-[11px] text-[#8c96a8]">{cl.note}</div>
                    {:else}
                      <div class="text-[11px] text-[#596375] italic">Clique para nomear</div>
                    {/if}
                  </button>
                {/if}
              </td>

              <td class="mono text-xs text-[#4da3ff]">{cl.mac}</td>
              <td class="text-xs text-[#f0f3f8]">{cl.vendor || 'Unknown Vendor'}</td>

              <td>
                <select
                  value={cl.device_type || 'laptop'}
                  onchange={(e) => changeDeviceType(cl.mac, e.currentTarget.value)}
                  class="bg-[#0b0d12] border border-[#232936] rounded px-2 py-1 text-xs text-[#f0f3f8]"
                >
                  {#each deviceTypes as dt}
                    <option value={dt.id}>{dt.label}</option>
                  {/each}
                </select>
              </td>

              <td>
                {#if cl.ip}
                  <div class="text-xs font-medium text-white mono">{cl.ip}</div>
                  <div class="text-[11px] text-[#8c96a8] mono">
                    Porta #{cl.port} {cl.vlan ? `• VLAN ${cl.vlan}` : ''}
                  </div>
                {:else}
                  <span class="text-xs text-[#596375]">—</span>
                {/if}
              </td>

              <td class="text-right">
                <button
                  onclick={() => deleteClient(cl.mac)}
                  class="text-xs text-[#8c96a8] hover:text-[#ff3d71] px-2 py-1"
                  title="Esquecer cliente"
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
</div>

{#if showCsvModal}
  <div class="fixed inset-0 z-50 bg-black/70 backdrop-blur-sm flex items-center justify-center p-4">
    <div class="unifi-card w-full max-w-lg p-6">
      <h3 class="text-base font-bold text-white mb-1">Importar Clientes via CSV</h3>
      <p class="text-xs text-[#8c96a8] mb-3">
        Informe um registro por linha no formato: <code class="mono text-[#4da3ff]">Hostname,AA:BB:CC:DD:EE:FF</code>
      </p>
      <textarea
        bind:value={csvText}
        rows="7"
        placeholder="Proxmox-VE,00:11:22:33:44:55"
        class="unifi-input mono text-xs mb-4"
      ></textarea>
      <div class="flex justify-end gap-2">
        <button onclick={() => (showCsvModal = false)} class="unifi-btn unifi-btn-secondary">Cancelar</button>
        <button onclick={importCsv} class="unifi-btn unifi-btn-primary">Importar</button>
      </div>
    </div>
  </div>
{/if}
