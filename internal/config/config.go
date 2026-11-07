package config

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"horaco-switch-dashboard/internal/auth"
)

const Version = "2.1.0"

var safeNameRegex = regexp.MustCompile(`^[a-zA-Z0-9._\-]+$`)

type SwitchConfig struct {
	Name        string `json:"name"`
	IP          string `json:"ip"`
	Username    string `json:"username"`
	Password    string `json:"password,omitempty"`
	HasPassword bool   `json:"has_password,omitempty"`
	Model       string `json:"model"`
	PortCount   int    `json:"port_count"`
	Enabled     bool   `json:"enabled"`
	ParentIP    string `json:"parent_ip,omitempty"`
	ParentPort  string `json:"parent_port,omitempty"`
	UplinkPort  string `json:"uplink_port,omitempty"`
}

type InfraDevice struct {
	Name string `json:"name"`
	MAC  string `json:"mac"`
	Type string `json:"type"`
}

type UnmanagedSwitch struct {
	Name       string `json:"name"`
	ParentIP   string `json:"parent_ip"`
	ParentPort string `json:"parent_port"`
}

type ClientEntry struct {
	MAC        string  `json:"mac"`
	Host       string  `json:"host"`
	DeviceType string  `json:"device_type,omitempty"`
	IP         string  `json:"ip"`
	Port       string  `json:"port"`
	VLAN       string  `json:"vlan"`
	Vendor     string  `json:"vendor,omitempty"`
	Note       string  `json:"note,omitempty"`
	Status     string  `json:"status"`
	LastSeen   float64 `json:"last_seen"`
}

type Settings struct {
	LogLevel    string   `json:"log_level,omitempty"`
	IgnoredMACs []string `json:"ignored_macs,omitempty"`
}

type AuthConfig struct {
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
}

type AppConfig struct {
	Title                 string                        `json:"title"`
	RefreshInterval       int                           `json:"refresh_interval"`
	MACRefreshMultiplier  int                           `json:"mac_refresh_multiplier"`
	PortsWrapThreshold    int                           `json:"ports_wrap_threshold"`
	MaxRequestRetries     int                           `json:"max_request_retries"`
	EnabledColumns        []string                      `json:"enabled_columns"`
	GridColumns           string                        `json:"grid_columns"`
	Switches              []SwitchConfig                `json:"switches"`
	InfrastructureDevices []InfraDevice                 `json:"infrastructure_devices"`
	UnmanagedSwitches     []UnmanagedSwitch             `json:"unmanaged_switches"`
	Clients               map[string]ClientEntry        `json:"clients"`
	Notes                 map[string]string             `json:"notes"`
	Settings              Settings                      `json:"settings"`
	ColumnWidths          map[string]any                `json:"column_widths,omitempty"`
	ColumnOrder           []string                      `json:"column_order,omitempty"`
	MapPositions          map[string]map[string]float64 `json:"map_positions,omitempty"`
	Auth                  AuthConfig                    `json:"auth"`
}

type Store struct {
	mu                 sync.RWMutex
	BaseDir            string
	DataDir            string
	ConfigPath         string
	DeviceTypesPath    string
	CountersPath       string
	HourlyPath         string
	DailyPath          string
	BackupDir          string
	DeviceTemplatesDir string
	VendorsTxtPath     string
	OUI36TxtPath       string
	OUITxtPath         string
	LogFilePath        string

	Vault  *auth.Vault
	cfg    AppConfig
	logger *RingLogger

	ouiMu       sync.RWMutex
	ieeeVendors map[string]string
}

type RingLogger struct {
	mu       sync.Mutex
	lines    []string
	maxLines int
	level    int
	levelStr string
	filePath string
}

const (
	LevelDebug = 10
	LevelInfo  = 20
	LevelWarn  = 30
	LevelError = 40
	LevelNone  = 99
)

func parseLogLevel(lvl string) (int, string) {
	switch strings.ToUpper(strings.TrimSpace(lvl)) {
	case "DEBUG":
		return LevelDebug, "DEBUG"
	case "WARN", "WARNING":
		return LevelWarn, "WARNING"
	case "ERROR", "CRITICAL":
		return LevelError, "ERROR"
	case "NONE":
		return LevelNone, "NONE"
	default:
		return LevelInfo, "INFO"
	}
}

func NewRingLogger(filePath, levelStr string) *RingLogger {
	_ = os.MkdirAll(filepath.Dir(filePath), 0750)
	lvl, norm := parseLogLevel(levelStr)
	rl := &RingLogger{
		lines:    make([]string, 0, 300),
		maxLines: 300,
		level:    lvl,
		levelStr: norm,
		filePath: filePath,
	}
	// Preload tail from existing file if present
	if f, err := os.Open(filePath); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			rl.appendLineLocked(sc.Text())
		}
	}
	return rl
}

func (l *RingLogger) appendLineLocked(line string) {
	if len(l.lines) >= l.maxLines {
		l.lines = l.lines[1:]
	}
	l.lines = append(l.lines, line)
}

func (l *RingLogger) SetLevel(levelStr string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	lvl, norm := parseLogLevel(levelStr)
	l.level = lvl
	l.levelStr = norm
	return norm
}

func (l *RingLogger) GetLevel() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.levelStr
}

func (l *RingLogger) Log(level int, levelName, component, format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if level < l.level {
		return
	}
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("%s - %s - %s - %s", time.Now().Format("2006-01-02 15:04:05,000"), component, levelName, msg)
	l.appendLineLocked(line)
	fmt.Println(line)
	if f, err := os.OpenFile(l.filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0640); err == nil {
		_, _ = f.WriteString(line + "\n")
		_ = f.Close()
	}
}

func (l *RingLogger) Debug(component, format string, args ...any) {
	l.Log(LevelDebug, "DEBUG", component, format, args...)
}

func (l *RingLogger) Info(component, format string, args ...any) {
	l.Log(LevelInfo, "INFO", component, format, args...)
}

func (l *RingLogger) Warn(component, format string, args ...any) {
	l.Log(LevelWarn, "WARNING", component, format, args...)
}

func (l *RingLogger) Error(component, format string, args ...any) {
	l.Log(LevelError, "ERROR", component, format, args...)
}

func (l *RingLogger) Recent(limit int) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if limit <= 0 || limit > len(l.lines) {
		limit = len(l.lines)
	}
	start := len(l.lines) - limit
	out := make([]string, limit)
	copy(out, l.lines[start:])
	return out
}

func (l *RingLogger) Clear() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = l.lines[:0]
	return os.WriteFile(l.filePath, []byte{}, 0640)
}

func NewStore(baseDir, dataDir string) (*Store, error) {
	if dataDir == "" {
		dataDir = baseDir
	}
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return nil, err
	}

	vault, err := auth.NewVault(dataDir)
	if err != nil {
		return nil, err
	}

	s := &Store{
		BaseDir:            baseDir,
		DataDir:            dataDir,
		ConfigPath:         filepath.Join(dataDir, "config.json"),
		DeviceTypesPath:    filepath.Join(dataDir, "device_types.yaml"),
		CountersPath:       filepath.Join(dataDir, "counters.json"),
		HourlyPath:         filepath.Join(dataDir, "history_hourly.json"),
		DailyPath:          filepath.Join(dataDir, "history_daily.json"),
		BackupDir:          filepath.Join(dataDir, "backup"),
		DeviceTemplatesDir: filepath.Join(dataDir, "device-templates"),
		VendorsTxtPath:     filepath.Join(dataDir, "mac_vendors.txt"),
		OUI36TxtPath:       filepath.Join(dataDir, "oui36.txt"),
		OUITxtPath:         filepath.Join(dataDir, "oui.txt"),
		LogFilePath:        filepath.Join(dataDir, "logs", "dashboard.log"),
		Vault:              vault,
	}

	_ = os.MkdirAll(s.DeviceTemplatesDir, 0750)
	_ = os.MkdirAll(s.BackupDir, 0750)

	// Seed default files if dataDir != baseDir
	if dataDir != baseDir {
		s.copyIfMissing(filepath.Join(baseDir, "device_types.yaml"), s.DeviceTypesPath)
		s.copyIfMissing(filepath.Join(baseDir, "mac_vendors.txt"), s.VendorsTxtPath)
		srcTplDir := filepath.Join(baseDir, "device-templates")
		if entries, err := os.ReadDir(srcTplDir); err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					s.copyIfMissing(filepath.Join(srcTplDir, e.Name()), filepath.Join(s.DeviceTemplatesDir, e.Name()))
				}
			}
		}
	}

	s.logger = NewRingLogger(s.LogFilePath, "INFO")
	s.Load()
	if s.cfg.Settings.LogLevel != "" {
		s.logger.SetLevel(s.cfg.Settings.LogLevel)
	}

	// Optional initial admin password via environment variable
	if envPass := os.Getenv("DASHBOARD_ADMIN_PASSWORD"); envPass != "" && s.cfg.Auth.PasswordHash == "" {
		if hash, err := auth.HashPassword(envPass); err == nil {
			s.mu.Lock()
			s.cfg.Auth.Username = "admin"
			s.cfg.Auth.PasswordHash = hash
			_ = s.saveLocked()
			s.mu.Unlock()
			s.logger.Info("auth", "Initialized administrator account from DASHBOARD_ADMIN_PASSWORD")
		}
	}

	return s, nil
}

func (s *Store) Logger() *RingLogger {
	return s.logger
}

func (s *Store) copyIfMissing(src, dst string) {
	if _, err := os.Stat(dst); err == nil {
		return
	}
	if data, err := os.ReadFile(src); err == nil {
		_ = os.WriteFile(dst, data, 0640)
	}
}

func defaultConfig() AppConfig {
	return AppConfig{
		Title:                 "Switch Dashboard",
		RefreshInterval:       3,
		MACRefreshMultiplier:  10,
		PortsWrapThreshold:    0,
		MaxRequestRetries:     3,
		EnabledColumns:        []string{"port", "status", "speed", "packets", "bytes", "info", "notes"},
		GridColumns:           "auto",
		Switches:              []SwitchConfig{},
		InfrastructureDevices: []InfraDevice{},
		UnmanagedSwitches:     []UnmanagedSwitch{},
		Clients:               make(map[string]ClientEntry),
		Notes:                 make(map[string]string),
		Settings: Settings{
			LogLevel:    "INFO",
			IgnoredMACs: []string{},
		},
		ColumnWidths: make(map[string]any),
		ColumnOrder:  []string{},
		MapPositions: make(map[string]map[string]float64),
		Auth: AuthConfig{
			Username: "admin",
		},
	}
}

func (s *Store) Load() {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg := defaultConfig()
	if data, err := os.ReadFile(s.ConfigPath); err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			s.logger.Error("config", "Error parsing config.json: %v", err)
		}
	}
	if cfg.Clients == nil {
		cfg.Clients = make(map[string]ClientEntry)
	}
	if cfg.Notes == nil {
		cfg.Notes = make(map[string]string)
	}
	if cfg.ColumnWidths == nil {
		cfg.ColumnWidths = make(map[string]any)
	}
	if cfg.MapPositions == nil {
		cfg.MapPositions = make(map[string]map[string]float64)
	}
	if cfg.RefreshInterval <= 0 {
		cfg.RefreshInterval = 3
	}
	if cfg.MACRefreshMultiplier <= 0 {
		cfg.MACRefreshMultiplier = 10
	}
	if len(cfg.EnabledColumns) == 0 {
		cfg.EnabledColumns = []string{"port", "status", "speed", "packets", "bytes", "info", "notes"}
	}
	if cfg.Auth.Username == "" {
		cfg.Auth.Username = "admin"
	}

	// Migrate legacy or empty title and legacy 15s default refresh interval
	needsSave := false
	if strings.TrimSpace(cfg.Title) == "" || strings.HasSuffix(strings.TrimSpace(cfg.Title), "Switch OS") {
		cfg.Title = "Switch Dashboard"
		needsSave = true
	}
	if cfg.RefreshInterval == 15 && cfg.MACRefreshMultiplier == 5 {
		cfg.RefreshInterval = 3
		cfg.MACRefreshMultiplier = 10
		needsSave = true
	}

	// Transparently encrypt any plaintext switch passwords at rest
	for i := range cfg.Switches {
		pw := cfg.Switches[i].Password
		if pw != "" && !s.Vault.IsEncrypted(pw) {
			if enc, err := s.Vault.Encrypt(pw); err == nil {
				cfg.Switches[i].Password = enc
				needsSave = true
			}
		}
	}
	s.cfg = cfg
	if needsSave {
		_ = s.saveLocked()
	}
}

func (s *Store) saveLocked() error {
	if strings.TrimSpace(s.cfg.Title) == "" || strings.HasSuffix(strings.TrimSpace(s.cfg.Title), "Switch OS") {
		s.cfg.Title = "Switch Dashboard"
	}
	for i := range s.cfg.Switches {
		pw := s.cfg.Switches[i].Password
		if pw != "" && !s.Vault.IsEncrypted(pw) {
			if enc, err := s.Vault.Encrypt(pw); err == nil {
				s.cfg.Switches[i].Password = enc
			}
		}
	}
	data, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := s.ConfigPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.ConfigPath)
}

func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

// GetPublicConfig returns a deep copy of AppConfig with all switch passwords stripped
// and replaced by HasPassword=true so secrets are never exposed to the browser.
func (s *Store) GetPublicConfig() AppConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	raw, _ := json.Marshal(s.cfg)
	var copyCfg AppConfig
	_ = json.Unmarshal(raw, &copyCfg)
	copyCfg.Auth.PasswordHash = ""
	if strings.TrimSpace(copyCfg.Title) == "" || strings.HasSuffix(strings.TrimSpace(copyCfg.Title), "Switch OS") {
		copyCfg.Title = "Switch Dashboard"
	}
	for i := range copyCfg.Switches {
		copyCfg.Switches[i].HasPassword = copyCfg.Switches[i].Password != ""
		copyCfg.Switches[i].Password = ""
	}
	return copyCfg
}

// GetDecryptedSwitches returns enabled/all switches with decrypted passwords for internal scraper use only.
func (s *Store) GetDecryptedSwitches() []SwitchConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]SwitchConfig, len(s.cfg.Switches))
	for i, sw := range s.cfg.Switches {
		dec, err := s.Vault.Decrypt(sw.Password)
		if err != nil {
			dec = ""
		}
		sw.Password = dec
		out[i] = sw
	}
	return out
}

func (s *Store) Update(fn func(cfg *AppConfig) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(&s.cfg); err != nil {
		return err
	}
	return s.saveLocked()
}

func (s *Store) Read(fn func(cfg *AppConfig)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fn(&s.cfg)
}

func NormalizeMAC(mac string) string {
	r := strings.NewReplacer(":", "", "-", "", " ", "", ".", "")
	return strings.ToUpper(strings.TrimSpace(r.Replace(mac)))
}

func FormatMAC(norm string) string {
	clean := NormalizeMAC(norm)
	if len(clean) != 12 {
		return strings.ToUpper(norm)
	}
	parts := make([]string, 6)
	for i := 0; i < 6; i++ {
		parts[i] = clean[i*2 : i*2+2]
	}
	return strings.Join(parts, ":")
}

func (s *Store) IsIgnoredMAC(mac string) bool {
	clean := NormalizeMAC(mac)
	if clean == "" {
		return false
	}
	s.mu.RLock()
	patterns := s.cfg.Settings.IgnoredMACs
	s.mu.RUnlock()

	for _, pat := range patterns {
		pClean := strings.ToUpper(strings.TrimSpace(pat))
		if pClean == "" {
			continue
		}
		if strings.HasSuffix(pClean, "*") {
			prefix := NormalizeMAC(strings.TrimSuffix(pClean, "*"))
			if prefix != "" && strings.HasPrefix(clean, prefix) {
				return true
			}
		} else {
			if clean == NormalizeMAC(pClean) {
				return true
			}
		}
	}
	return false
}

// SanitizeModelName prevents path traversal when resolving YAML templates or model images.
func SanitizeModelName(model string) (string, error) {
	m := strings.TrimSpace(model)
	if m == "" {
		return "", errors.New("empty model name")
	}
	if strings.Contains(m, "..") || strings.ContainsAny(m, `/\`) || !safeNameRegex.MatchString(m) {
		return "", errors.New("invalid characters in model name")
	}
	return m, nil
}

func SanitizeFilename(name string) (string, error) {
	n := strings.TrimSpace(name)
	if n == "" || n == "." || n == ".." {
		return "", errors.New("empty filename")
	}
	if filepath.Base(n) != n || strings.Contains(n, "..") || strings.ContainsAny(n, `/\`) || !safeNameRegex.MatchString(n) {
		return "", errors.New("invalid filename")
	}
	return n, nil
}

func (s *Store) LoadCustomVendors() map[string]string {
	vendors := make(map[string]string)
	data, err := os.ReadFile(s.VendorsTxtPath)
	if err != nil {
		return vendors
	}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			prefix := NormalizeMAC(fields[0])
			name := strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
			if len(prefix) == 6 || len(prefix) == 9 {
				vendors[prefix] = name
			}
		}
	}
	return vendors
}

func parseOUIFile(path string, out map[string]string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var currentOUI string
	for sc.Scan() {
		line := sc.Text()
		if strings.Contains(line, "(hex)") {
			parts := strings.SplitN(line, "(hex)", 2)
			currentOUI = NormalizeMAC(parts[0])
		} else if strings.Contains(line, "(base 16)") && currentOUI != "" {
			parts := strings.SplitN(line, "(base 16)", 2)
			rangeStr := strings.TrimSpace(parts[0])
			name := strings.TrimSpace(parts[1])
			if strings.Contains(rangeStr, "-") {
				rParts := strings.SplitN(rangeStr, "-", 2)
				rPrefix := strings.TrimSpace(rParts[0])
				if len(rPrefix) >= 3 {
					out[currentOUI+strings.ToUpper(rPrefix[:3])] = name
				}
			} else {
				out[currentOUI] = name
			}
		}
	}
}

func (s *Store) GetIEEEVendors() map[string]string {
	s.ouiMu.RLock()
	if s.ieeeVendors != nil {
		defer s.ouiMu.RUnlock()
		return s.ieeeVendors
	}
	s.ouiMu.RUnlock()

	s.ouiMu.Lock()
	defer s.ouiMu.Unlock()
	if s.ieeeVendors != nil {
		return s.ieeeVendors
	}
	db := make(map[string]string)
	parseOUIFile(s.OUI36TxtPath, db)
	parseOUIFile(s.OUITxtPath, db)
	s.ieeeVendors = db
	return db
}

func (s *Store) UpdateIEEEOUIFiles() error {
	client := &http.Client{Timeout: 20 * time.Second}
	urls := map[string]string{
		"https://standards-oui.ieee.org/oui36/oui36.txt": s.OUI36TxtPath,
		"https://standards-oui.ieee.org/oui/oui.txt":     s.OUITxtPath,
	}
	for urlStr, dest := range urls {
		req, err := http.NewRequest(http.MethodGet, urlStr, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "SwitchDashboard-Go/1.0")
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024*1024))
		resp.Body.Close()
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK || len(body) < 1000 {
			return fmt.Errorf("unexpected response from %s: HTTP %d", urlStr, resp.StatusCode)
		}
		if err := os.WriteFile(dest, body, 0640); err != nil {
			return err
		}
	}
	s.ouiMu.Lock()
	s.ieeeVendors = nil
	s.ouiMu.Unlock()
	return nil
}

func (s *Store) LookupVendor(mac string, customVendors, ieeeVendors map[string]string) string {
	clean := NormalizeMAC(mac)
	if len(clean) < 6 {
		return ""
	}
	if len(clean) >= 9 {
		p9 := clean[:9]
		if v, ok := customVendors[p9]; ok {
			return v
		}
		if v, ok := ieeeVendors[p9]; ok {
			return v
		}
	}
	p6 := clean[:6]
	if v, ok := customVendors[p6]; ok {
		return v
	}
	if v, ok := ieeeVendors[p6]; ok {
		return v
	}
	return ""
}
