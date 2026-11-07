<script>
  import { onMount } from 'svelte';
  import { apiFetch, refreshDashboardData } from '$lib/api.js';
  import { authState, showToast } from '$lib/stores.js';

  let activeTab = $state('switches');
  let loading = $state(true);

  // Configuration state
  let switches = $state([]);
  let dashboardTitle = $state('UniFi Switch OS');
  let pollingInterval = $state(3);
  let macRefreshInterval = $state(10);
  let macIgnoreList = $state('');
  let infraDevices = $state([]);
  let unmanagedSwitches = $state([]);

  // Templates & Vendors state
  let templates = $state([]);
  let selectedTemplateFile = $state('');
  let templateContent = $state('');
  let vendorsContent = $state('');
  let updatingOui = $state(false);

  // Admin credentials state
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

      dashboardTitle = cfg.title || 'UniFi Switch OS';
      pollingInterval = cfg.refresh_interval || 3;
      macRefreshInterval = cfg.mac_refresh_multiplier || 10;
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
      showToast(`Comando de reboot enviado para ${name}`, 'success');
    } catch (e) {
      showToast(e.message, 'error');
    }
  }

  async function resetCounters() {
    if (!confirm('Zerar os contadores acumulados e o histórico de tráfego?')) return;
    try {
      await apiFetch('/api/reset', { method: 'POST' });
      showToast('Contadores de tráfego reiniciados', 'success');
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
      showToast(`Template ${selectedTemplateFile} atualizado`, 'success');
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
      showToast('Lista de fabricantes MAC salva', 'success');
    } catch (e) {
      showToast(e.message, 'error');
    }
  }

  async function updateOuiFromIeee() {
    updatingOui = true;
    try {
      const res = await apiFetch('/api/vendors/update_oui', { method: 'POST' });
      showToast(res.message || 'Base IEEE OUI atualizada', 'success');
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
      showToast('Credenciais de administrador atualizadas', 'success');
    } catch (err) {
      showToast(err.message, 'error');
    }
  }

  onMount(loadConfig);
</script>

<div class="mb-6">
  <h2 class="text-base font-bold text-white">Configurações do Controlador UniFi Switch OS</h2>
  <p class="text-xs text-[#8c96a8]">
    Gerencie switches, topologia L2, templates YAML e segurança do painel
  </p>
</div>

<!-- Settings Sub-navigation Tabs -->
<div class="flex flex-wrap items-center gap-2 mb-6 border-b border-[#232936] pb-3">
  {#each [
    { id: 'switches', label: 'Switches Gerenciados' },
    { id: 'general', label: 'Geral & Topologia L2' },
    { id: 'templates', label: 'Templates YAML & OUI' },
    { id: 'security', label: 'Segurança & Administrador' }
  ] as tab}
    <button
      onclick={() => (activeTab = tab.id)}
      class="px-3.5 py-2 rounded-lg text-xs font-medium transition-colors {activeTab === tab.id
        ? 'bg-[#006fff] text-white'
        : 'bg-[#151820] text-[#8c96a8] hover:text-white border border-[#232936]'}"
    >
      {tab.label}
    </button>
  {/each}
</div>

{#if loading}
  <div class="unifi-card p-8 text-center text-xs text-[#8c96a8]">Carregando configurações...</div>
{:else if activeTab === 'switches'}
  <!-- TAB 1: SWITCHES -->
  <div class="unifi-card p-6">
    <div class="flex flex-wrap items-center justify-between gap-3 mb-4">
      <div>
        <h3 class="text-sm font-semibold text-white">Switches da Rede</h3>
        <p class="text-xs text-[#8c96a8]">
          As senhas dos switches são criptografadas em repouso com AES-256-GCM e nunca trafegam de volta em texto puro.
        </p>
      </div>
      <button onclick={addSwitchRow} class="unifi-btn unifi-btn-secondary">
        + Adicionar Switch
      </button>
    </div>

    <div class="space-y-4">
      {#each switches as sw, idx}
        <div class="bg-[#0b0d12] border border-[#232936] rounded-xl p-4">
          <div class="grid grid-cols-1 md:grid-cols-6 gap-3 items-end">
            <label class="block">
              <span class="block text-[11px] text-[#8c96a8] mb-1">Nome do Switch</span>
              <input type="text" bind:value={sw.name} class="unifi-input text-xs" placeholder="Core-Switch" />
            </label>

            <label class="block">
              <span class="block text-[11px] text-[#8c96a8] mb-1">Endereço IP / Host:Porta</span>
              <input type="text" bind:value={sw.ip} class="unifi-input text-xs mono" placeholder="192.168.1.2" />
            </label>

            <label class="block">
              <span class="block text-[11px] text-[#8c96a8] mb-1">Usuário</span>
              <input type="text" bind:value={sw.username} class="unifi-input text-xs" placeholder="admin" />
            </label>

            <label class="block">
              <span class="block text-[11px] text-[#8c96a8] mb-1">
                Senha {sw.has_password ? '(Salva em AES-GCM)' : '(AES-256-GCM)'}
              </span>
              <input
                type="password"
                bind:value={sw.password}
                class="unifi-input text-xs"
                placeholder={sw.has_password ? '•••••••• (manter atual)' : 'Senha do switch'}
              />
            </label>

            <label class="block">
              <span class="block text-[11px] text-[#8c96a8] mb-1">Driver / Template</span>
              <select bind:value={sw.model} class="unifi-input text-xs">
                {#each builtInModels as bm}
                  <option value={bm.id}>{bm.label}</option>
                {/each}
                {#each templates as t}
                  <option value={t.id}>YAML: {t.name || t.id}</option>
                {/each}
              </select>
            </label>

            <label class="block">
              <span class="block text-[11px] text-[#8c96a8] mb-1">IP Switch Pai (Uplink)</span>
              <input
                type="text"
                bind:value={sw.parent_ip}
                class="unifi-input text-xs mono"
                placeholder="Opcional: 192.168.1.1"
              />
            </label>
          </div>

          <div class="flex items-center justify-end gap-2 mt-3 pt-3 border-t border-[#1a1f2b]">
            <button
              onclick={() => rebootSwitch(sw.ip, sw.name)}
              class="unifi-btn unifi-btn-secondary py-1 px-2.5 text-xs text-[#ffaa00]"
            >
              Reiniciar Switch
            </button>
            <button
              onclick={() => removeSwitchRow(idx)}
              class="unifi-btn unifi-btn-danger py-1 px-2.5 text-xs"
            >
              Remover
            </button>
          </div>
        </div>
      {/each}
    </div>

    <div class="mt-6 flex justify-end">
      <button
        onclick={() => saveFullConfig('Switches salvos (senhas criptografadas com AES-256-GCM)')}
        class="unifi-btn unifi-btn-primary"
      >
        Salvar Switches
      </button>
    </div>
  </div>
{:else if activeTab === 'general'}
  <!-- TAB 2: GENERAL & TOPOLOGY -->
  <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
    <div class="unifi-card p-6 space-y-4">
      <h3 class="text-sm font-semibold text-white">Parâmetros de Polling</h3>

      <label class="block">
        <span class="block text-xs text-[#8c96a8] mb-1.5">Título do Controlador</span>
        <input type="text" bind:value={dashboardTitle} class="unifi-input" />
      </label>

      <div class="grid grid-cols-2 gap-4">
        <label class="block">
          <span class="block text-xs text-[#8c96a8] mb-1.5">Intervalo de Polling (segundos)</span>
          <input type="number" min="1" max="300" bind:value={pollingInterval} class="unifi-input mono" />
        </label>
        <label class="block">
          <span class="block text-xs text-[#8c96a8] mb-1.5">Multiplicador de Refresh MAC</span>
          <input type="number" min="1" max="100" bind:value={macRefreshInterval} class="unifi-input mono" />
        </label>
      </div>

      <label class="block">
        <span class="block text-xs text-[#8c96a8] mb-1.5">
          Filtro de MACs Ignorados (1 por linha, suporta wildcard <code class="mono">*</code>)
        </span>
        <textarea
          bind:value={macIgnoreList}
          rows="4"
          placeholder="01:00:5E:*&#10;33:33:*"
          class="unifi-input mono text-xs"
        ></textarea>
      </label>

      <div class="flex items-center justify-between pt-2">
        <button onclick={resetCounters} class="unifi-btn unifi-btn-danger">
          Zerar Contadores de Tráfego
        </button>
        <button
          onclick={() => saveFullConfig('Configurações gerais salvas')}
          class="unifi-btn unifi-btn-primary"
        >
          Salvar Configurações
        </button>
      </div>
    </div>

    <!-- Infrastructure & Unmanaged Switches -->
    <div class="unifi-card p-6 space-y-5">
      <div>
        <div class="flex items-center justify-between mb-2">
          <h3 class="text-sm font-semibold text-white">Switches Não-Gerenciáveis (Hubs L2)</h3>
          <button
            onclick={() =>
              (unmanagedSwitches = [
                ...unmanagedSwitches,
                { name: 'Mini-Switch', parent_ip: '', parent_port: '1' }
              ])}
            class="unifi-btn unifi-btn-secondary py-1 px-2 text-xs"
          >
            + Adicionar Hub
          </button>
        </div>
        <div class="space-y-2">
          {#each unmanagedSwitches as u, idx}
            <div class="grid grid-cols-4 gap-2 items-center bg-[#0b0d12] p-2 rounded-lg border border-[#232936]">
              <input type="text" bind:value={u.name} placeholder="Nome" class="unifi-input py-1 text-xs" />
              <input
                type="text"
                bind:value={u.parent_ip}
                placeholder="IP Switch Pai"
                class="unifi-input py-1 text-xs mono"
              />
              <input
                type="text"
                bind:value={u.parent_port}
                placeholder="Porta"
                class="unifi-input py-1 text-xs mono"
              />
              <button
                onclick={() => (unmanagedSwitches = unmanagedSwitches.filter((_, i) => i !== idx))}
                class="unifi-btn unifi-btn-danger py-1 text-xs justify-center"
              >
                Remover
              </button>
            </div>
          {/each}
        </div>
      </div>

      <div class="pt-4 border-t border-[#232936]">
        <div class="flex items-center justify-between mb-2">
          <h3 class="text-sm font-semibold text-white">Nós de Infraestrutura (Gateways / APs por MAC)</h3>
          <button
            onclick={() =>
              (infraDevices = [
                ...infraDevices,
                { name: 'Gateway', mac: '', type: 'router' }
              ])}
            class="unifi-btn unifi-btn-secondary py-1 px-2 text-xs"
          >
            + Adicionar Nó
          </button>
        </div>
        <div class="space-y-2">
          {#each infraDevices as inf, idx}
            <div class="grid grid-cols-4 gap-2 items-center bg-[#0b0d12] p-2 rounded-lg border border-[#232936]">
              <input type="text" bind:value={inf.name} placeholder="Nome" class="unifi-input py-1 text-xs" />
              <input type="text" bind:value={inf.mac} placeholder="AA:BB:CC:DD:EE:FF" class="unifi-input py-1 text-xs mono" />
              <input
                type="text"
                bind:value={inf.type}
                placeholder="router / ap"
                class="unifi-input py-1 text-xs"
              />
              <button
                onclick={() => (infraDevices = infraDevices.filter((_, i) => i !== idx))}
                class="unifi-btn unifi-btn-danger py-1 text-xs justify-center"
              >
                ✕
              </button>
            </div>
          {/each}
        </div>
      </div>
    </div>
  </div>
{:else if activeTab === 'templates'}
  <!-- TAB 3: YAML TEMPLATES & MAC VENDORS -->
  <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
    <div class="unifi-card p-6 flex flex-col">
      <div class="flex items-center justify-between mb-3">
        <div>
          <h3 class="text-sm font-semibold text-white">Templates YAML de Switches</h3>
          <p class="text-xs text-[#8c96a8]">Suporte declarativo para modelos Realtek / Horaco / KeepLink</p>
        </div>
        <select
          value={selectedTemplateFile}
          onchange={(e) => selectTemplate(e.currentTarget.value)}
          class="bg-[#0b0d12] border border-[#232936] rounded-lg px-3 py-1.5 text-xs text-white mono"
        >
          {#each templates as t}
            <option value={t.filename}>{t.filename}</option>
          {/each}
        </select>
      </div>

      <textarea
        bind:value={templateContent}
        rows="16"
        class="unifi-input mono text-xs flex-1 mb-4"
      ></textarea>

      <div class="flex justify-end">
        <button onclick={saveTemplate} class="unifi-btn unifi-btn-primary">
          Salvar Template YAML
        </button>
      </div>
    </div>

    <div class="unifi-card p-6 flex flex-col">
      <div class="flex items-center justify-between mb-3">
        <div>
          <h3 class="text-sm font-semibold text-white">Prefixos MAC OUI (`mac_vendors.txt`)</h3>
          <p class="text-xs text-[#8c96a8]">Mapeamento local sem consultas automáticas para a internet</p>
        </div>
        <button
          onclick={updateOuiFromIeee}
          disabled={updatingOui}
          class="unifi-btn unifi-btn-secondary text-xs"
        >
          {updatingOui ? 'Baixando IEEE OUI...' : 'Atualizar OUI (IEEE)'}
        </button>
      </div>

      <textarea
        bind:value={vendorsContent}
        rows="16"
        class="unifi-input mono text-xs flex-1 mb-4"
      ></textarea>

      <div class="flex justify-end">
        <button onclick={saveVendors} class="unifi-btn unifi-btn-primary">
          Salvar mac_vendors.txt
        </button>
      </div>
    </div>
  </div>
{:else if activeTab === 'security'}
  <!-- TAB 4: SECURITY & ADMIN -->
  <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
    <div class="unifi-card p-6">
      <h3 class="text-sm font-semibold text-white mb-1">Credenciais do Administrador</h3>
      <p class="text-xs text-[#8c96a8] mb-4">
        Senhas de administrador são armazenadas com hash <code class="mono text-[#4da3ff]">bcrypt</code>.
      </p>

      <form onsubmit={updateAdminPassword} class="space-y-4">
        <label class="block">
          <span class="block text-xs text-[#8c96a8] mb-1">Novo Usuário Administrador</span>
          <input type="text" bind:value={newUsername} required class="unifi-input" />
        </label>

        <label class="block">
          <span class="block text-xs text-[#8c96a8] mb-1">Senha Atual</span>
          <input type="password" bind:value={currentPassword} required class="unifi-input" />
        </label>

        <label class="block">
          <span class="block text-xs text-[#8c96a8] mb-1">Nova Senha (mínimo 8 caracteres)</span>
          <input type="password" bind:value={newPassword} required minlength="8" class="unifi-input" />
        </label>

        <button type="submit" class="unifi-btn unifi-btn-primary">
          Atualizar Senha do Painel
        </button>
      </form>
    </div>

    <div class="unifi-card p-6">
      <h3 class="text-sm font-semibold text-white mb-3">Blindagem de Segurança Ativa</h3>
      <ul class="space-y-3 text-xs text-[#8c96a8]">
        <li class="flex items-start gap-2.5">
          <span class="w-2 h-2 rounded-full bg-[#00d68f] mt-1 shrink-0"></span>
          <span><strong class="text-white">Zero Telemetria Externa:</strong> Nenhuma conexão automática para domínios externos ou CDNs de terceiros.</span>
        </li>
        <li class="flex items-start gap-2.5">
          <span class="w-2 h-2 rounded-full bg-[#00d68f] mt-1 shrink-0"></span>
          <span><strong class="text-white">Cofre AES-256-GCM:</strong> As senhas dos switches em <code class="mono">config.json</code> são criptografadas com chave mestre de 256 bits (<code class="mono">.master.key</code> perm 0600).</span>
        </li>
        <li class="flex items-start gap-2.5">
          <span class="w-2 h-2 rounded-full bg-[#00d68f] mt-1 shrink-0"></span>
          <span><strong class="text-white">Proteção Anti-CSRF & SameSite=Strict:</strong> Todas as rotas de mutação exigem token CSRF sincronizado, verificação de cabeçalho <code class="mono">Origin</code> e <code class="mono">Content-Type: application/json</code>.</span>
        </li>
        <li class="flex items-start gap-2.5">
          <span class="w-2 h-2 rounded-full bg-[#00d68f] mt-1 shrink-0"></span>
          <span><strong class="text-white">Superfície de Ataque Reduzida:</strong> Módulos legados vulneráveis (SSH/OVS <code class="mono">shell=True</code>, SOAP TR-064 e scanner ARP root) foram permanentemente eliminados.</span>
        </li>
      </ul>
    </div>
  </div>
{/if}
