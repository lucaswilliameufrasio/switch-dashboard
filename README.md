# UniFi Switch OS — Hardened Controller for Horaco / KeepLink / RTL Managed Switches

Painel de controle e telemetria L2 de alta performance reescrito em **Go (`1.27+`)** e **SvelteKit**, com interface inspirada no **UniFi OS Server** e compilado em um **único binário estático autocontido** (`switch-dashboard`).

Projetado para monitorar switches gerenciáveis **Horaco**, **KeepLink** e firmwares **RTLPlayground** (chipsets Realtek RTL8372 / RTL8373 2.5GbE + 10GbE SFP+) diretamente via interface web/CGI nativa (sem necessidade de SNMP).

---

## Blindagem de Segurança (Security-First)

- **Zero Telemetria Externa & Zero CDNs**: Nenhuma conexão oculta para servidores externos (`workers.dev`) ou carregamento de fontes/scripts de terceiros.
- **Cofre Criptográfico AES-256-GCM**: Todas as senhas dos switches armazenadas em `config.json` são criptografadas em repouso com chave mestre de 256 bits (`.master.key` com permissão `0600` ou variável `DASHBOARD_MASTER_KEY`) e nunca retornam em texto puro na API.
- **Autenticação Nativa & Anti-CSRF**:
  - Setup inicial obrigatório de administrador com hash `bcrypt` (custo 12).
  - Cookies de sessão `HttpOnly`, `SameSite=Strict` e `Secure` (quando sob HTTPS).
  - Validação estrita de cabeçalhos `X-CSRF-Token`, `Origin`/`Referer` (com suporte a `X-Forwarded-Host`) e `Content-Type: application/json`.
  - Rate limiting por IP contra tentativas de força bruta no login.
- **Superfície de Ataque Mínima**:
  - Escuta por padrão apenas em `127.0.0.1:8080`.
  - Execução sem privilégios de `root` tanto no Docker (`USER switchos`) quanto no `systemd`.
  - Módulos legados inseguros (SSH/OVS `shell=True`, SOAP TR-064 e scanner ARP root) foram permanentemente removidos.

---

## Funcionalidades Principais

1. **Chassi Virtual 1U (UniFi Devices)**:
   - Representação física das portas RJ45 2.5GbE e gaiolas SFP+ 10GbE com LEDs dinâmicos por velocidade (`10G`, `2.5G`, `1G`, `100M`).
   - Gráficos de banda em 3 escalas (`Tempo Real`, `1 Hora`, `24 Horas`).
   - Diagnóstico óptico **SFP+ DDMI** sob demanda (temperatura, tensão, corrente de bias, potência TX/RX em dBm).
   - Visualização e atualização instantânea da tabela **MAC FDB** por switch.
2. **Mapa de Topologia L2 (`/topology`)**:
   - Descoberta passiva baseada nas tabelas MAC FDB dos switches, uplinks estáticos/automáticos, switches não-gerenciáveis (hubs L2) e nós de infraestrutura.
   - Grafo SVG interativo com *pan/zoom*, *drag-and-drop* persistente e auto-layout.
3. **Inventário de Clientes (`/clients`)**:
   - Lista consolidada de dispositivos aprendidos na rede L2, com busca instantânea, edição inline de hostname/notas, seletor de categoria e importação CSV.
4. **Cofre de Backups (`/backups`)**:
   - Extração, download e gerenciamento dos arquivos binários (`.bin`) de configuração dos switches.
5. **Templates YAML Customizáveis (`/settings`)**:
   - Suporte declarativo a múltiplos modelos de switches via arquivos em `device-templates/*.yaml` e base local de fabricantes MAC (`mac_vendors.txt` + atualização manual IEEE OUI).

---

## Requisitos & Build Rápido (`mise` + `Makefile`)

O projeto inclui configuração declarativa para o [mise](https://mise.jdx.dev/) (`mise.toml`) utilizando a última versão estável do Go e Node LTS, além de um `Makefile` completo:

```bash
# Instalar dependências (Go + Frontend SvelteKit)
make setup

# Compilar a SPA SvelteKit + binário estático ./switch-dashboard
make build

# Executar localmente em http://127.0.0.1:8080
make run
```

Para ver todos os comandos disponíveis:
```bash
make help
```

---

## Acesso Remoto Seguro via Tailscale ou NetBird

Você pode expor o painel **exclusivamente** na interface privada da sua VPN Mesh (**Tailscale** ou **NetBird**), mantendo a porta fechada na rede local:

### 1. Usando interpolação de comando no `make run`

#### Tailscale
- **Bash / Zsh**:
  ```bash
  make run HOST=$(tailscale ip -4) PORT=8080
  ```
- **Fish Shell**:
  ```fish
  make run HOST=(tailscale ip -4) PORT=8080
  ```

#### NetBird
- **Bash / Zsh** (extraindo o IPv4 da interface `wt0` ou via `netbird status`):
  ```bash
  make run HOST=$(ip -4 -o addr show dev wt0 | awk '{split($4,a,"/"); print a[1]}') PORT=8080
  # ou via CLI do netbird:
  make run HOST=$(netbird status | awk '/NetBird IP:/ {split($3,a,"/"); print a[1]}') PORT=8080
  ```
- **Fish Shell**:
  ```fish
  make run HOST=(ip -4 -o addr show dev wt0 | awk '{split($4,a,"/"); print a[1]}') PORT=8080
  # ou via CLI do netbird:
  make run HOST=(netbird status | awk '/NetBird IP:/ {split($3,a,"/"); print a[1]}') PORT=8080
  ```

### 2. Detecção Automática de VPN (`make run-vpn`)
O `Makefile` também possui um alvo que detecta automaticamente o IP da interface `tailscale0` ou `wt0`:

```bash
make run-vpn
```

### 3. HTTPS com Certificado Automático via `tailscale serve`
Se quiser manter o binário escutando apenas em `127.0.0.1:8080` e usar o proxy HTTPS da sua Tailnet:

```bash
make run
sudo tailscale serve --bg 8080
```

---

## Executando via Docker Compose

Por padrão, o `docker-compose.yml` faz bind apenas em `127.0.0.1:8080:8080` e executa o processo como usuário sem privilégios (`switchos`, UID `10001`):

```bash
make docker-up
```
