package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"horaco-switch-dashboard/internal/auth"
	"horaco-switch-dashboard/internal/config"
	"horaco-switch-dashboard/internal/poller"
	"horaco-switch-dashboard/internal/scraper"
	"horaco-switch-dashboard/web"
)

type Server struct {
	store  *config.Store
	auth   *auth.Manager
	poller *poller.Engine
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func readStrictJSON(r *http.Request, dst any) error {
	ct := strings.ToLower(r.Header.Get("Content-Type"))
	if !strings.HasPrefix(ct, "application/json") {
		return errors.New("Content-Type must be application/json")
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 4*1024*1024))
	return dec.Decode(dst)
}

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; connect-src 'self'; font-src 'self' data:; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAuth(handler func(http.ResponseWriter, *http.Request, *auth.Session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess := s.auth.GetSession(r)
		if sess == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Authentication required"})
			return
		}
		if !auth.ValidateCSRFAndOrigin(r, sess) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "CSRF / Origin validation failed"})
			return
		}
		handler(w, r, sess)
	}
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	var setupRequired bool
	var title string
	s.store.Read(func(cfg *config.AppConfig) {
		setupRequired = cfg.Auth.PasswordHash == ""
		title = strings.TrimSpace(cfg.Title)
		if title == "" || strings.HasSuffix(title, "Switch OS") {
			title = "Switch Dashboard"
		}
	})
	sess := s.auth.GetSession(r)
	if sess == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"setup_required": setupRequired,
			"authenticated":  false,
			"version":        config.Version,
			"title":          title,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"setup_required": setupRequired,
		"authenticated":  true,
		"username":       sess.Username,
		"csrf_token":     sess.CSRFToken,
		"version":        config.Version,
		"title":          title,
	})
}

func (s *Server) handleAuthSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "Method not allowed"})
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readStrictJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	user := strings.TrimSpace(body.Username)
	if user == "" {
		user = "admin"
	}
	if len(body.Password) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Password must be at least 8 characters"})
		return
	}

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to hash password"})
		return
	}

	err = s.store.Update(func(cfg *config.AppConfig) error {
		if cfg.Auth.PasswordHash != "" {
			return errors.New("initial setup already completed")
		}
		cfg.Auth.Username = user
		cfg.Auth.PasswordHash = hash
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}

	sess, err := s.auth.CreateSession(user)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to create session"})
		return
	}
	auth.SetSessionCookie(w, r, sess)
	s.store.Logger().Info("auth", "Administrator account '%s' configured from initial setup", user)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"username":   sess.Username,
		"csrf_token": sess.CSRFToken,
	})
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "Method not allowed"})
		return
	}
	clientIP := auth.ClientIP(r)
	if s.auth.IsRateLimited(clientIP) {
		s.store.Logger().Warn("auth", "Rate limit triggered for login from %s", clientIP)
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "Too many failed login attempts. Try again in 1 minute."})
		return
	}

	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readStrictJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	var expectedUser, expectedHash string
	s.store.Read(func(cfg *config.AppConfig) {
		expectedUser = cfg.Auth.Username
		expectedHash = cfg.Auth.PasswordHash
	})
	if expectedHash == "" {
		writeJSON(w, http.StatusPreconditionRequired, map[string]any{"error": "Initial setup required"})
		return
	}

	userMatch := strings.EqualFold(strings.TrimSpace(body.Username), expectedUser)
	if !userMatch || !auth.VerifyPassword(expectedHash, body.Password) {
		s.auth.RecordFailure(clientIP)
		s.store.Logger().Warn("auth", "Failed login attempt for user '%s' from %s", body.Username, clientIP)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Invalid username or password"})
		return
	}

	s.auth.ResetFailures(clientIP)
	sess, err := s.auth.CreateSession(expectedUser)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Session creation error"})
		return
	}
	auth.SetSessionCookie(w, r, sess)
	s.store.Logger().Info("auth", "User '%s' logged in from %s", expectedUser, clientIP)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"username":   sess.Username,
		"csrf_token": sess.CSRFToken,
	})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "Method not allowed"})
		return
	}
	s.auth.DestroySession(sess.ID)
	auth.ClearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleAuthChangePassword(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "Method not allowed"})
		return
	}
	var body struct {
		Username        string `json:"username"`
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := readStrictJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if len(body.NewPassword) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "New password must be at least 8 characters"})
		return
	}

	var curHash string
	s.store.Read(func(cfg *config.AppConfig) {
		curHash = cfg.Auth.PasswordHash
	})
	if !auth.VerifyPassword(curHash, body.CurrentPassword) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Current password is incorrect"})
		return
	}

	newHash, err := auth.HashPassword(body.NewPassword)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to hash password"})
		return
	}

	newUser := strings.TrimSpace(body.Username)
	_ = s.store.Update(func(cfg *config.AppConfig) error {
		if newUser != "" {
			cfg.Auth.Username = newUser
		}
		cfg.Auth.PasswordHash = newHash
		return nil
	})
	s.store.Logger().Info("auth", "Administrator password updated by '%s'", sess.Username)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleSwitches(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	data := s.poller.GetSwitchesData()
	if data == nil {
		data = []scraper.SwitchData{}
	}
	writeJSON(w, http.StatusOK, data)
}

func (s *Server) handleSwitchAction(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	// Route pattern: /api/switches/{ip}/{action}
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/switches/")
	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 || parts[0] == "" {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Invalid switch endpoint"})
		return
	}
	ip := parts[0]
	action := parts[1]

	switch action {
	case "refresh_mac":
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST required"})
			return
		}
		tbl, err := s.poller.RefreshSwitchMAC(ip)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "count": len(tbl), "mac_table": tbl})

	case "transceiver":
		sw, maxRetries, err := s.findDecryptedSwitch(ip)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		}
		if strings.EqualFold(sw.Model, "internet") {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "No transceiver on virtual Internet node"})
			return
		}
		sc := scraper.NewHCSwitchScraper(*sw, s.store, maxRetries)
		info, err := sc.ScrapeTransceiver()
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, info)

	case "image":
		sw, _, err := s.findDecryptedSwitch(ip)
		if err == nil && sw.Model != "" {
			if safeModel, err := config.SanitizeModelName(sw.Model); err == nil {
				for _, ext := range []string{"png", "jpg", "jpeg"} {
					imgName := safeModel + "." + ext
					for _, dir := range []string{s.store.DeviceTemplatesDir, filepath.Join(s.store.BaseDir, "device-templates")} {
						p := filepath.Join(dir, imgName)
						if st, err := os.Stat(p); err == nil && !st.IsDir() {
							http.ServeFile(w, r, p)
							return
						}
					}
				}
			}
		}
		fallbackIcon := filepath.Join(s.store.BaseDir, "static", "switch_icon.png")
		http.ServeFile(w, r, fallbackIcon)

	case "backup":
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST required"})
			return
		}
		sw, maxRetries, err := s.findDecryptedSwitch(ip)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		}
		if strings.EqualFold(sw.Model, "internet") {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Virtual Internet node cannot be backed up"})
			return
		}
		sc := scraper.NewHCSwitchScraper(*sw, s.store, maxRetries)
		binData, err := sc.DownloadBackup()
		if err != nil || len(binData) == 0 {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to retrieve backup from switch"})
			return
		}
		ipDashed := strings.ReplaceAll(strings.ReplaceAll(ip, ".", "-"), ":", "-")
		ts := time.Now().Format("20060102_1504")
		filename, err := config.SanitizeFilename(fmt.Sprintf("switch_cfg_%s_%s.bin", ipDashed, ts))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid switch IP for backup filename"})
			return
		}
		fullPath := filepath.Join(s.store.BackupDir, filename)
		if err := os.WriteFile(fullPath, binData, 0640); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		s.store.Logger().Info("backup", "Saved switch configuration backup: %s (%d bytes)", filename, len(binData))
		writeJSON(w, http.StatusOK, map[string]any{
			"status":    "ok",
			"filename":  filename,
			"size":      len(binData),
			"timestamp": ts,
		})

	case "reboot":
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST required"})
			return
		}
		sw, maxRetries, err := s.findDecryptedSwitch(ip)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		}
		if strings.EqualFold(sw.Model, "internet") {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Virtual Internet node cannot be rebooted"})
			return
		}
		sc := scraper.NewHCSwitchScraper(*sw, s.store, maxRetries)
		if err := sc.RebootSwitch(); err != nil {
			s.store.Logger().Error("reboot", "Reboot failed for %s: %v", ip, err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		s.store.Logger().Warn("reboot", "Reboot command sent to switch %s (%s)", sw.Name, ip)
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "message": "Reboot command sent successfully"})

	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Unknown switch action"})
	}
}

func (s *Server) findDecryptedSwitch(ip string) (*config.SwitchConfig, int, error) {
	switches := s.store.GetDecryptedSwitches()
	var maxRetries int
	s.store.Read(func(cfg *config.AppConfig) {
		maxRetries = cfg.MaxRequestRetries
	})
	for i := range switches {
		if switches[i].IP == ip {
			return &switches[i], maxRetries, nil
		}
	}
	return nil, maxRetries, errors.New("switch not found")
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if r.Method == http.MethodGet {
		pub := s.store.GetPublicConfig()
		writeJSON(w, http.StatusOK, pub)
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "Method not allowed"})
		return
	}

	var incoming config.AppConfig
	if err := readStrictJSON(r, &incoming); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	err := s.store.Update(func(cfg *config.AppConfig) error {
		// Map existing encrypted passwords by IP so blank passwords in UI preserve existing secret
		existingPasswords := make(map[string]string)
		for _, sw := range cfg.Switches {
			existingPasswords[sw.IP] = sw.Password
		}

		for i := range incoming.Switches {
			sw := &incoming.Switches[i]
			sw.Name = strings.TrimSpace(sw.Name)
			sw.IP = strings.TrimSpace(sw.IP)
			sw.Username = strings.TrimSpace(sw.Username)
			sw.Model = strings.TrimSpace(sw.Model)
			if sw.Model != "" && !strings.EqualFold(sw.Model, "internet") && !strings.EqualFold(sw.Model, "Generic Model") {
				if _, err := config.SanitizeModelName(sw.Model); err != nil {
					return fmt.Errorf("invalid model name '%s'", sw.Model)
				}
			}
			if sw.PortCount <= 0 {
				sw.PortCount = 9
			}
			if strings.TrimSpace(sw.Password) == "" {
				sw.Password = existingPasswords[sw.IP]
			}
		}

		if incoming.Title != "" {
			cfg.Title = strings.TrimSpace(incoming.Title)
		}
		if incoming.RefreshInterval >= 1 && incoming.RefreshInterval <= 300 {
			cfg.RefreshInterval = incoming.RefreshInterval
		}
		if incoming.MACRefreshMultiplier >= 1 && incoming.MACRefreshMultiplier <= 100 {
			cfg.MACRefreshMultiplier = incoming.MACRefreshMultiplier
		}
		if incoming.PortsWrapThreshold >= 0 && incoming.PortsWrapThreshold <= 100 {
			cfg.PortsWrapThreshold = incoming.PortsWrapThreshold
		}
		if incoming.MaxRequestRetries >= 0 && incoming.MaxRequestRetries <= 20 {
			cfg.MaxRequestRetries = incoming.MaxRequestRetries
		}
		if len(incoming.EnabledColumns) > 0 {
			cfg.EnabledColumns = incoming.EnabledColumns
		}
		if incoming.GridColumns != "" {
			cfg.GridColumns = incoming.GridColumns
		}
		cfg.Switches = incoming.Switches
		cfg.InfrastructureDevices = incoming.InfrastructureDevices
		cfg.UnmanagedSwitches = incoming.UnmanagedSwitches
		cfg.Settings.IgnoredMACs = incoming.Settings.IgnoredMACs
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	s.poller.TriggerPoll()
	s.store.Logger().Info("config", "System configuration updated")
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "config": s.store.GetPublicConfig()})
}

func (s *Server) handleConfigSettings(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if r.Method == http.MethodGet {
		var res map[string]any
		s.store.Read(func(cfg *config.AppConfig) {
			res = map[string]any{
				"column_widths": cfg.ColumnWidths,
				"column_order":  cfg.ColumnOrder,
				"map_positions": cfg.MapPositions,
			}
		})
		writeJSON(w, http.StatusOK, res)
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "Method not allowed"})
		return
	}
	var body struct {
		ColumnWidths map[string]any                `json:"column_widths"`
		ColumnOrder  []string                      `json:"column_order"`
		MapPositions map[string]map[string]float64 `json:"map_positions"`
	}
	if err := readStrictJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	_ = s.store.Update(func(cfg *config.AppConfig) error {
		if body.ColumnWidths != nil {
			cfg.ColumnWidths = body.ColumnWidths
		}
		if body.ColumnOrder != nil {
			cfg.ColumnOrder = body.ColumnOrder
		}
		if body.MapPositions != nil {
			cfg.MapPositions = body.MapPositions
		}
		return nil
	})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleClients(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	customVendors := s.store.LoadCustomVendors()
	ieeeVendors := s.store.GetIEEEVendors()

	var list []config.ClientEntry
	s.store.Read(func(cfg *config.AppConfig) {
		for norm, c := range cfg.Clients {
			if s.store.IsIgnoredMAC(norm) {
				continue
			}
			if c.MAC == "" {
				c.MAC = config.FormatMAC(norm)
			}
			c.Vendor = s.store.LookupVendor(norm, customVendors, ieeeVendors)
			if c.DeviceType == "" {
				c.DeviceType = "laptop"
			}
			list = append(list, c)
		}
	})
	sort.Slice(list, func(i, j int) bool {
		if list[i].Status != list[j].Status {
			return list[i].Status == "online"
		}
		return list[i].MAC < list[j].MAC
	})
	if list == nil {
		list = []config.ClientEntry{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleClientUpdateHost(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST required"})
		return
	}
	var body struct {
		MAC  string `json:"mac"`
		Host string `json:"host"`
		Note string `json:"note"`
	}
	if err := readStrictJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	norm := config.NormalizeMAC(body.MAC)
	if norm == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Missing mac"})
		return
	}
	_ = s.store.Update(func(cfg *config.AppConfig) error {
		c, ok := cfg.Clients[norm]
		if !ok {
			c = config.ClientEntry{
				MAC:        config.FormatMAC(norm),
				DeviceType: "laptop",
				Status:     "offline",
			}
		}
		c.Host = strings.TrimSpace(body.Host)
		if body.Note != "" {
			c.Note = strings.TrimSpace(body.Note)
		}
		cfg.Clients[norm] = c
		return nil
	})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleClientUpdateType(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST required"})
		return
	}
	var body struct {
		MAC  string `json:"mac"`
		Type string `json:"type"`
	}
	if err := readStrictJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	norm := config.NormalizeMAC(body.MAC)
	if norm == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Missing mac"})
		return
	}
	_ = s.store.Update(func(cfg *config.AppConfig) error {
		c, ok := cfg.Clients[norm]
		if !ok {
			c = config.ClientEntry{
				MAC:    config.FormatMAC(norm),
				Status: "offline",
			}
		}
		c.DeviceType = strings.TrimSpace(body.Type)
		cfg.Clients[norm] = c
		return nil
	})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleClientDelete(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST required"})
		return
	}
	var body struct {
		MAC string `json:"mac"`
	}
	if err := readStrictJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	norm := config.NormalizeMAC(body.MAC)
	if norm == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Missing mac"})
		return
	}
	_ = s.store.Update(func(cfg *config.AppConfig) error {
		delete(cfg.Clients, norm)
		delete(cfg.MapPositions, norm)
		return nil
	})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleClientImportCSV(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST required"})
		return
	}
	if err := r.ParseMultipartForm(2 * 1024 * 1024); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid multipart form"})
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Missing CSV file"})
		return
	}
	defer file.Close()
	if !strings.HasSuffix(strings.ToLower(hdr.Filename), ".csv") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Only .csv files are allowed"})
		return
	}
	reader := csv.NewReader(io.LimitReader(file, 2*1024*1024))
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Failed to parse CSV"})
		return
	}
	imported := 0
	_ = s.store.Update(func(cfg *config.AppConfig) error {
		for _, row := range rows {
			if len(row) < 2 {
				continue
			}
			hostName := strings.TrimSpace(row[0])
			macRaw := strings.TrimSpace(row[1])
			if strings.Contains(strings.ToLower(hostName), "host") || strings.Contains(strings.ToLower(macRaw), "mac") {
				continue
			}
			norm := config.NormalizeMAC(macRaw)
			if len(norm) == 12 {
				c, ok := cfg.Clients[norm]
				if !ok {
					c = config.ClientEntry{
						MAC:        config.FormatMAC(norm),
						DeviceType: "laptop",
						Status:     "offline",
					}
				}
				c.Host = hostName
				cfg.Clients[norm] = c
				imported++
			}
		}
		return nil
	})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "imported": imported})
}

func (s *Server) handleNotes(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST required"})
		return
	}
	var body struct {
		Key  string `json:"key"`
		Note string `json:"note"`
	}
	if err := readStrictJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.Key) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Missing key"})
		return
	}
	_ = s.store.Update(func(cfg *config.AppConfig) error {
		note := strings.TrimSpace(body.Note)
		if note != "" {
			cfg.Notes[body.Key] = note
		} else {
			delete(cfg.Notes, body.Key)
		}
		return nil
	})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleDeviceTypes(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	parsed := make(map[string]any)
	if data, err := os.ReadFile(s.store.DeviceTypesPath); err == nil {
		_ = yaml.Unmarshal(data, &parsed)
	}
	writeJSON(w, http.StatusOK, parsed)
}

func (s *Server) handleDeviceTypesRaw(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if r.Method == http.MethodGet {
		data, _ := os.ReadFile(s.store.DeviceTypesPath)
		writeJSON(w, http.StatusOK, map[string]any{"content": string(data)})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "Method not allowed"})
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := readStrictJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	var parsed map[string]map[string]any
	if err := yaml.Unmarshal([]byte(body.Content), &parsed); err != nil || len(parsed) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid YAML dictionary syntax"})
		return
	}
	if err := os.WriteFile(s.store.DeviceTypesPath, []byte(body.Content), 0640); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleVendors(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if r.Method == http.MethodGet {
		data, err := os.ReadFile(s.store.VendorsTxtPath)
		if err != nil {
			defaultTxt := "# Custom MAC Vendor mappings (one per line)\n# Format: AA:BB:CC Vendor Name\n"
			_ = os.WriteFile(s.store.VendorsTxtPath, []byte(defaultTxt), 0640)
			data = []byte(defaultTxt)
		}
		writeJSON(w, http.StatusOK, map[string]any{"content": string(data)})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "Method not allowed"})
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := readStrictJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := os.WriteFile(s.store.VendorsTxtPath, []byte(body.Content), 0640); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleUpdateOUI(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST required"})
		return
	}
	if err := s.store.UpdateIEEEOUIFiles(); err != nil {
		s.store.Logger().Error("oui", "Manual OUI update failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	s.store.Logger().Info("oui", "IEEE OUI database updated manually")
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "message": "IEEE OUI databases successfully updated."})
}

func (s *Server) handleTemplatesList(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	var files []string
	if entries, err := os.ReadDir(s.store.DeviceTemplatesDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
				files = append(files, e.Name())
			}
		}
	}
	sort.Strings(files)
	if files == nil {
		files = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": files})
}

func (s *Server) handleTemplateItem(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	rawName := strings.TrimPrefix(r.URL.Path, "/api/templates/")
	safeName, err := config.SanitizeFilename(rawName)
	if err != nil || !strings.HasSuffix(safeName, ".yaml") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid template filename"})
		return
	}
	fullPath := filepath.Join(s.store.DeviceTemplatesDir, safeName)

	if r.Method == http.MethodGet {
		data, err := os.ReadFile(fullPath)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "Template not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"content": string(data)})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "Method not allowed"})
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := readStrictJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(body.Content), &parsed); err != nil || len(parsed) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("Invalid YAML syntax: %v", err)})
		return
	}
	if err := os.WriteFile(fullPath, []byte(body.Content), 0640); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	s.store.Logger().Info("templates", "Saved device template %s", safeName)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleBackupsList(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	var backups []map[string]any
	entries, err := os.ReadDir(s.store.BackupDir)
	if err == nil {
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() && strings.HasPrefix(name, "switch_cfg_") && strings.HasSuffix(name, ".bin") {
				info, err := e.Info()
				if err != nil {
					continue
				}
				size := info.Size()
				var sizeStr string
				switch {
				case size >= 1048576:
					sizeStr = fmt.Sprintf("%.1f MB", float64(size)/1048576.0)
				case size >= 1024:
					sizeStr = fmt.Sprintf("%.1f KB", float64(size)/1024.0)
				default:
					sizeStr = fmt.Sprintf("%d B", size)
				}
				parts := strings.Split(strings.TrimSuffix(name, ".bin"), "_")
				ip := "Unknown"
				dtStr := info.ModTime().Format("2006-01-02 15:04")
				if len(parts) >= 5 {
					ip = strings.ReplaceAll(parts[2], "-", ".")
					dp, tp := parts[3], parts[4]
					if len(dp) == 8 && len(tp) == 4 {
						dtStr = fmt.Sprintf("%s-%s-%s %s:%s", dp[0:4], dp[4:6], dp[6:8], tp[0:2], tp[2:4])
					}
				}
				backups = append(backups, map[string]any{
					"filename": name,
					"ip":       ip,
					"size":     size,
					"size_str": sizeStr,
					"datetime": dtStr,
					"mtime":    info.ModTime().Unix(),
				})
			}
		}
	}
	sort.Slice(backups, func(i, j int) bool {
		return backups[i]["mtime"].(int64) > backups[j]["mtime"].(int64)
	})
	if backups == nil {
		backups = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, backups)
}

func (s *Server) handleBackupItem(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/backups/")
	parts := strings.Split(trimmed, "/")
	safeName, err := config.SanitizeFilename(parts[0])
	if err != nil || !strings.HasSuffix(safeName, ".bin") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid backup filename"})
		return
	}
	fullPath := filepath.Join(s.store.BackupDir, safeName)
	st, err := os.Stat(fullPath)
	if err != nil || st.IsDir() {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Backup file not found"})
		return
	}

	if len(parts) == 2 && parts[1] == "download" && r.Method == http.MethodGet {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, safeName))
		http.ServeFile(w, r, fullPath)
		return
	}
	if len(parts) == 1 && r.Method == http.MethodDelete {
		if err := os.Remove(fullPath); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		s.store.Logger().Info("backup", "Deleted backup file %s", safeName)
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
		return
	}
	writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "Method not allowed"})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	writeJSON(w, http.StatusOK, map[string]any{
		"level": s.store.Logger().GetLevel(),
		"lines": s.store.Logger().Recent(200),
	})
}

func (s *Server) handleLogsLevel(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST required"})
		return
	}
	var body struct {
		Level string `json:"level"`
	}
	if err := readStrictJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	norm := s.store.Logger().SetLevel(body.Level)
	_ = s.store.Update(func(cfg *config.AppConfig) error {
		cfg.Settings.LogLevel = norm
		return nil
	})
	s.store.Logger().Warn("logs", "Log level changed to %s", norm)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "level": norm})
}

func (s *Server) handleLogsClear(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST required"})
		return
	}
	if err := s.store.Logger().Clear(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	s.store.Logger().Warn("logs", "Log history cleared by administrator")
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleLogsDownload(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	w.Header().Set("Content-Disposition", `attachment; filename="dashboard.log"`)
	http.ServeFile(w, r, s.store.LogFilePath)
}

func main() {
	baseDir, err := os.Getwd()
	if err != nil {
		baseDir = "."
	}
	dataDir := os.Getenv("DASHBOARD_DATA_DIR")
	if dataDir == "" {
		dataDir = baseDir
	}

	store, err := config.NewStore(baseDir, dataDir)
	if err != nil {
		panic(fmt.Sprintf("Failed to initialize config store: %v", err))
	}

	authMgr := auth.NewManager()
	engine := poller.NewEngine(store)
	engine.Start()

	srv := &Server{
		store:  store,
		auth:   authMgr,
		poller: engine,
	}

	mux := http.NewServeMux()

	// Auth endpoints
	mux.HandleFunc("/api/auth/status", srv.handleAuthStatus)
	mux.HandleFunc("/api/auth/setup", srv.handleAuthSetup)
	mux.HandleFunc("/api/auth/login", srv.handleAuthLogin)
	mux.HandleFunc("/api/auth/logout", srv.requireAuth(srv.handleAuthLogout))
	mux.HandleFunc("/api/auth/password", srv.requireAuth(srv.handleAuthChangePassword))

	// Switch & Telemetry endpoints
	mux.HandleFunc("/api/switches", srv.requireAuth(srv.handleSwitches))
	mux.HandleFunc("/api/switches/", srv.requireAuth(srv.handleSwitchAction))
	mux.HandleFunc("/api/topology", srv.requireAuth(func(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
		writeJSON(w, http.StatusOK, srv.poller.BuildTopology())
	}))
	mux.HandleFunc("/api/speeds", srv.requireAuth(func(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
		writeJSON(w, http.StatusOK, srv.poller.GetSpeeds())
	}))
	mux.HandleFunc("/api/history", srv.requireAuth(func(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
		ip := r.URL.Query().Get("ip")
		port := r.URL.Query().Get("port")
		rng := r.URL.Query().Get("range")
		if ip == "" || port == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Missing ip or port"})
			return
		}
		tx, rx, ts := srv.poller.GetHistory(ip, port, rng)
		writeJSON(w, http.StatusOK, map[string]any{"tx": tx, "rx": rx, "timestamps": ts})
	}))
	mux.HandleFunc("/api/reset", srv.requireAuth(func(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST required"})
			return
		}
		srv.poller.ResetHistoryAndCounters()
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	}))

	// Config, Clients, Templates, Backups, Logs
	mux.HandleFunc("/api/config", srv.requireAuth(srv.handleConfig))
	mux.HandleFunc("/api/config/settings", srv.requireAuth(srv.handleConfigSettings))
	mux.HandleFunc("/api/notes", srv.requireAuth(srv.handleNotes))
	mux.HandleFunc("/api/clients", srv.requireAuth(srv.handleClients))
	mux.HandleFunc("/api/clients/update_host", srv.requireAuth(srv.handleClientUpdateHost))
	mux.HandleFunc("/api/clients/update_type", srv.requireAuth(srv.handleClientUpdateType))
	mux.HandleFunc("/api/clients/delete", srv.requireAuth(srv.handleClientDelete))
	mux.HandleFunc("/api/clients/import_csv", srv.requireAuth(srv.handleClientImportCSV))
	mux.HandleFunc("/api/device_types", srv.requireAuth(srv.handleDeviceTypes))
	mux.HandleFunc("/api/device_types/raw", srv.requireAuth(srv.handleDeviceTypesRaw))
	mux.HandleFunc("/api/vendors", srv.requireAuth(srv.handleVendors))
	mux.HandleFunc("/api/vendors/update_oui", srv.requireAuth(srv.handleUpdateOUI))
	mux.HandleFunc("/api/templates", srv.requireAuth(srv.handleTemplatesList))
	mux.HandleFunc("/api/templates/", srv.requireAuth(srv.handleTemplateItem))
	mux.HandleFunc("/api/backups", srv.requireAuth(srv.handleBackupsList))
	mux.HandleFunc("/api/backups/", srv.requireAuth(srv.handleBackupItem))
	mux.HandleFunc("/api/logs", srv.requireAuth(srv.handleLogs))
	mux.HandleFunc("/api/logs/level", srv.requireAuth(srv.handleLogsLevel))
	mux.HandleFunc("/api/logs/clear", srv.requireAuth(srv.handleLogsClear))
	mux.HandleFunc("/api/logs/download", srv.requireAuth(srv.handleLogsDownload))

	// Static assets (/static/logo.png, /static/switch_icon.png)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(filepath.Join(baseDir, "static")))))

	// Embedded SvelteKit SPA fallback
	distFS, err := fs.Sub(web.Assets, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(distFS))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "API endpoint not found"})
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if f, err := distFS.Open(path); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// SPA fallback to index.html for client-side SvelteKit routes
		if indexBytes, err := fs.ReadFile(distFS, "index.html"); err == nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(indexBytes)
			return
		}
		http.NotFound(w, r)
	})

	host := os.Getenv("DASHBOARD_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("DASHBOARD_PORT")
	if port == "" {
		port = "8080"
	}
	addr := host + ":" + port
	store.Logger().Info("server", "Switch Dashboard Backend listening on http://%s (DataDir: %s)", addr, dataDir)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           securityHeadersMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	if err := httpServer.ListenAndServe(); err != nil {
		panic(err)
	}
}
