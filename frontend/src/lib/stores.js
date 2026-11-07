import { writable, derived } from 'svelte/store';

export const authState = writable({
	loading: true,
	setupRequired: false,
	authenticated: false,
	username: '',
	csrfToken: '',
	version: '',
	title: 'UniFi Switch OS'
});

export const switchesStore = writable([]);
export const topologyStore = writable({ nodes: [], links: [] });
export const configStore = writable(null);
export const activeSwitchIdx = writable(0);
export const toastStore = writable(null);

export const dashboardState = derived(
	[switchesStore, configStore],
	([$switches, $cfg]) => {
		const normalizedSwitches = ($switches || []).map((sw) => ({
			...sw,
			status: sw.error ? 'Offline' : sw.status || 'Online',
			mac_address: sw.mac || sw.mac_address || '',
			supports_mac: true,
			supports_ddmi: true,
			supports_backup: true,
			ports: (sw.ports || []).map((p) => ({
				...p,
				is_up: p.status === 'up' || p.is_up === true,
				tx_rate: (p.speed_tx_bps || 0) / 8,
				rx_rate: (p.speed_rx_bps || 0) / 8,
				tx_bytes: p.cum_tx || p.tx_bytes || 0,
				rx_bytes: p.cum_rx || p.rx_bytes || 0
			})),
			mac_table: (sw.mac_table || []).map((m) => ({
				...m,
				hostname: m.host || m.hostname || ''
			}))
		}));

		return {
			switches: normalizedSwitches,
			notes: ($cfg && $cfg.notes) || {},
			config: $cfg || {}
		};
	}
);

export const selectedSwitch = derived(
	[dashboardState, activeSwitchIdx],
	([$state, $idx]) => {
		const list = $state.switches || [];
		if (list.length === 0) return null;
		return list[$idx] || list[0];
	}
);

let toastTimer = null;
export function showToast(message, type = 'info') {
	if (toastTimer) clearTimeout(toastTimer);
	toastStore.set({ message, type });
	toastTimer = setTimeout(() => {
		toastStore.set(null);
	}, 3800);
}

export function formatBytes(bytes) {
	const b = Number(bytes) || 0;
	if (b >= 1099511627776) return (b / 1099511627776).toFixed(2) + ' TB';
	if (b >= 1073741824) return (b / 1073741824).toFixed(2) + ' GB';
	if (b >= 1048576) return (b / 1048576).toFixed(1) + ' MB';
	if (b >= 1024) return (b / 1024).toFixed(1) + ' KB';
	return b + ' B';
}

export function formatBps(bps) {
	const v = Number(bps) || 0;
	if (v >= 1_000_000_000) return (v / 1_000_000_000).toFixed(2) + ' Gbps';
	if (v >= 1_000_000) return (v / 1_000_000).toFixed(1) + ' Mbps';
	if (v >= 1_000) return (v / 1_000).toFixed(1) + ' Kbps';
	return v + ' bps';
}

export function formatRate(bytesPerSec) {
	const bps = (Number(bytesPerSec) || 0) * 8;
	return formatBps(bps);
}

export function speedColor(status, speed) {
	if (status === 'disable' || status === 'disabled') return 'var(--status-disable)';
	if (status !== 'up') return 'var(--status-down)';
	const s = (speed || '').toLowerCase();
	if (s.includes('10g') || s.includes('5g')) return 'var(--status-10g)';
	if (s.includes('2.5g') || s.includes('2g')) return 'var(--status-25g)';
	if (s.includes('100m') || s.includes('10m')) return 'var(--status-100m)';
	return 'var(--status-1g)';
}
