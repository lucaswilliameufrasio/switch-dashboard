package poller

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"horaco-switch-dashboard/internal/config"
	"horaco-switch-dashboard/internal/scraper"
)

type CounterState struct {
	Tx    uint64   `json:"tx"`
	Rx    uint64   `json:"rx"`
	CumTx uint64   `json:"cum_tx"`
	CumRx uint64   `json:"cum_rx"`
	Ts    *float64 `json:"ts"`
}

type HistoryPoint struct {
	Ts float64 `json:"ts"`
	Tx int64   `json:"tx"`
	Rx int64   `json:"rx"`
}

type LiveSample struct {
	Ts float64
	Tx uint64
	Rx uint64
}

type PortSpeed struct {
	SpeedTx int64 `json:"speed_tx"`
	SpeedRx int64 `json:"speed_rx"`
}

type Engine struct {
	store *config.Store

	cacheMu        sync.RWMutex
	cachedData     map[string]scraper.SwitchData
	cachedSpeeds   map[string]map[string]PortSpeed
	macTables      map[string][]scraper.MACEntry
	lastMACScrape  map[string]float64

	countersMu sync.Mutex
	historyMu  sync.RWMutex
	liveHist   map[string][]LiveSample
	hourlyHist map[string][]HistoryPoint
	dailyHist  map[string][]HistoryPoint

	wakeCh chan struct{}
}

func NewEngine(store *config.Store) *Engine {
	e := &Engine{
		store:         store,
		cachedData:    make(map[string]scraper.SwitchData),
		cachedSpeeds:  make(map[string]map[string]PortSpeed),
		macTables:     make(map[string][]scraper.MACEntry),
		lastMACScrape: make(map[string]float64),
		liveHist:      make(map[string][]LiveSample),
		hourlyHist:    make(map[string][]HistoryPoint),
		dailyHist:     make(map[string][]HistoryPoint),
		wakeCh:        make(chan struct{}, 1),
	}
	e.loadHistory()
	return e
}

func (e *Engine) Start() {
	go func() {
		for {
			e.PollOnce()
			interval := 15
			e.store.Read(func(cfg *config.AppConfig) {
				if cfg.RefreshInterval > 0 {
					interval = cfg.RefreshInterval
				}
			})
			select {
			case <-time.After(time.Duration(interval) * time.Second):
			case <-e.wakeCh:
			}
		}
	}()
}

func (e *Engine) TriggerPoll() {
	select {
	case e.wakeCh <- struct{}{}:
	default:
	}
}

func (e *Engine) loadCounters() map[string]CounterState {
	out := make(map[string]CounterState)
	if data, err := os.ReadFile(e.store.CountersPath); err == nil {
		_ = json.Unmarshal(data, &out)
	}
	return out
}

func (e *Engine) saveCounters(c map[string]CounterState) {
	if data, err := json.MarshalIndent(c, "", "  "); err == nil {
		_ = os.WriteFile(e.store.CountersPath, data, 0640)
	}
}

func (e *Engine) loadHistory() {
	e.historyMu.Lock()
	defer e.historyMu.Unlock()

	loadFile := func(path string, target map[string][]HistoryPoint) {
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		var raw map[string][]HistoryPoint
		if json.Unmarshal(data, &raw) != nil {
			return
		}
		for k, pts := range raw {
			cleaned := make([]HistoryPoint, 0, len(pts))
			for _, pt := range pts {
				if pt.Tx > 12_500_000_000 || pt.Tx < 0 {
					pt.Tx = 0
				}
				if pt.Rx > 12_500_000_000 || pt.Rx < 0 {
					pt.Rx = 0
				}
				cleaned = append(cleaned, pt)
			}
			target[k] = cleaned
		}
	}

	loadFile(e.store.HourlyPath, e.hourlyHist)
	loadFile(e.store.DailyPath, e.dailyHist)
}

func (e *Engine) saveHistoryLocked() {
	if data, err := json.MarshalIndent(e.hourlyHist, "", "  "); err == nil {
		_ = os.WriteFile(e.store.HourlyPath, data, 0640)
	}
	if data, err := json.MarshalIndent(e.dailyHist, "", "  "); err == nil {
		_ = os.WriteFile(e.store.DailyPath, data, 0640)
	}
}

func (e *Engine) getAvgSpeedLocked(key string, duration float64) (int64, int64) {
	hl := e.liveHist[key]
	if len(hl) < 2 {
		return 0, 0
	}
	targetTs := float64(time.Now().Unix()) - duration
	bestIdx := 0
	minDiff := math.Abs(hl[0].Ts - targetTs)
	for idx, s := range hl {
		diff := math.Abs(s.Ts - targetTs)
		if diff < minDiff {
			minDiff = diff
			bestIdx = idx
		}
	}
	sStart := hl[bestIdx]
	sEnd := hl[len(hl)-1]
	dt := sEnd.Ts - sStart.Ts
	if dt <= 0 {
		return 0, 0
	}
	var txDiff, rxDiff uint64
	if sEnd.Tx >= sStart.Tx {
		txDiff = sEnd.Tx - sStart.Tx
	} else {
		txDiff = sEnd.Tx
	}
	if sEnd.Rx >= sStart.Rx {
		rxDiff = sEnd.Rx - sStart.Rx
	} else {
		rxDiff = sEnd.Rx
	}
	return int64(float64(txDiff)*8.0/dt), int64(float64(rxDiff)*8.0/dt)
}

func (e *Engine) PollOnce() {
	switches := e.store.GetDecryptedSwitches()
	var refreshInterval, macMultiplier, maxRetries int
	var infraDevices []config.InfraDevice
	e.store.Read(func(cfg *config.AppConfig) {
		refreshInterval = cfg.RefreshInterval
		macMultiplier = cfg.MACRefreshMultiplier
		maxRetries = cfg.MaxRequestRetries
		infraDevices = append([]config.InfraDevice(nil), cfg.InfrastructureDevices...)
	})
	if refreshInterval <= 0 {
		refreshInterval = 15
	}
	if macMultiplier <= 0 {
		macMultiplier = 5
	}

	e.countersMu.Lock()
	counters := e.loadCounters()
	e.countersMu.Unlock()

	now := float64(time.Now().UnixMilli()) / 1000.0
	results := make(map[string]scraper.SwitchData)
	speeds := make(map[string]map[string]PortSpeed)
	shouldSaveHist := false

	enabledIPs := make(map[string]bool)
	for _, sw := range switches {
		if sw.Enabled {
			enabledIPs[sw.IP] = true
		}
	}

	e.cacheMu.Lock()
	for ip := range e.cachedData {
		if !enabledIPs[ip] {
			delete(e.cachedData, ip)
		}
	}
	e.cacheMu.Unlock()

	for _, sw := range switches {
		if !sw.Enabled {
			continue
		}
		ip := sw.IP
		if strings.EqualFold(sw.Model, "internet") {
			results[ip] = scraper.SwitchData{
				Name:         sw.Name,
				IP:           ip,
				Model:        "internet",
				Status:       "online",
				Ports:        []scraper.PortData{},
				MACTable:     []scraper.MACEntry{},
				MACTimestamp: now,
				Timestamp:    now,
			}
			speeds[ip] = map[string]PortSpeed{}
			continue
		}

		e.cacheMu.RLock()
		lastMAC := e.lastMACScrape[ip]
		_, hasMAC := e.macTables[ip]
		prevData, hasPrevData := e.cachedData[ip]
		e.cacheMu.RUnlock()

		needMAC := float64(time.Now().UnixMilli())/1000.0-lastMAC >= float64(refreshInterval*macMultiplier) || !hasMAC

		sc := scraper.NewHCSwitchScraper(sw, e.store, maxRetries)
		sc.IncludeExtended = needMAC || !hasPrevData

		// 1. Scrape port status & traffic counters FIRST so bandwidth telemetry is never delayed by multi-page MAC scraping
		data := sc.Scrape()
		now = float64(time.Now().UnixMilli()) / 1000.0
		data.Timestamp = now

		if !sc.IncludeExtended && hasPrevData {
			data.DHCPSnooping = prevData.DHCPSnooping
			data.IGMP = prevData.IGMP
			data.JumboFrame = prevData.JumboFrame
		}

		e.cacheMu.RLock()
		data.MACTable = e.macTables[ip]
		data.MACTimestamp = e.lastMACScrape[ip]
		e.cacheMu.RUnlock()

		swSpeeds := make(map[string]PortSpeed)
		for i := range data.Ports {
			p := &data.Ports[i]
			key := fmt.Sprintf("%s:%s", ip, p.Port)
			last, hasLast := counters[key]
			curTx := p.TxBytes
			curRx := p.RxBytes

			var deltaTx, deltaRx uint64
			if !hasLast || last.Ts == nil {
				deltaTx = 0
				deltaRx = 0
			} else {
				if curTx >= last.Tx {
					deltaTx = curTx - last.Tx
				} else if last.Tx > 2_000_000_000 {
					deltaTx = (4_294_967_296 - last.Tx) + curTx
				} else {
					deltaTx = curTx
				}

				if curRx >= last.Rx {
					deltaRx = curRx - last.Rx
				} else if last.Rx > 2_000_000_000 {
					deltaRx = (4_294_967_296 - last.Rx) + curRx
				} else {
					deltaRx = curRx
				}

				// Sanity check physical link limit
				sLower := strings.ToLower(p.Speed)
				var linkBps float64 = 10_000_000_000
				switch {
				case strings.Contains(sLower, "10g"):
					linkBps = 10_000_000_000
				case strings.Contains(sLower, "2.5g") || strings.Contains(sLower, "2500"):
					linkBps = 2_500_000_000
				case strings.Contains(sLower, "2g") || strings.Contains(sLower, "2000"):
					linkBps = 2_000_000_000
				case strings.Contains(sLower, "1g") || strings.Contains(sLower, "1000"):
					linkBps = 1_000_000_000
				case strings.Contains(sLower, "100m") || strings.Contains(sLower, "100"):
					linkBps = 100_000_000
				case strings.Contains(sLower, "10m") || strings.Contains(sLower, "10"):
					linkBps = 10_000_000
				default:
					pLow := strings.ToLower(p.Port)
					if strings.Contains(pLow, "trunk") || strings.Contains(pLow, "lag") {
						linkBps = 20_000_000_000
					}
				}
				dt := now - *last.Ts
				if dt <= 0 {
					dt = float64(refreshInterval)
				}
				maxBytes := uint64((linkBps * dt * 1.5) / 8.0)
				if deltaTx > maxBytes {
					deltaTx = 0
				}
				if deltaRx > maxBytes {
					deltaRx = 0
				}
			}

			cumTx := last.CumTx + deltaTx
			cumRx := last.CumRx + deltaRx
			tsCopy := now
			counters[key] = CounterState{
				Tx:    curTx,
				Rx:    curRx,
				CumTx: cumTx,
				CumRx: cumRx,
				Ts:    &tsCopy,
			}
			p.CumTx = cumTx
			p.CumRx = cumRx

			e.historyMu.Lock()
			hl := e.liveHist[key]
			hl = append(hl, LiveSample{Ts: now, Tx: cumTx, Rx: cumRx})
			if len(hl) > 120 {
				hl = hl[len(hl)-120:]
			}
			e.liveHist[key] = hl

			if len(hl) >= 2 {
				p1 := hl[len(hl)-2]
				p2 := hl[len(hl)-1]
				dt := p2.Ts - p1.Ts
				if dt > 0 {
					var dTx, dRx uint64
					if p2.Tx >= p1.Tx {
						dTx = p2.Tx - p1.Tx
					} else {
						dTx = p2.Tx
					}
					if p2.Rx >= p1.Rx {
						dRx = p2.Rx - p1.Rx
					} else {
						dRx = p2.Rx
					}
					p.SpeedTxBps = int64(float64(dTx) * 8.0 / dt)
					p.SpeedRxBps = int64(float64(dRx) * 8.0 / dt)
				}
			}

			// Hourly history
			maxPts := 3600 / refreshInterval
			if maxPts < 1 {
				maxPts = 1
			}
			ptsH := append(e.hourlyHist[key], HistoryPoint{Ts: now, Tx: p.SpeedTxBps, Rx: p.SpeedRxBps})
			if len(ptsH) > maxPts {
				ptsH = ptsH[len(ptsH)-maxPts:]
			}
			e.hourlyHist[key] = ptsH
			shouldSaveHist = true

			// Daily history (15-min intervals)
			ptsD := e.dailyHist[key]
			if len(ptsD) == 0 || now-ptsD[len(ptsD)-1].Ts >= 900 {
				avgTx, avgRx := e.getAvgSpeedLocked(key, 900)
				ptsD = append(ptsD, HistoryPoint{Ts: now, Tx: avgTx, Rx: avgRx})
				if len(ptsD) > 96 {
					ptsD = ptsD[len(ptsD)-96:]
				}
				e.dailyHist[key] = ptsD
				shouldSaveHist = true
			}
			e.historyMu.Unlock()

			swSpeeds[p.Port] = PortSpeed{
				SpeedTx: p.SpeedTxBps,
				SpeedRx: p.SpeedRxBps,
			}
		}

		// Publish updated port telemetry immediately into live cache before any slow MAC table scrape
		e.cacheMu.Lock()
		e.cachedData[ip] = data
		e.cachedSpeeds[ip] = swSpeeds
		e.cacheMu.Unlock()

		// 2. Scrape MAC table afterwards when due
		if needMAC {
			e.store.Logger().Info("poller", "Scraping MAC table for %s (%s)...", sw.Name, ip)
			if tbl, err := sc.ScrapeMACTable(); err == nil {
				macNow := float64(time.Now().UnixMilli()) / 1000.0
				e.cacheMu.Lock()
				e.macTables[ip] = tbl
				e.lastMACScrape[ip] = macNow
				data.MACTable = tbl
				data.MACTimestamp = macNow
				e.cachedData[ip] = data
				e.cacheMu.Unlock()
			} else {
				e.store.Logger().Warn("poller", "MAC scrape failed for %s: %v", ip, err)
				e.cacheMu.Lock()
				if _, ok := e.macTables[ip]; !ok {
					e.macTables[ip] = []scraper.MACEntry{}
				}
				e.cacheMu.Unlock()
			}
		}

		results[ip] = data
		speeds[ip] = swSpeeds
	}

	// Update passive client discovery from switch MAC tables
	swMACs := make(map[string]bool)
	scrapedIPs := make(map[string]bool)
	for ip, d := range results {
		if d.Error == "" {
			scrapedIPs[ip] = true
		}
		if m := config.NormalizeMAC(d.MAC); m != "" {
			swMACs[m] = true
		}
	}
	infraMACs := make(map[string]bool)
	for _, dev := range infraDevices {
		if m := config.NormalizeMAC(dev.MAC); m != "" {
			infraMACs[m] = true
		}
	}

	type learnedClient struct {
		MAC  string
		IP   string
		Port string
		VLAN string
	}
	activeClients := make(map[string]learnedClient)
	for swIP := range scrapedIPs {
		swData := results[swIP]
		portMACs := make(map[string][]scraper.MACEntry)
		for _, entry := range swData.MACTable {
			portMACs[entry.Port] = append(portMACs[entry.Port], entry)
		}
		for port, entries := range portMACs {
			hasSwitch := false
			for _, entry := range entries {
				if swMACs[config.NormalizeMAC(entry.MAC)] {
					hasSwitch = true
					break
				}
			}
			if !hasSwitch {
				for _, entry := range entries {
					norm := config.NormalizeMAC(entry.MAC)
					if norm != "" && !swMACs[norm] && !infraMACs[norm] && !e.store.IsIgnoredMAC(norm) {
						activeClients[norm] = learnedClient{
							MAC:  config.FormatMAC(norm),
							IP:   swIP,
							Port: port,
							VLAN: entry.VLAN,
						}
					}
				}
			}
		}
	}

	_ = e.store.Update(func(cfg *config.AppConfig) error {
		if cfg.Clients == nil {
			cfg.Clients = make(map[string]config.ClientEntry)
		}
		for mac, info := range activeClients {
			c, exists := cfg.Clients[mac]
			if !exists {
				c = config.ClientEntry{
					MAC:        info.MAC,
					DeviceType: "laptop",
				}
			}
			c.MAC = info.MAC
			c.IP = info.IP
			c.Port = info.Port
			c.VLAN = info.VLAN
			c.Status = "online"
			c.LastSeen = now
			cfg.Clients[mac] = c
		}
		for mac, c := range cfg.Clients {
			if _, ok := activeClients[mac]; !ok {
				if scrapedIPs[c.IP] {
					c.Status = "offline"
					cfg.Clients[mac] = c
				}
			}
		}
		return nil
	})

	e.countersMu.Lock()
	e.saveCounters(counters)
	e.countersMu.Unlock()

	if shouldSaveHist {
		e.historyMu.RLock()
		e.saveHistoryLocked()
		e.historyMu.RUnlock()
	}

	e.cacheMu.Lock()
	e.cachedData = results
	e.cachedSpeeds = speeds
	e.cacheMu.Unlock()
}

func (e *Engine) GetSwitchesData() []scraper.SwitchData {
	customVendors := e.store.LoadCustomVendors()
	ieeeVendors := e.store.GetIEEEVendors()

	var switches []config.SwitchConfig
	var notes map[string]string
	var clients map[string]config.ClientEntry
	e.store.Read(func(cfg *config.AppConfig) {
		switches = append([]config.SwitchConfig(nil), cfg.Switches...)
		notes = make(map[string]string, len(cfg.Notes))
		for k, v := range cfg.Notes {
			notes[k] = v
		}
		clients = make(map[string]config.ClientEntry, len(cfg.Clients))
		for k, v := range cfg.Clients {
			clients[k] = v
		}
	})

	e.cacheMu.RLock()
	defer e.cacheMu.RUnlock()

	var out []scraper.SwitchData
	seen := make(map[string]bool)
	for _, sw := range switches {
		if !sw.Enabled || strings.EqualFold(sw.Model, "internet") || seen[sw.IP] {
			continue
		}
		seen[sw.IP] = true
		d, ok := e.cachedData[sw.IP]
		if !ok {
			continue
		}
		// Copy slice structures so we can enrich MAC vendors, hosts, and port notes safely
		dCopy := d
		portsCopy := make([]scraper.PortData, len(d.Ports))
		copy(portsCopy, d.Ports)
		for i := range portsCopy {
			noteKey := fmt.Sprintf("%s:%s", sw.IP, portsCopy[i].Port)
			portsCopy[i].Note = notes[noteKey]
		}
		dCopy.Ports = portsCopy

		var macsCopy []scraper.MACEntry
		for _, m := range d.MACTable {
			if e.store.IsIgnoredMAC(m.MAC) {
				continue
			}
			m.Vendor = e.store.LookupVendor(m.MAC, customVendors, ieeeVendors)
			norm := config.NormalizeMAC(m.MAC)
			if c, ok := clients[norm]; ok {
				m.Host = c.Host
			}
			macsCopy = append(macsCopy, m)
		}
		if macsCopy == nil {
			macsCopy = []scraper.MACEntry{}
		}
		dCopy.MACTable = macsCopy
		out = append(out, dCopy)
	}
	return out
}

func (e *Engine) RefreshSwitchMAC(ip string) ([]scraper.MACEntry, error) {
	switches := e.store.GetDecryptedSwitches()
	var target *config.SwitchConfig
	var maxRetries int
	e.store.Read(func(cfg *config.AppConfig) {
		maxRetries = cfg.MaxRequestRetries
	})
	for i := range switches {
		if switches[i].IP == ip {
			target = &switches[i]
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("switch %s not found", ip)
	}
	if strings.EqualFold(target.Model, "internet") {
		return []scraper.MACEntry{}, nil
	}

	sc := scraper.NewHCSwitchScraper(*target, e.store, maxRetries)
	macTable, err := sc.ScrapeMACTable()
	if err != nil {
		return nil, err
	}

	customVendors := e.store.LoadCustomVendors()
	ieeeVendors := e.store.GetIEEEVendors()
	var filtered []scraper.MACEntry
	for _, entry := range macTable {
		if e.store.IsIgnoredMAC(entry.MAC) {
			continue
		}
		entry.Vendor = e.store.LookupVendor(entry.MAC, customVendors, ieeeVendors)
		filtered = append(filtered, entry)
	}
	if filtered == nil {
		filtered = []scraper.MACEntry{}
	}

	now := float64(time.Now().Unix())
	e.cacheMu.Lock()
	e.macTables[ip] = filtered
	e.lastMACScrape[ip] = now
	if d, ok := e.cachedData[ip]; ok {
		d.MACTable = filtered
		d.MACTimestamp = now
		e.cachedData[ip] = d
	}
	e.cacheMu.Unlock()

	return filtered, nil
}

func (e *Engine) GetSpeeds() map[string]map[string]PortSpeed {
	e.cacheMu.RLock()
	defer e.cacheMu.RUnlock()
	out := make(map[string]map[string]PortSpeed, len(e.cachedSpeeds))
	for ip, m := range e.cachedSpeeds {
		inner := make(map[string]PortSpeed, len(m))
		for p, s := range m {
			inner[p] = s
		}
		out[ip] = inner
	}
	return out
}

func (e *Engine) GetHistory(ip, port, rangeType string) ([]int64, []int64, []float64) {
	key := fmt.Sprintf("%s:%s", ip, port)
	tx := []int64{}
	rx := []int64{}
	timestamps := []float64{}

	e.historyMu.RLock()
	defer e.historyMu.RUnlock()

	switch rangeType {
	case "1h":
		for _, pt := range e.hourlyHist[key] {
			tx = append(tx, pt.Tx)
			rx = append(rx, pt.Rx)
			timestamps = append(timestamps, pt.Ts)
		}
	case "24h":
		for _, pt := range e.dailyHist[key] {
			tx = append(tx, pt.Tx)
			rx = append(rx, pt.Rx)
			timestamps = append(timestamps, pt.Ts)
		}
	default: // "live"
		hl := e.liveHist[key]
		for i := 1; i < len(hl); i++ {
			dt := hl[i].Ts - hl[i-1].Ts
			if dt > 0 {
				var dTx, dRx uint64
				if hl[i].Tx >= hl[i-1].Tx {
					dTx = hl[i].Tx - hl[i-1].Tx
				} else {
					dTx = hl[i].Tx
				}
				if hl[i].Rx >= hl[i-1].Rx {
					dRx = hl[i].Rx - hl[i-1].Rx
				} else {
					dRx = hl[i].Rx
				}
				tx = append(tx, int64(float64(dTx)*8.0/dt))
				rx = append(rx, int64(float64(dRx)*8.0/dt))
				timestamps = append(timestamps, hl[i].Ts)
			}
		}
	}
	return tx, rx, timestamps
}

func (e *Engine) ResetHistoryAndCounters() {
	e.countersMu.Lock()
	_ = os.Remove(e.store.CountersPath)
	e.countersMu.Unlock()

	e.historyMu.Lock()
	_ = os.Remove(e.store.HourlyPath)
	_ = os.Remove(e.store.DailyPath)
	e.liveHist = make(map[string][]LiveSample)
	e.hourlyHist = make(map[string][]HistoryPoint)
	e.dailyHist = make(map[string][]HistoryPoint)
	e.historyMu.Unlock()
}

func (e *Engine) BuildTopology() map[string]any {
	customVendors := e.store.LoadCustomVendors()
	ieeeVendors := e.store.GetIEEEVendors()

	var switches []config.SwitchConfig
	var unmanagedList []config.UnmanagedSwitch
	var infraList []config.InfraDevice
	var dbClients map[string]config.ClientEntry

	e.store.Read(func(cfg *config.AppConfig) {
		switches = append([]config.SwitchConfig(nil), cfg.Switches...)
		unmanagedList = append([]config.UnmanagedSwitch(nil), cfg.UnmanagedSwitches...)
		infraList = append([]config.InfraDevice(nil), cfg.InfrastructureDevices...)
		dbClients = make(map[string]config.ClientEntry, len(cfg.Clients))
		for k, v := range cfg.Clients {
			dbClients[k] = v
		}
	})

	unmanagedByPort := make(map[string]map[string]string)
	for _, us := range unmanagedList {
		if us.ParentIP != "" && us.ParentPort != "" {
			key := us.ParentIP + "|" + us.ParentPort
			name := us.Name
			if name == "" {
				name = "Unmanaged Switch"
			}
			unmanagedByPort[key] = map[string]string{
				"id":          fmt.Sprintf("unmanaged_%s_%s", us.ParentIP, us.ParentPort),
				"name":        name,
				"parent_ip":   us.ParentIP,
				"parent_port": us.ParentPort,
			}
		}
	}

	e.cacheMu.RLock()
	swDataCopy := make(map[string]scraper.SwitchData, len(e.cachedData))
	for k, v := range e.cachedData {
		swDataCopy[k] = v
	}
	e.cacheMu.RUnlock()

	type swNodeInfo struct {
		IP       string
		Name     string
		Model    string
		MAC      string
		Ports    []scraper.PortData
		MACTable []scraper.MACEntry
		Status   string
	}
	switchesByIP := make(map[string]swNodeInfo)
	macToSwitchIP := make(map[string]string)

	for _, sw := range switches {
		if !sw.Enabled {
			continue
		}
		swData, hasData := swDataCopy[sw.IP]
		swMAC := config.NormalizeMAC(swData.MAC)
		status := "offline"
		if hasData && swData.Error == "" {
			status = "online"
		}
		switchesByIP[sw.IP] = swNodeInfo{
			IP:       sw.IP,
			Name:     sw.Name,
			Model:    sw.Model,
			MAC:      swMAC,
			Ports:    swData.Ports,
			MACTable: swData.MACTable,
			Status:   status,
		}
		if swMAC != "" {
			macToSwitchIP[swMAC] = sw.IP
		}
	}

	infraByMAC := make(map[string]map[string]string)
	var routerMAC string
	for _, dev := range infraList {
		norm := config.NormalizeMAC(dev.MAC)
		if norm == "" || macToSwitchIP[norm] != "" {
			continue
		}
		devType := dev.Type
		if devType == "" {
			devType = "other"
		}
		infraByMAC[norm] = map[string]string{
			"mac":    norm,
			"name":   dev.Name,
			"type":   devType,
			"vendor": e.store.LookupVendor(norm, customVendors, ieeeVendors),
		}
		if devType == "router" && routerMAC == "" {
			routerMAC = norm
		}
	}

	swPortMACs := make(map[string]map[string][]string)
	for ip, sw := range switchesByIP {
		swPortMACs[ip] = make(map[string][]string)
		for _, entry := range sw.MACTable {
			port := strings.TrimSpace(strings.TrimPrefix(entry.Port, "Port "))
			mac := config.NormalizeMAC(entry.MAC)
			if port != "" && mac != "" && !e.store.IsIgnoredMAC(mac) {
				exists := false
				for _, m := range swPortMACs[ip][port] {
					if m == mac {
						exists = true
						break
					}
				}
				if !exists {
					swPortMACs[ip][port] = append(swPortMACs[ip][port], mac)
				}
			}
		}
	}

	type directLink struct {
		SrcIP string
		Port  string
		DstIP string
	}
	var directLinks []directLink
	for srcIP, ports := range swPortMACs {
		srcMAC := switchesByIP[srcIP].MAC
		for port, macs := range ports {
			var swOnPort []string
			for _, m := range macs {
				if dstIP, ok := macToSwitchIP[m]; ok {
					swOnPort = append(swOnPort, dstIP)
				}
			}
			for _, tIP := range swOnPort {
				tMAC := switchesByIP[tIP].MAC
				isBehind := false
				for _, uIP := range swOnPort {
					if uIP == tIP {
						continue
					}
					uPorts := swPortMACs[uIP]
					var portUT, portUS string
					for uP, uMACs := range uPorts {
						for _, um := range uMACs {
							if um == tMAC {
								portUT = uP
							}
							if um == srcMAC {
								portUS = uP
							}
						}
					}
					if portUS == "" && routerMAC != "" {
						for uP, uMACs := range uPorts {
							for _, um := range uMACs {
								if um == routerMAC {
									portUS = uP
									break
								}
							}
						}
					}
					if portUT != "" && portUS != "" && portUT != portUS {
						isBehind = true
						break
					}
				}
				if !isBehind {
					directLinks = append(directLinks, directLink{SrcIP: srcIP, Port: port, DstIP: tIP})
				}
			}
		}
	}

	staticUplinks := make(map[string]config.SwitchConfig)
	for _, sw := range switches {
		if sw.Enabled && sw.ParentIP != "" && sw.ParentPort != "" {
			staticUplinks[sw.IP] = sw
		}
	}

	processedLinks := make(map[string]bool)
	var links []map[string]any

	linkKey := func(a, b string) string {
		pair := []string{a, b}
		sort.Strings(pair)
		return pair[0] + "<->" + pair[1]
	}

	for childIP, info := range staticUplinks {
		if _, ok := switchesByIP[childIP]; !ok {
			continue
		}
		pIP := info.ParentIP
		pPort := info.ParentPort
		upPort := info.UplinkPort

		speed := "Unknown"
		var txBps, rxBps int64
		if pSw, ok := switchesByIP[pIP]; ok {
			for _, pInfo := range pSw.Ports {
				if strings.EqualFold(pInfo.Port, pPort) || strings.EqualFold("Port "+pInfo.Port, pPort) {
					speed = pInfo.Speed
					txBps = pInfo.SpeedTxBps
					rxBps = pInfo.SpeedRxBps
					break
				}
			}
		}
		srcLabel := pPort
		if !strings.Contains(strings.ToLower(pPort), "port") && !strings.Contains(strings.ToLower(pPort), "lan") && !strings.Contains(strings.ToLower(pPort), "wan") {
			srcLabel = "Port " + pPort
		}
		tgtLabel := "unknown"
		if upPort != "" {
			if strings.Contains(strings.ToLower(upPort), "port") {
				tgtLabel = upPort
			} else {
				tgtLabel = "Port " + upPort
			}
		}
		links = append(links, map[string]any{
			"source":      pIP,
			"target":      childIP,
			"source_port": srcLabel,
			"target_port": tgtLabel,
			"speed":       speed,
			"tx_bps":      txBps,
			"rx_bps":      rxBps,
			"type":        "uplink",
		})
		processedLinks[linkKey(pIP, childIP)] = true
	}

	swLinkPorts := make(map[string]string)
	for _, dl := range directLinks {
		swLinkPorts[dl.SrcIP+"->"+dl.DstIP] = dl.Port
	}
	for _, dl := range directLinks {
		lk := linkKey(dl.SrcIP, dl.DstIP)
		if processedLinks[lk] {
			continue
		}
		if st, ok := staticUplinks[dl.DstIP]; ok && st.ParentIP != dl.SrcIP {
			continue
		}
		processedLinks[lk] = true
		dstPort := swLinkPorts[dl.DstIP+"->"+dl.SrcIP]
		tgtLabel := "unknown"
		if dstPort != "" {
			tgtLabel = "Port " + dstPort
		}
		speed := "Unknown"
		var txBps, rxBps int64
		for _, pInfo := range switchesByIP[dl.SrcIP].Ports {
			if pInfo.Port == dl.Port {
				speed = pInfo.Speed
				txBps = pInfo.SpeedTxBps
				rxBps = pInfo.SpeedRxBps
				break
			}
		}
		links = append(links, map[string]any{
			"source":      dl.SrcIP,
			"target":      dl.DstIP,
			"source_port": "Port " + dl.Port,
			"target_port": tgtLabel,
			"speed":       speed,
			"tx_bps":      txBps,
			"rx_bps":      rxBps,
			"type":        "uplink",
		})
	}

	// Infra connections
	type ipPort struct {
		IP   string
		Port string
	}
	infraConns := make(map[string]ipPort)
	for mac := range infraByMAC {
		for ip, ports := range swPortMACs {
			for port, macs := range ports {
				hasMAC := false
				hasSw := false
				for _, m := range macs {
					if m == mac {
						hasMAC = true
					}
					if macToSwitchIP[m] != "" {
						hasSw = true
					}
				}
				if hasMAC && !hasSw {
					infraConns[mac] = ipPort{IP: ip, Port: port}
					break
				}
			}
		}
	}

	for mac, conn := range infraConns {
		speed := "Unknown"
		for _, pInfo := range switchesByIP[conn.IP].Ports {
			if pInfo.Port == conn.Port {
				speed = pInfo.Speed
				break
			}
		}
		sourceID := conn.IP
		sourcePort := "Port " + conn.Port
		if us, ok := unmanagedByPort[conn.IP+"|"+conn.Port]; ok {
			sourceID = us["id"]
			sourcePort = ""
		}
		links = append(links, map[string]any{
			"source":      sourceID,
			"target":      mac,
			"source_port": sourcePort,
			"target_port": "",
			"speed":       speed,
			"type":        "infra",
		})
	}

	// Client connections
	activeMACsToPort := make(map[string]ipPort)
	allClientMACs := make(map[string]bool)
	for k := range dbClients {
		allClientMACs[k] = true
	}
	for ip, ports := range swPortMACs {
		for port, macs := range ports {
			hasSw := false
			for _, m := range macs {
				if macToSwitchIP[m] != "" {
					hasSw = true
					break
				}
			}
			if !hasSw {
				for _, m := range macs {
					if macToSwitchIP[m] == "" && infraByMAC[m] == nil {
						activeMACsToPort[m] = ipPort{IP: ip, Port: port}
						allClientMACs[m] = true
					}
				}
			}
		}
	}

	allSwitchIPs := make(map[string]bool)
	for _, sw := range switches {
		allSwitchIPs[sw.IP] = true
	}

	var clientNodes []map[string]any
	var clientLinks []map[string]any

	for mac := range allClientMACs {
		if e.store.IsIgnoredMAC(mac) || macToSwitchIP[mac] != "" || infraByMAC[mac] != nil {
			continue
		}
		cEntry := dbClients[mac]
		conn, isActive := activeMACsToPort[mac]
		ip := conn.IP
		port := conn.Port
		status := "online"
		if !isActive {
			ip = cEntry.IP
			port = strings.TrimSpace(strings.TrimPrefix(cEntry.Port, "Port "))
			status = "offline"
		}
		if ip == "" || port == "" {
			continue
		}
		if _, isSw := allSwitchIPs[ip]; isSw {
			if _, enabledSw := switchesByIP[ip]; !enabledSw {
				continue
			}
		}

		var attachedInfra string
		for infraMAC, ic := range infraConns {
			if ic.IP == ip && ic.Port == port {
				attachedInfra = infraMAC
				break
			}
		}

		vendor := e.store.LookupVendor(mac, customVendors, ieeeVendors)
		fmtMAC := cEntry.MAC
		if fmtMAC == "" {
			fmtMAC = config.FormatMAC(mac)
		}
		displayName := cEntry.Host
		if displayName == "" {
			if vendor != "" {
				displayName = vendor
			} else if len(fmtMAC) >= 8 {
				displayName = "Client " + fmtMAC[len(fmtMAC)-8:]
			} else {
				displayName = fmtMAC
			}
		}
		devType := cEntry.DeviceType
		if devType == "" {
			devType = "laptop"
		}

		clientNodes = append(clientNodes, map[string]any{
			"id":             mac,
			"name":           displayName,
			"type":           "client",
			"device_type":    devType,
			"mac":            fmtMAC,
			"vendor":         vendor,
			"status":         status,
			"host":           cEntry.Host,
			"last_seen_ip":   ip,
			"last_seen_port": port,
			"last_seen_time": cEntry.LastSeen,
		})

		sourceNode := ip
		sourcePort := "Port " + port
		if attachedInfra != "" {
			sourceNode = attachedInfra
			sourcePort = ""
		} else if us, ok := unmanagedByPort[ip+"|"+port]; ok {
			sourceNode = us["id"]
			sourcePort = ""
		}

		speed := ""
		if attachedInfra == "" && isActive {
			speed = "Unknown"
			for _, pInfo := range switchesByIP[ip].Ports {
				if pInfo.Port == port {
					speed = pInfo.Speed
					break
				}
			}
		} else if attachedInfra == "" && !isActive {
			speed = "offline"
		}

		clientLinks = append(clientLinks, map[string]any{
			"source":      sourceNode,
			"target":      mac,
			"source_port": sourcePort,
			"target_port": "",
			"speed":       speed,
			"type":        "client",
		})
	}

	var nodes []map[string]any
	for _, sw := range switches {
		if !sw.Enabled {
			continue
		}
		info, ok := switchesByIP[sw.IP]
		if !ok {
			continue
		}
		if strings.EqualFold(info.Model, "internet") {
			nodes = append(nodes, map[string]any{
				"id":     "internet",
				"name":   info.Name,
				"type":   "internet",
				"status": "online",
			})
			continue
		}
		nodes = append(nodes, map[string]any{
			"id":     info.IP,
			"name":   info.Name,
			"type":   "switch",
			"ip":     info.IP,
			"mac":    info.MAC,
			"model":  info.Model,
			"status": info.Status,
		})
	}

	for mac, dev := range infraByMAC {
		status := "offline"
		if _, ok := infraConns[mac]; ok || dev["type"] == "router" {
			status = "online"
		}
		nodes = append(nodes, map[string]any{
			"id":     mac,
			"name":   dev["name"],
			"type":   dev["type"],
			"mac":    mac,
			"vendor": dev["vendor"],
			"status": status,
		})
	}

	for _, us := range unmanagedByPort {
		pStatus := "online"
		if pSw, ok := switchesByIP[us["parent_ip"]]; ok {
			pStatus = pSw.Status
		}
		nodes = append(nodes, map[string]any{
			"id":          us["id"],
			"name":        us["name"],
			"type":        "unmanaged_switch",
			"status":      pStatus,
			"parent_ip":   us["parent_ip"],
			"parent_port": us["parent_port"],
		})
		speed := "Unknown"
		if pSw, ok := switchesByIP[us["parent_ip"]]; ok {
			for _, pInfo := range pSw.Ports {
				if pInfo.Port == us["parent_port"] {
					speed = pInfo.Speed
					break
				}
			}
		}
		links = append(links, map[string]any{
			"source":      us["parent_ip"],
			"target":      us["id"],
			"source_port": "Port " + us["parent_port"],
			"target_port": "",
			"speed":       speed,
			"type":        "infra",
		})
	}

	nodes = append(nodes, clientNodes...)
	allLinks := append(links, clientLinks...)
	if nodes == nil {
		nodes = []map[string]any{}
	}
	if allLinks == nil {
		allLinks = []map[string]any{}
	}

	return map[string]any{
		"nodes": nodes,
		"links": allLinks,
	}
}
