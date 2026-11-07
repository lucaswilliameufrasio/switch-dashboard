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
      showToast(`Backup salvo: ${res.filename} (${formatBytes(res.size)})`, 'success');
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
      showToast('Backup removido', 'success');
    } catch (e) {
      showToast(e.message, 'error');
    }
  }

  onMount(loadBackups);
</script>

<div class="mb-6">
  <h2 class="text-base font-bold text-white">Cofre de Backups de Configuração</h2>
  <p class="text-xs text-[#8c96a8]">
    Extraia e armazene imagens binárias de configuração dos switches Horaco / KeepLink
  </p>
</div>

<!-- Switch Quick Backup Cards -->
<div class="grid grid-cols-1 md:grid-cols-3 gap-4 mb-6">
  {#each $dashboardState.switches || [] as sw}
    <div class="unifi-card p-4 flex items-center justify-between">
      <div>
        <div class="flex items-center gap-2">
          <span class="w-2 h-2 rounded-full {sw.status === 'Online' ? 'bg-[#00d68f]' : 'bg-[#ff3d71]'}"></span>
          <span class="text-sm font-semibold text-white">{sw.name}</span>
        </div>
        <div class="text-xs text-[#8c96a8] mono mt-0.5">{sw.ip} • {sw.model || 'Switch'}</div>
      </div>

      <button
        onclick={() => triggerBackup(sw)}
        disabled={backingUpIp === sw.ip || !sw.supports_backup}
        class="unifi-btn unifi-btn-primary"
      >
        {backingUpIp === sw.ip ? 'Extraindo...' : 'Gerar Backup'}
      </button>
    </div>
  {/each}
</div>

<!-- Stored Backups Table -->
<div class="unifi-card overflow-hidden">
  <div class="px-5 py-3.5 border-b border-[#232936] flex items-center justify-between">
    <h3 class="text-sm font-semibold text-white">Arquivos Salvos ({backups.length})</h3>
    <button onclick={loadBackups} class="unifi-btn unifi-btn-secondary py-1 px-2.5 text-xs">
      Atualizar Lista
    </button>
  </div>

  {#if loading}
    <div class="p-8 text-center text-xs text-[#8c96a8]">Carregando arquivos de backup...</div>
  {:else if backups.length === 0}
    <div class="p-8 text-center text-xs text-[#8c96a8]">
      Nenhum backup armazenado ainda. Clique em "Gerar Backup" em um dos switches acima.
    </div>
  {:else}
    <div class="overflow-x-auto">
      <table class="unifi-table">
        <thead>
          <tr>
            <th>Arquivo</th>
            <th>Tamanho</th>
            <th>Data de Criação</th>
            <th class="text-right">Ações</th>
          </tr>
        </thead>
        <tbody>
          {#each backups as b}
            <tr>
              <td class="mono text-xs font-medium text-white">{b.filename}</td>
              <td class="mono text-xs text-[#8c96a8]">{formatBytes(b.size)}</td>
              <td class="mono text-xs text-[#8c96a8]">{b.datetime || b.created || ''}</td>
              <td class="text-right space-x-2">
                <a
                  href="/api/backups/{encodeURIComponent(b.filename)}/download"
                  class="unifi-btn unifi-btn-secondary py-1 px-2.5 text-xs"
                >
                  Download
                </a>
                <button
                  onclick={() => deleteBackup(b.filename)}
                  class="unifi-btn unifi-btn-danger py-1 px-2.5 text-xs"
                >
                  Excluir
                </button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>
