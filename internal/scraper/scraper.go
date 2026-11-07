package scraper

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
	"gopkg.in/yaml.v3"

	"horaco-switch-dashboard/internal/config"
)

var (
	locksMu     sync.Mutex
	switchLocks = make(map[string]*sync.Mutex)

	reUptime   = regexp.MustCompile(`(?:(\d+)Day)?(?:(\d+)Hour)?(?:(\d+)Minute)?(?:(\d+)Second)?`)
	rePortNum  = regexp.MustCompile(`(?i)Port\s*(\d+)`)
	reDigits   = regexp.MustCompile(`\d+`)
	reSpeed    = regexp.MustCompile(`(?i)^(\d+G?)(Full|Half)?`)
	reUnifySpd = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*([mMgGtT]?)[bB]?[pP]?[sS]?$`)
)

func getSwitchLock(ip string) *sync.Mutex {
	locksMu.Lock()
	defer locksMu.Unlock()
	if l, ok := switchLocks[ip]; ok {
		return l
	}
	l := &sync.Mutex{}
	switchLocks[ip] = l
	return l
}

type PortData struct {
	Port         string `json:"port"`
	Status       string `json:"status"`
	Link         string `json:"link"`
	Speed        string `json:"speed"`
	Duplex       string `json:"duplex"`
	FlowControl  string `json:"flow_control"`
	TxPackets    uint64 `json:"tx_packets"`
	RxPackets    uint64 `json:"rx_packets"`
	TxBytes      uint64 `json:"tx_bytes"`
	RxBytes      uint64 `json:"rx_bytes"`
	CumTx        uint64 `json:"cum_tx"`
	CumRx        uint64 `json:"cum_rx"`
	SpeedTxBps   int64  `json:"speed_tx_bps"`
	SpeedRxBps   int64  `json:"speed_rx_bps"`
	Note         string `json:"note,omitempty"`
}

type MACEntry struct {
	MAC    string `json:"mac"`
	Type   string `json:"type"`
	Port   string `json:"port"`
	VLAN   string `json:"vlan"`
	Vendor string `json:"vendor,omitempty"`
	Host   string `json:"host,omitempty"`
}

type DHCPSnooping struct {
	Enabled bool              `json:"enabled"`
	Ports   map[string]string `json:"ports"`
}

type IGMPEntry struct {
	VLAN  string `json:"vlan"`
	IP    string `json:"ip"`
	Ports string `json:"ports"`
}

type IGMPInfo struct {
	Enabled bool        `json:"enabled"`
	Entries []IGMPEntry `json:"entries"`
}

type JumboFrameInfo struct {
	Enabled bool   `json:"enabled"`
	Size    string `json:"size"`
}

type SwitchData struct {
	Name         string         `json:"name"`
	IP           string         `json:"ip"`
	Model        string         `json:"model"`
	MAC          string         `json:"mac"`
	Uptime       string         `json:"uptime"`
	Firmware     string         `json:"firmware"`
	Status       string         `json:"status,omitempty"`
	Error        string         `json:"error,omitempty"`
	Ports        []PortData     `json:"ports"`
	MACTable     []MACEntry     `json:"mac_table"`
	MACTimestamp float64        `json:"mac_timestamp"`
	DHCPSnooping DHCPSnooping   `json:"dhcp_snooping"`
	IGMP         IGMPInfo       `json:"igmp"`
	JumboFrame   JumboFrameInfo `json:"jumbo_frame"`
	Timestamp    float64        `json:"timestamp"`
}

// YAML Template definitions matching existing device-templates/*.yaml
type DeviceTemplate struct {
	Model        string `yaml:"model"`
	Manufacturer string `yaml:"manufacturer"`
	Format       string `yaml:"format"`
	Login        any    `yaml:"login"`
	DeviceInfo   struct {
		URL      string            `yaml:"url"`
		Method   string            `yaml:"method"`
		Format   string            `yaml:"format"`
		Mappings map[string]string `yaml:"mappings"`
	} `yaml:"device_info"`
	Ports struct {
		URL            string            `yaml:"url"`
		Source         string            `yaml:"source"`
		Format         string            `yaml:"format"`
		ListKey        string            `yaml:"list_key"`
		PortTableMatch string            `yaml:"port_table_match"`
		Columns        map[string]int    `yaml:"columns"`
		Mapping        map[string]string `yaml:"mapping"`
	} `yaml:"ports"`
	Statistics struct {
		URL    string              `yaml:"url"`
		Method string              `yaml:"method"`
		Terms  map[string][]string `yaml:"terms"`
	} `yaml:"statistics"`
	DHCPSnooping struct {
		URL                string `yaml:"url"`
		EnableInputName    string `yaml:"enable_input_name"`
		PortsFormAction    string `yaml:"ports_form_action"`
		TrustCheckboxClass string `yaml:"trust_checkbox_class"`
	} `yaml:"dhcp_snooping"`
	IGMP struct {
		URL                 string   `yaml:"url"`
		EnableInputName     string   `yaml:"enable_input_name"`
		TableHeaderKeywords []string `yaml:"table_header_keywords"`
	} `yaml:"igmp"`
	JumboFrame struct {
		URL             string `yaml:"url"`
		Format          string `yaml:"format"`
		EnableInputName string `yaml:"enable_input_name"`
		SelectName      string `yaml:"select_name"`
	} `yaml:"jumbo_frame"`
	MACTable struct {
		URL              string            `yaml:"url"`
		Format           string            `yaml:"format"`
		PageParameter    string            `yaml:"page_parameter"`
		PerPageParameter string            `yaml:"perpage_parameter"`
		PerPageValue     string            `yaml:"perpage_value"`
		CmdParameter     string            `yaml:"cmd_parameter"`
		CmdValue         string            `yaml:"cmd_value"`
		Mapping          map[string]string `yaml:"mapping"`
	} `yaml:"mac_table"`
	Backup struct {
		URL         string            `yaml:"url"`
		RefererPath string            `yaml:"referer_path"`
		Method      string            `yaml:"method"`
		PostData    map[string]string `yaml:"post_data"`
	} `yaml:"backup"`
	Reboot struct {
		URL         string            `yaml:"url"`
		RefererPath string            `yaml:"referer_path"`
		Method      string            `yaml:"method"`
		PostData    map[string]string `yaml:"post_data"`
	} `yaml:"reboot"`
}

type HCSwitchScraper struct {
	Name       string
	IP         string
	Username   string
	Password   string
	PortCount  int
	Model      string
	BaseURL    string
	MaxRetries int
	Store      *config.Store

	client  *http.Client
	jar     *cookiejar.Jar
	cookies map[string]string
}

func NewHCSwitchScraper(sw config.SwitchConfig, store *config.Store, maxRetries int) *HCSwitchScraper {
	pc := sw.PortCount
	if pc <= 0 {
		pc = 9
	}
	user := sw.Username
	if user == "" {
		user = "admin"
	}
	model := sw.Model
	if model == "" {
		model = "Generic Model"
	}
	if maxRetries < 1 {
		maxRetries = 1
	}
	return &HCSwitchScraper{
		Name:       sw.Name,
		IP:         sw.IP,
		Username:   user,
		Password:   sw.Password,
		PortCount:  pc,
		Model:      model,
		BaseURL:    "http://" + sw.IP,
		MaxRetries: maxRetries,
		Store:      store,
		cookies:    make(map[string]string),
	}
}

func UnifySpeed(speedVal string) string {
	s := strings.TrimSpace(speedVal)
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	if lower == "auto" || lower == "disabled" || lower == "disable" || lower == "down" || lower == "unknown" {
		return s
	}
	m := reUnifySpd.FindStringSubmatch(s)
	if len(m) == 3 {
		val, err := strconv.ParseFloat(m[1], 64)
		if err == nil {
			unit := strings.ToUpper(m[2])
			if unit == "M" || unit == "" {
				if val >= 1000 {
					vg := val / 1000.0
					if vg == math.Trunc(vg) {
						return fmt.Sprintf("%dG", int(vg))
					}
					return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", vg), "0"), ".") + "G"
				}
				if val == math.Trunc(val) {
					return fmt.Sprintf("%dM", int(val))
				}
				return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", val), "0"), ".") + "M"
			} else if unit == "G" {
				if val == math.Trunc(val) {
					return fmt.Sprintf("%dG", int(val))
				}
				return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", val), "0"), ".") + "G"
			}
		}
	}
	return s
}

func (s *HCSwitchScraper) initHTTPClient() {
	jar, _ := cookiejar.New(nil)
	s.jar = jar
	targetHost := strings.Split(s.IP, ":")[0]
	s.client = &http.Client{
		Jar:     jar,
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// SSRF defense: block redirects to any host other than the switch IP
			if !strings.EqualFold(req.URL.Hostname(), targetHost) {
				return errors.New("cross-host redirect blocked for security")
			}
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}

func (s *HCSwitchScraper) loadTemplate() *DeviceTemplate {
	if s.Store == nil {
		return nil
	}
	safeModel, err := config.SanitizeModelName(s.Model)
	if err != nil || safeModel == "" {
		return nil
	}
	tplName := safeModel + ".yaml"
	tplPath := filepath.Join(s.Store.DeviceTemplatesDir, tplName)
	if _, err := os.Stat(tplPath); err != nil {
		tplPath = filepath.Join(s.Store.BaseDir, "device-templates", tplName)
	}
	data, err := os.ReadFile(tplPath)
	if err != nil {
		return nil
	}
	var tpl DeviceTemplate
	if err := yaml.Unmarshal(data, &tpl); err != nil {
		s.Store.Logger().Error("scraper", "Error parsing YAML template %s: %v", tplPath, err)
		return nil
	}
	return &tpl
}

func (s *HCSwitchScraper) doRequestWithRetry(method, fullURL string, headers map[string]string, postBody []byte) ([]byte, string, error) {
	// Spacing between sequential uIP HTTP requests
	time.Sleep(350 * time.Millisecond)

	attempts := s.MaxRetries
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		var bodyReader io.Reader
		if len(postBody) > 0 {
			bodyReader = bytes.NewReader(postBody)
		}
		req, err := http.NewRequest(method, fullURL, bodyReader)
		if err != nil {
			return nil, "", err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		for k, v := range s.cookies {
			req.AddCookie(&http.Cookie{Name: k, Value: v})
		}

		resp, err := s.client.Do(req)
		if err == nil {
			body, rErr := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
			resp.Body.Close()
			if resp.StatusCode == http.StatusNotFound {
				return nil, "", fmt.Errorf("HTTP 404 Not Found for %s", fullURL)
			}
			if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
				s.client = nil
				return nil, "", fmt.Errorf("HTTP %d for %s", resp.StatusCode, fullURL)
			}
			if rErr == nil && resp.StatusCode < 400 {
				finalURL := fullURL
				if resp.Request != nil && resp.Request.URL != nil {
					finalURL = resp.Request.URL.String()
				}
				return body, finalURL, nil
			}
			if rErr != nil {
				lastErr = rErr
			} else {
				lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			}
		} else {
			lastErr = err
		}

		// Raw socket fallback for uIP embedded switch servers
		rawResp, rawErr := doRawSocketRequest(fullURL, method, headers, s.cookies, postBody, "HTTP/1.1", 10*time.Second)
		if rawErr == nil && len(rawResp.Body) > 0 {
			for k, v := range rawResp.Cookies {
				s.cookies[k] = v
			}
			return rawResp.Body, rawResp.FinalURL, nil
		}

		if attempt < attempts {
			time.Sleep(1500 * time.Millisecond)
		}
	}
	return nil, "", lastErr
}

func md5Hex(input string) string {
	sum := md5.Sum([]byte(input))
	return hex.EncodeToString(sum[:])
}

func (s *HCSwitchScraper) login() error {
	s.initHTTPClient()
	s.cookies = make(map[string]string)

	tpl := s.loadTemplate()
	loginRequired := true
	var loginMap map[string]any
	if tpl != nil && tpl.Login != nil {
		switch v := tpl.Login.(type) {
		case bool:
			loginRequired = v
		case map[string]any:
			loginMap = v
			if reqVal, ok := v["required"].(bool); ok {
				loginRequired = reqVal
			}
		}
	}

	if !loginRequired {
		return nil
	}

	hashStr := md5Hex(s.Username + s.Password)

	if loginMap != nil {
		if loginURL, ok := loginMap["url"].(string); ok && loginURL != "" {
			method := "POST"
			if m, ok := loginMap["method"].(string); ok && m != "" {
				method = strings.ToUpper(m)
			}
			refererPath := "/login.html"
			if rp, ok := loginMap["referer_path"].(string); ok && rp != "" {
				refererPath = rp
			}
			vals := url.Values{}
			if pd, ok := loginMap["post_data"].(map[string]any); ok {
				for k, rawV := range pd {
					strV := fmt.Sprintf("%v", rawV)
					strV = strings.ReplaceAll(strV, "{{username}}", s.Username)
					strV = strings.ReplaceAll(strV, "{{password}}", s.Password)
					strV = strings.ReplaceAll(strV, "{{md5hash}}", hashStr)
					vals.Set(k, strV)
				}
			}
			headers := map[string]string{
				"Referer": s.BaseURL + refererPath,
			}
			var body []byte
			if method == "POST" {
				body = []byte(vals.Encode())
				headers["Content-Type"] = "application/x-www-form-urlencoded"
			}
			_, finalURL, err := s.doRequestWithRetry(method, s.BaseURL+loginURL, headers, body)
			if err == nil && !strings.Contains(finalURL, "login.html") {
				return nil
			}
			// Try HTTP/1.0 raw socket login fallback (for RTLPlayground / custom uIP)
			rawResp, rawErr := doRawSocketRequest(s.BaseURL+loginURL, method, headers, s.cookies, body, "HTTP/1.0", 6*time.Second)
			if rawErr == nil && len(rawResp.Cookies) > 0 {
				for k, v := range rawResp.Cookies {
					s.cookies[k] = v
				}
				return nil
			}
			if err != nil {
				return err
			}
			return errors.New("custom template login failed")
		}
	}

	// Default Horaco / KeepLink login (/login.cgi)
	vals := url.Values{}
	vals.Set("username", s.Username)
	vals.Set("password", s.Password)
	vals.Set("Response", hashStr)
	vals.Set("language", "EN")

	headers := map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
	}
	_, _, err := s.doRequestWithRetry(http.MethodPost, s.BaseURL+"/login.cgi", headers, []byte(vals.Encode()))
	if err != nil {
		return err
	}

	s.cookies["admin"] = hashStr
	if u, err := url.Parse(s.BaseURL); err == nil && s.jar != nil {
		s.jar.SetCookies(u, []*http.Cookie{{Name: "admin", Value: hashStr, Path: "/"}})
	}

	_, _, _ = s.doRequestWithRetry(http.MethodGet, s.BaseURL+"/", nil, nil)
	return nil
}

func (s *HCSwitchScraper) fetch(path string) (string, error) {
	if s.client == nil {
		if err := s.login(); err != nil {
			return "", err
		}
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	headers := map[string]string{
		"Referer": s.BaseURL + "/",
	}
	body, _, err := s.doRequestWithRetry(http.MethodGet, s.BaseURL+path, headers, nil)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func fmtUptime(raw string) string {
	m := reUptime.FindStringSubmatch(raw)
	if len(m) == 5 {
		units := []string{"d", "h", "m", "s"}
		var parts []string
		for i := 1; i <= 4; i++ {
			if m[i] != "" {
				parts = append(parts, m[i]+units[i-1])
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, " ")
		}
	}
	return raw
}

func parseCounter(val string) uint64 {
	val = strings.TrimSpace(val)
	if val == "" {
		return 0
	}
	if strings.HasPrefix(strings.ToLower(val), "0x") {
		v, err := strconv.ParseUint(val[2:], 16, 64)
		if err == nil {
			return v
		}
		return 0
	}
	parts := strings.Split(val, "-")
	if len(parts) == 2 {
		hi, err1 := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 64)
		lo, err2 := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
		if err1 == nil && err2 == nil {
			return hi*4294967296 + lo
		}
		return 0
	}
	v, err := strconv.ParseUint(val, 10, 64)
	if err == nil {
		return v
	}
	return 0
}

func (s *HCSwitchScraper) fallbackPorts() []PortData {
	ports := make([]PortData, s.PortCount)
	for i := 1; i <= s.PortCount; i++ {
		ports[i-1] = PortData{
			Port:   strconv.Itoa(i),
			Status: "unknown",
			Link:   "Unknown",
		}
	}
	return ports
}

func (s *HCSwitchScraper) fallbackData(errMsg string) SwitchData {
	return SwitchData{
		Name:         s.Name,
		IP:           s.IP,
		Model:        s.Model,
		Status:       "offline",
		Ports:        s.fallbackPorts(),
		MACTable:     []MACEntry{},
		DHCPSnooping: DHCPSnooping{Enabled: false, Ports: map[string]string{}},
		IGMP:         IGMPInfo{Enabled: false, Entries: []IGMPEntry{}},
		JumboFrame:   JumboFrameInfo{Enabled: false, Size: "Disabled"},
		Error:        errMsg,
		Timestamp:    float64(time.Now().Unix()),
	}
}

func (s *HCSwitchScraper) Scrape() SwitchData {
	lock := getSwitchLock(s.IP)
	lock.Lock()
	defer lock.Unlock()

	if tpl := s.loadTemplate(); tpl != nil {
		data, err := s.scrapeWithTemplate(tpl)
		if err == nil {
			for i := range data.Ports {
				data.Ports[i].Speed = UnifySpeed(data.Ports[i].Speed)
			}
			return data
		}
		if s.Store != nil {
			s.Store.Logger().Warn("scraper", "Template scrape failed for %s (%v), trying built-in scraper", s.IP, err)
		}
	}

	data, err := s.scrapeBuiltin()
	if err != nil {
		return s.fallbackData(err.Error())
	}
	for i := range data.Ports {
		data.Ports[i].Speed = UnifySpeed(data.Ports[i].Speed)
	}
	return data
}

// HTML parsing helpers using golang.org/x/net/html
type htmlCell struct {
	Tag   string
	Text  string
	Attrs map[string]string
}

type htmlTable struct {
	RawText string
	Rows    [][]htmlCell
}

func parseHTMLDoc(rawHTML string) *html.Node {
	doc, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return nil
	}
	return doc
}

func nodeText(n *html.Node) string {
	var buf strings.Builder
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if cur.Type == html.TextNode {
			buf.WriteString(cur.Data)
		}
		for c := cur.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(buf.String())
}

func nodeAttrs(n *html.Node) map[string]string {
	m := make(map[string]string, len(n.Attr))
	for _, a := range n.Attr {
		m[strings.ToLower(a.Key)] = a.Val
	}
	return m
}

func extractTables(doc *html.Node) []htmlTable {
	var tables []htmlTable
	if doc == nil {
		return tables
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "table" {
			var t htmlTable
			t.RawText = nodeText(n)
			var rowWalk func(*html.Node)
			rowWalk = func(rn *html.Node) {
				if rn.Type == html.ElementNode && rn.Data == "tr" {
					var row []htmlCell
					for cn := rn.FirstChild; cn != nil; cn = cn.NextSibling {
						if cn.Type == html.ElementNode && (cn.Data == "td" || cn.Data == "th") {
							row = append(row, htmlCell{
								Tag:   cn.Data,
								Text:  nodeText(cn),
								Attrs: nodeAttrs(cn),
							})
						}
					}
					if len(row) > 0 {
						t.Rows = append(t.Rows, row)
					}
					return
				}
				for c := rn.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.ElementNode && c.Data == "table" {
						continue
					}
					rowWalk(c)
				}
			}
			rowWalk(n)
			tables = append(tables, t)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return tables
}

func findElements(doc *html.Node, tag string) []*html.Node {
	var out []*html.Node
	if doc == nil {
		return out
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (tag == "" || n.Data == tag) {
			out = append(out, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out
}

func (s *HCSwitchScraper) scrapeBuiltin() (SwitchData, error) {
	if err := s.login(); err != nil {
		return SwitchData{}, err
	}

	infoHTML, err := s.fetch("/info.cgi")
	if err != nil {
		return SwitchData{}, err
	}
	statsHTML, _ := s.fetch("/port.cgi?page=stats")
	portCfgHTML, _ := s.fetch("/port.cgi")

	deviceInfo := make(map[string]string)
	portStates := make(map[string]string)
	var ports []PortData

	// Parse admin states from /port.cgi if available
	if portCfgHTML != "" {
		pDoc := parseHTMLDoc(portCfgHTML)
		pTables := extractTables(pDoc)
		for _, t := range pTables {
			if len(t.Rows) > 1 {
				for _, row := range t.Rows {
					if len(row) >= 2 {
						pName := row[0].Text
						state := strings.ToLower(strings.TrimSpace(row[1].Text))
						if m := reDigits.FindString(pName); m != "" && (state == "enable" || state == "disable" || state == "enabled" || state == "disabled") {
							portStates[m] = state
						}
					}
				}
			}
		}
	}

	if infoHTML != "" {
		doc := parseHTMLDoc(infoHTML)
		tables := extractTables(doc)
		if len(tables) >= 1 {
			for _, row := range tables[0].Rows {
				for i := 0; i+1 < len(row); i += 2 {
					label := strings.TrimSuffix(row[i].Text, ":")
					val := row[i+1].Text
					switch {
					case strings.Contains(label, "Sys Uptime"):
						deviceInfo["uptime"] = fmtUptime(val)
					case strings.Contains(label, "MAC Address"):
						deviceInfo["mac"] = val
					case strings.Contains(label, "IP Address"):
						deviceInfo["ip"] = val
					case strings.Contains(label, "Firmware Version"):
						deviceInfo["firmware"] = val
					case strings.Contains(label, "Device Model"):
						deviceInfo["model"] = val
					}
				}
			}
		}
		if len(tables) >= 2 && len(tables[1].Rows) > 1 {
			for _, row := range tables[1].Rows[1:] {
				if len(row) >= 4 {
					pName := row[0].Text
					pNum := pName
					if m := rePortNum.FindStringSubmatch(pName); len(m) == 2 {
						pNum = m[1]
					}
					adminState := portStates[pNum]
					var statusVal, linkVal string
					if adminState == "disable" || adminState == "disabled" {
						statusVal = "disable"
						linkVal = "Disabled"
					} else {
						linkVal = row[1].Text
						if strings.Contains(linkVal, "Up") {
							statusVal = "up"
						} else {
							statusVal = "down"
						}
					}
					fc := ""
					if len(row) > 4 {
						fc = row[4].Text
					}
					ports = append(ports, PortData{
						Port:        pNum,
						Status:      statusVal,
						Link:        linkVal,
						Duplex:      row[2].Text,
						Speed:       row[3].Text,
						FlowControl: fc,
					})
				}
			}
		}
	}

	// KeepLink fallback: port status table inside /port.cgi
	if len(ports) == 0 && portCfgHTML != "" {
		ports = parseKeepLinkPortTable(portCfgHTML, "Actual", map[string]int{
			"port": 0, "admin_state": 1, "actual_link": 3, "flow_control": 5,
		})
	}

	// Parse statistics from /port.cgi?page=stats
	if statsHTML != "" {
		applyStatsHTML(statsHTML, ports, nil)
	}

	dhcp := s.scrapeDHCPSnooping("/dhcp_snooping.cgi?page=dump", "enable_dhcpsnp", "page=static", "chkp")
	igmp := s.scrapeIGMP("/igmp.cgi?page=dump", "enable_igmp", []string{"IP Address", "Port", "VLAN ID"})
	jumbo := s.scrapeJumboFrame("/fwd.cgi?page=jumboframe", "enable_jumbo", "jumboframe")

	if len(ports) == 0 {
		ports = s.fallbackPorts()
	}
	model := s.Model
	if m := deviceInfo["model"]; m != "" {
		model = m
	}

	return SwitchData{
		Name:         s.Name,
		IP:           s.IP,
		Model:        model,
		MAC:          deviceInfo["mac"],
		Uptime:       deviceInfo["uptime"],
		Firmware:     deviceInfo["firmware"],
		Status:       "online",
		Ports:        ports,
		MACTable:     []MACEntry{},
		DHCPSnooping: dhcp,
		IGMP:         igmp,
		JumboFrame:   jumbo,
		Timestamp:    float64(time.Now().Unix()),
	}, nil
}

func parseKeepLinkPortTable(rawHTML, matchKeyword string, cols map[string]int) []PortData {
	var ports []PortData
	doc := parseHTMLDoc(rawHTML)
	tables := extractTables(doc)
	if matchKeyword == "" {
		matchKeyword = "Actual"
	}

	for _, t := range tables {
		if len(t.Rows) >= 3 && strings.Contains(t.RawText, matchKeyword) && strings.Contains(t.RawText, "Port 1") {
			startIdx := -1
			for idx, r := range t.Rows {
				if len(r) > 0 && strings.Contains(r[0].Text, "Port 1") {
					startIdx = idx
					break
				}
			}
			if startIdx >= 0 {
				portIdx := getCol(cols, "port", 0)
				adminIdx := getCol(cols, "admin_state", 1)
				linkIdx := getCol(cols, "actual_link", 3)
				flowIdx := getCol(cols, "flow_control", 5)

				for _, row := range t.Rows[startIdx:] {
					if len(row) > maxInt(portIdx, adminIdx, linkIdx) {
						pName := row[portIdx].Text
						pNum := pName
						if m := rePortNum.FindStringSubmatch(pName); len(m) == 2 {
							pNum = m[1]
						}
						adminState := strings.ToLower(strings.TrimSpace(row[adminIdx].Text))
						actualLink := strings.TrimSpace(row[linkIdx].Text)

						var statusVal, linkVal, speedVal, duplexVal string
						if adminState == "disable" || adminState == "disabled" {
							statusVal = "disable"
							linkVal = "Disabled"
							speedVal = "Disabled"
							duplexVal = "Disabled"
						} else {
							if strings.Contains(strings.ToLower(actualLink), "down") {
								statusVal = "down"
								linkVal = "Link Down"
								speedVal = "Auto"
								duplexVal = ""
							} else {
								statusVal = "up"
								linkVal = "Link Up"
								speedVal = "Auto"
								duplexVal = "Full"
								if sm := reSpeed.FindStringSubmatch(actualLink); len(sm) >= 2 {
									sNum := strings.ToUpper(sm[1])
									if strings.Contains(sNum, "G") {
										speedVal = sNum
									} else {
										speedVal = sNum + "M"
									}
									if len(sm) >= 3 && sm[2] != "" {
										if strings.EqualFold(sm[2], "half") {
											duplexVal = "Half"
										} else {
											duplexVal = "Full"
										}
									}
								} else {
									speedVal = actualLink
								}
							}
						}
						fc := ""
						if len(row) > flowIdx {
							fc = row[flowIdx].Text
						}
						ports = append(ports, PortData{
							Port:        pNum,
							Status:      statusVal,
							Link:        linkVal,
							Speed:       speedVal,
							Duplex:      duplexVal,
							FlowControl: fc,
						})
					}
				}
			}
			break
		}
	}
	return ports
}

func applyStatsHTML(statsHTML string, ports []PortData, customTerms map[string][]string) {
	doc := parseHTMLDoc(statsHTML)
	tables := extractTables(doc)
	if len(tables) == 0 || len(tables[0].Rows) == 0 {
		return
	}
	rows := tables[0].Rows
	firstRow := rows[0]

	txPktTerms := []string{"txgoodpkt", "txpackets", "tx packet", "txok", "tx_pkt", "tx pkt"}
	rxPktTerms := []string{"rxgoodpkt", "rxpackets", "rx packet", "rxok", "rx_pkt", "rx pkt"}
	txByteTerms := []string{"txbytes", "tx byte", "tx_octet", "tx octet", "txgoodbytes", "tx_bytes", "tx good bytes"}
	rxByteTerms := []string{"rxbytes", "rx byte", "rx_octet", "rx octet", "rxgoodbytes", "rx_bytes", "rx good bytes"}

	if customTerms != nil {
		if t, ok := customTerms["tx_packets"]; ok && len(t) > 0 {
			txPktTerms = t
		}
		if t, ok := customTerms["rx_packets"]; ok && len(t) > 0 {
			rxPktTerms = t
		}
		if t, ok := customTerms["tx_bytes"]; ok && len(t) > 0 {
			txByteTerms = t
		}
		if t, ok := customTerms["rx_bytes"]; ok && len(t) > 0 {
			rxByteTerms = t
		}
	}

	txPktIdx, rxPktIdx, txBytesIdx, rxBytesIdx := -1, -1, -1, -1
	hasRxGoodPkt := false
	for idx, cell := range firstRow {
		h := strings.ToLower(strings.TrimSpace(cell.Text))
		if h == "rxgoodpkt" {
			hasRxGoodPkt = true
		}
		isBytes := strings.Contains(h, "byte") || strings.Contains(h, "octet")
		if !isBytes && (containsAny(h, txPktTerms) || h == "tx") {
			txPktIdx = idx
		} else if !isBytes && (containsAny(h, rxPktTerms) || h == "rx") {
			rxPktIdx = idx
		} else if containsAny(h, txByteTerms) {
			txBytesIdx = idx
		} else if containsAny(h, rxByteTerms) {
			rxBytesIdx = idx
		}
	}
	if txPktIdx == -1 {
		txPktIdx = 3
	}
	if rxPktIdx == -1 {
		if hasRxGoodPkt {
			rxPktIdx = 5
		} else {
			rxPktIdx = 4
		}
	}

	for _, row := range rows[1:] {
		if len(row) > maxInt(txPktIdx, rxPktIdx) {
			pName := strings.TrimPrefix(row[0].Text, "Port ")
			for i := range ports {
				if ports[i].Port == pName {
					txPkts := parseCounter(row[txPktIdx].Text)
					rxPkts := parseCounter(row[rxPktIdx].Text)
					ports[i].TxPackets = txPkts
					ports[i].RxPackets = rxPkts

					if txBytesIdx != -1 && len(row) > txBytesIdx {
						ports[i].TxBytes = parseCounter(row[txBytesIdx].Text)
					} else {
						ports[i].TxBytes = txPkts * 800
					}
					if rxBytesIdx != -1 && len(row) > rxBytesIdx {
						ports[i].RxBytes = parseCounter(row[rxBytesIdx].Text)
					} else {
						ports[i].RxBytes = rxPkts * 800
					}
					break
				}
			}
		}
	}
}

func (s *HCSwitchScraper) scrapeDHCPSnooping(urlPath, enableInputName, formAction, chkClass string) DHCPSnooping {
	res := DHCPSnooping{Enabled: false, Ports: make(map[string]string)}
	rawHTML, err := s.fetch(urlPath)
	if err != nil || rawHTML == "" {
		return res
	}
	doc := parseHTMLDoc(rawHTML)
	inputs := findElements(doc, "input")
	labels := findElements(doc, "label")
	labelMap := make(map[string]string)
	for _, lbl := range labels {
		attrs := nodeAttrs(lbl)
		if forID := attrs["for"]; forID != "" {
			labelMap[forID] = nodeText(lbl)
		}
	}

	for _, inp := range inputs {
		attrs := nodeAttrs(inp)
		if attrs["name"] == enableInputName {
			if _, ok := attrs["checked"]; ok {
				res.Enabled = true
			}
		}
		if chkClass != "" && strings.Contains(attrs["class"], chkClass) {
			if id := attrs["id"]; id != "" {
				if lblText, ok := labelMap[id]; ok {
					pName := strings.TrimPrefix(lblText, "Port ")
					if _, checked := attrs["checked"]; checked {
						res.Ports[pName] = "Trusted"
					} else {
						res.Ports[pName] = "Untrusted"
					}
				}
			}
		}
	}
	return res
}

func (s *HCSwitchScraper) scrapeIGMP(urlPath, enableInputName string, keywords []string) IGMPInfo {
	res := IGMPInfo{Enabled: false, Entries: []IGMPEntry{}}
	rawHTML, err := s.fetch(urlPath)
	if err != nil || rawHTML == "" {
		return res
	}
	doc := parseHTMLDoc(rawHTML)
	for _, inp := range findElements(doc, "input") {
		attrs := nodeAttrs(inp)
		if attrs["name"] == enableInputName {
			if _, ok := attrs["checked"]; ok {
				res.Enabled = true
			}
		}
	}
	tables := extractTables(doc)
	for _, t := range tables {
		matched := true
		for _, kw := range keywords {
			if !strings.Contains(t.RawText, kw) {
				matched = false
				break
			}
		}
		if matched && len(t.Rows) > 1 {
			for _, row := range t.Rows[1:] {
				if len(row) >= 3 {
					res.Entries = append(res.Entries, IGMPEntry{
						IP:    row[0].Text,
						Ports: row[1].Text,
						VLAN:  row[2].Text,
					})
				}
			}
			break
		}
	}
	return res
}

func (s *HCSwitchScraper) scrapeJumboFrame(urlPath, enableInputName, selectName string) JumboFrameInfo {
	res := JumboFrameInfo{Enabled: false, Size: "Disabled"}
	rawHTML, err := s.fetch(urlPath)
	if err != nil || rawHTML == "" {
		return res
	}
	doc := parseHTMLDoc(rawHTML)
	foundEnableInput := false
	for _, inp := range findElements(doc, "input") {
		attrs := nodeAttrs(inp)
		if attrs["name"] == enableInputName {
			foundEnableInput = true
			if _, ok := attrs["checked"]; ok {
				res.Enabled = true
			}
		}
	}
	if !foundEnableInput {
		res.Enabled = true
	}

	for _, sel := range findElements(doc, "select") {
		attrs := nodeAttrs(sel)
		if attrs["name"] == selectName {
			var firstText, selectedText string
			for c := sel.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && c.Data == "option" {
					txt := directNodeText(c)
					if firstText == "" {
						firstText = txt
					}
					optAttrs := nodeAttrs(c)
					if _, ok := optAttrs["selected"]; ok {
						selectedText = txt
						break
					}
				}
			}
			if selectedText != "" {
				res.Size = selectedText
			} else if firstText != "" {
				res.Size = firstText
			}
			break
		}
	}
	return res
}

func directNodeText(n *html.Node) string {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			t := strings.TrimSpace(c.Data)
			if t != "" {
				return t
			}
		}
	}
	return nodeText(n)
}

func (s *HCSwitchScraper) scrapeWithTemplate(tpl *DeviceTemplate) (SwitchData, error) {
	if err := s.login(); err != nil {
		return SwitchData{}, err
	}

	deviceInfo := make(map[string]string)
	var ports []PortData

	// 1. Device Info
	if tpl.DeviceInfo.URL != "" {
		raw, err := s.fetch(tpl.DeviceInfo.URL)
		if err == nil && raw != "" {
			if tpl.DeviceInfo.Format == "json" || tpl.Format == "json" {
				var data map[string]any
				if json.Unmarshal([]byte(raw), &data) == nil {
					for k, jsonKey := range tpl.DeviceInfo.Mappings {
						if v, ok := data[jsonKey]; ok {
							deviceInfo[k] = fmt.Sprintf("%v", v)
						}
					}
				}
			} else {
				doc := parseHTMLDoc(raw)
				tables := extractTables(doc)
				if len(tables) > 0 && tpl.DeviceInfo.Method == "key_value_grid" {
					for _, row := range tables[0].Rows {
						for i := 0; i+1 < len(row); i += 2 {
							label := strings.TrimSuffix(row[i].Text, ":")
							val := row[i+1].Text
							for key, term := range tpl.DeviceInfo.Mappings {
								if strings.Contains(strings.ToLower(label), strings.ToLower(term)) {
									if key == "uptime" {
										deviceInfo["uptime"] = fmtUptime(val)
									} else {
										deviceInfo[key] = val
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// 2. Ports
	if tpl.Ports.URL != "" {
		raw, err := s.fetch(tpl.Ports.URL)
		if err == nil && raw != "" {
			if tpl.Ports.Format == "json" || tpl.Format == "json" {
				var rawAny any
				if json.Unmarshal([]byte(raw), &rawAny) == nil {
					var portList []any
					if arr, ok := rawAny.([]any); ok {
						portList = arr
					} else if obj, ok := rawAny.(map[string]any); ok && tpl.Ports.ListKey != "" {
						if arr, ok := obj[tpl.Ports.ListKey].([]any); ok {
							portList = arr
						}
					}
					mapping := tpl.Ports.Mapping
					for _, item := range portList {
						pObj, ok := item.(map[string]any)
						if !ok {
							continue
						}
						portNum := mapStr(pObj, getMapKey(mapping, "port", "portNum"), "")
						adminVal := pObj[getMapKey(mapping, "admin_state", "enabled")]
						adminDisabled := adminVal == float64(0) || adminVal == false || adminVal == "0"

						var statusVal, linkVal, speedVal, duplexVal string
						if adminDisabled {
							statusVal = "disable"
							linkVal = "Disabled"
							speedVal = "Disabled"
							duplexVal = "Disabled"
						} else {
							linkRaw := pObj[getMapKey(mapping, "link", "link")]
							if numLink, ok := linkRaw.(float64); ok {
								code := int(numLink)
								if code == 0 {
									statusVal = "down"
									linkVal = "Link Down"
									speedVal = "Auto"
									duplexVal = ""
								} else {
									statusVal = "up"
									linkVal = "Link Up"
									linkMap := map[int][2]string{
										1: {"100M", "Half"},
										2: {"100M", "Full"},
										3: {"1000M", "Half"},
										4: {"1000M", "Full"},
										5: {"2.5G", "Full"},
										6: {"5G", "Full"},
										7: {"10G", "Full"},
									}
									if sd, ok := linkMap[code]; ok {
										speedVal, duplexVal = sd[0], sd[1]
									} else {
										speedVal, duplexVal = "Auto", "Full"
									}
								}
							} else {
								linkStr := fmt.Sprintf("%v", linkRaw)
								if strings.Contains(strings.ToLower(linkStr), "up") {
									statusVal = "up"
								} else {
									statusVal = "down"
								}
								linkVal = linkStr
								speedVal = mapStr(pObj, getMapKey(mapping, "speed", "speed"), "Auto")
								duplexVal = mapStr(pObj, getMapKey(mapping, "duplex", "duplex"), "Full")
							}
						}

						ports = append(ports, PortData{
							Port:        portNum,
							Status:      statusVal,
							Link:        linkVal,
							Speed:       speedVal,
							Duplex:      duplexVal,
							FlowControl: mapStr(pObj, getMapKey(mapping, "flow_control", "flow"), ""),
							TxPackets:   parseCounter(mapStr(pObj, getMapKey(mapping, "tx_packets", "txG"), "0")),
							RxPackets:   parseCounter(mapStr(pObj, getMapKey(mapping, "rx_packets", "rxG"), "0")),
							TxBytes:     parseCounter(mapStr(pObj, getMapKey(mapping, "tx_bytes", "txB"), "0")),
							RxBytes:     parseCounter(mapStr(pObj, getMapKey(mapping, "rx_bytes", "rxB"), "0")),
						})
					}
				}
			} else if tpl.Ports.Source == "port_table" {
				ports = parseKeepLinkPortTable(raw, tpl.Ports.PortTableMatch, tpl.Ports.Columns)
			} else {
				doc := parseHTMLDoc(raw)
				tables := extractTables(doc)
				if len(tables) >= 2 && len(tables[1].Rows) > 1 {
					cols := tpl.Ports.Columns
					portIdx := getCol(cols, "port", 0)
					linkIdx := getCol(cols, "link", 1)
					duplexIdx := getCol(cols, "duplex", 2)
					speedIdx := getCol(cols, "speed", 3)
					flowIdx := getCol(cols, "flow_control", 4)

					for _, row := range tables[1].Rows[1:] {
						if len(row) > maxInt(portIdx, linkIdx, duplexIdx, speedIdx) {
							pName := row[portIdx].Text
							pNum := pName
							if m := rePortNum.FindStringSubmatch(pName); len(m) == 2 {
								pNum = m[1]
							}
							linkVal := row[linkIdx].Text
							statusVal := "down"
							if strings.Contains(linkVal, "Up") {
								statusVal = "up"
							}
							fc := ""
							if len(row) > flowIdx {
								fc = row[flowIdx].Text
							}
							ports = append(ports, PortData{
								Port:        pNum,
								Status:      statusVal,
								Link:        linkVal,
								Duplex:      row[duplexIdx].Text,
								Speed:       row[speedIdx].Text,
								FlowControl: fc,
							})
						}
					}
				}
			}
		}
	}

	// 3. Statistics
	hasStats := false
	for _, p := range ports {
		if p.TxPackets > 0 || p.RxPackets > 0 {
			hasStats = true
			break
		}
	}
	if tpl.Statistics.URL != "" && !hasStats {
		if statsHTML, err := s.fetch(tpl.Statistics.URL); err == nil && statsHTML != "" {
			applyStatsHTML(statsHTML, ports, tpl.Statistics.Terms)
		}
	}

	// 4. DHCP Snooping
	dhcp := DHCPSnooping{Enabled: false, Ports: map[string]string{}}
	if tpl.DHCPSnooping.URL != "" {
		dhcp = s.scrapeDHCPSnooping(
			tpl.DHCPSnooping.URL,
			defaultStr(tpl.DHCPSnooping.EnableInputName, "enable_dhcpsnp"),
			defaultStr(tpl.DHCPSnooping.PortsFormAction, "page=static"),
			defaultStr(tpl.DHCPSnooping.TrustCheckboxClass, "chkp"),
		)
	}

	// 5. IGMP
	igmp := IGMPInfo{Enabled: false, Entries: []IGMPEntry{}}
	if tpl.IGMP.URL != "" {
		kws := tpl.IGMP.TableHeaderKeywords
		if len(kws) == 0 {
			kws = []string{"IP Address", "Port", "VLAN ID"}
		}
		igmp = s.scrapeIGMP(tpl.IGMP.URL, defaultStr(tpl.IGMP.EnableInputName, "enable_igmp"), kws)
	}

	// 6. Jumbo Frame
	jumbo := JumboFrameInfo{Enabled: false, Size: "Disabled"}
	if tpl.JumboFrame.URL != "" {
		if tpl.JumboFrame.Format == "json" || tpl.Format == "json" {
			if raw, err := s.fetch(tpl.JumboFrame.URL); err == nil && raw != "" {
				var arr []map[string]any
				if json.Unmarshal([]byte(raw), &arr) == nil {
					maxMTU := uint64(1500)
					for _, obj := range arr {
						v := parseCounter(mapStr(obj, "mtu", "1500"))
						if v > maxMTU {
							maxMTU = v
						}
					}
					if maxMTU > 1522 {
						jumbo = JumboFrameInfo{Enabled: true, Size: fmt.Sprintf("%dBytes", maxMTU)}
					}
				}
			}
		} else {
			jumbo = s.scrapeJumboFrame(
				tpl.JumboFrame.URL,
				defaultStr(tpl.JumboFrame.EnableInputName, "enable_jumbo"),
				defaultStr(tpl.JumboFrame.SelectName, "jumboframe"),
			)
		}
	}

	if len(ports) == 0 {
		ports = s.fallbackPorts()
	}
	model := s.Model
	if m := deviceInfo["model"]; m != "" {
		model = m
	}

	return SwitchData{
		Name:         s.Name,
		IP:           s.IP,
		Model:        model,
		MAC:          deviceInfo["mac"],
		Uptime:       deviceInfo["uptime"],
		Firmware:     deviceInfo["firmware"],
		Status:       "online",
		Ports:        ports,
		MACTable:     []MACEntry{},
		DHCPSnooping: dhcp,
		IGMP:         igmp,
		JumboFrame:   jumbo,
		Timestamp:    float64(time.Now().Unix()),
	}, nil
}

func (s *HCSwitchScraper) ScrapeMACTable() ([]MACEntry, error) {
	lock := getSwitchLock(s.IP)
	lock.Lock()
	defer lock.Unlock()

	if tpl := s.loadTemplate(); tpl != nil && tpl.MACTable.URL != "" {
		entries, err := s.scrapeMACWithTemplate(tpl)
		if err == nil && len(entries) > 0 {
			return entries, nil
		}
	}

	if err := s.login(); err != nil {
		return nil, err
	}

	rawHTML, err := s.fetch("/mac.cgi?page=fwd_tbl")
	if err != nil || rawHTML == "" {
		return nil, err
	}
	doc := parseHTMLDoc(rawHTML)
	entries := parseMACTableRows(doc)

	totalPages := extractTotalPages(doc)
	for page := 2; page <= totalPages; page++ {
		pageHTML, err := s.fetchMACPage("/mac.cgi?page=fwd_tbl", "cmd", "goto", "pageidx", strconv.Itoa(page), "perpage", "3")
		if err == nil && pageHTML != "" {
			entries = append(entries, parseMACTableRows(parseHTMLDoc(pageHTML))...)
		}
	}
	return deduplicateMACs(entries), nil
}

func (s *HCSwitchScraper) scrapeMACWithTemplate(tpl *DeviceTemplate) ([]MACEntry, error) {
	if err := s.login(); err != nil {
		return nil, err
	}
	macCfg := tpl.MACTable
	urlPath := defaultStr(macCfg.URL, "/mac.cgi?page=fwd_tbl")
	formatType := defaultStr(macCfg.Format, "html")

	fetchURL := urlPath
	if formatType == "json" && macCfg.PageParameter != "" && !strings.Contains(urlPath, "?") {
		fetchURL = fmt.Sprintf("%s?%s=0", urlPath, macCfg.PageParameter)
	}

	raw, err := s.fetch(fetchURL)
	if err != nil || raw == "" {
		return nil, err
	}

	var entries []MACEntry
	if formatType == "json" {
		var data []map[string]any
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			return nil, err
		}
		entries = append(entries, parseMACJSONEntries(data, macCfg.Mapping)...)
		pageParam := macCfg.PageParameter
		perPageVal, _ := strconv.Atoi(defaultStr(macCfg.PerPageValue, "3"))
		if perPageVal <= 0 {
			perPageVal = 3
		}
		seen := make(map[string]bool)
		for pageParam != "" && len(data) >= perPageVal {
			lastIdxRaw := mapStr(data[len(data)-1], pageParam, "")
			if lastIdxRaw == "" {
				break
			}
			val := parseCounter(lastIdxRaw)
			if val == 0 && len(lastIdxRaw) == 4 {
				if vHex, err := strconv.ParseUint(lastIdxRaw, 16, 64); err == nil {
					val = vHex
				}
			}
			queryIdx := strconv.FormatUint(val+1, 10)
			if seen[queryIdx] {
				break
			}
			seen[queryIdx] = true

			nextRaw, err := s.fetch(fmt.Sprintf("%s?%s=%s", urlPath, pageParam, queryIdx))
			if err != nil || nextRaw == "" {
				break
			}
			data = nil
			if json.Unmarshal([]byte(nextRaw), &data) != nil || len(data) == 0 {
				break
			}
			pageEntries := parseMACJSONEntries(data, macCfg.Mapping)
			if len(pageEntries) == 0 {
				break
			}
			entries = append(entries, pageEntries...)
		}
	} else {
		doc := parseHTMLDoc(raw)
		entries = append(entries, parseMACTableRows(doc)...)
		if macCfg.PageParameter != "" {
			totalPages := extractTotalPages(doc)
			for page := 2; page <= totalPages; page++ {
				pageHTML, err := s.fetchMACPage(
					urlPath,
					macCfg.CmdParameter,
					defaultStr(macCfg.CmdValue, "goto"),
					macCfg.PageParameter,
					strconv.Itoa(page),
					macCfg.PerPageParameter,
					defaultStr(macCfg.PerPageValue, "3"),
				)
				if err == nil && pageHTML != "" {
					entries = append(entries, parseMACTableRows(parseHTMLDoc(pageHTML))...)
				}
			}
		}
	}
	return deduplicateMACs(entries), nil
}

func (s *HCSwitchScraper) fetchMACPage(urlPath, cmdParam, cmdVal, pageParam, pageVal, perPageParam, perPageVal string) (string, error) {
	vals := url.Values{}
	if cmdParam != "" {
		vals.Set(cmdParam, cmdVal)
	}
	if pageParam != "" {
		vals.Set(pageParam, pageVal)
	}
	if perPageParam != "" {
		vals.Set(perPageParam, perPageVal)
	}
	headers := map[string]string{
		"Referer":      s.BaseURL + "/",
		"Content-Type": "application/x-www-form-urlencoded",
	}
	body, _, err := s.doRequestWithRetry(http.MethodPost, s.BaseURL+urlPath, headers, []byte(vals.Encode()))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func extractTotalPages(doc *html.Node) int {
	for _, n := range findElements(doc, "") {
		attrs := nodeAttrs(n)
		if attrs["id"] == "totalpage" {
			if tp, err := strconv.Atoi(strings.TrimSpace(nodeText(n))); err == nil && tp > 1 && tp <= 100 {
				return tp
			}
		}
	}
	return 1
}

func parseMACJSONEntries(data []map[string]any, mapping map[string]string) []MACEntry {
	var out []MACEntry
	macKey := getMapKey(mapping, "mac", "mac")
	portKey := getMapKey(mapping, "port", "port")
	typeKey := getMapKey(mapping, "type", "type")
	vlanKey := getMapKey(mapping, "vlan", "vlan")

	for _, item := range data {
		mac := strings.ToUpper(mapStr(item, macKey, ""))
		port := mapStr(item, portKey, "")
		mType := strings.ToLower(mapStr(item, typeKey, "dynamic"))
		vlan := mapStr(item, vlanKey, "1")
		if mac != "" {
			tVal := "dynamic"
			if strings.Contains(mType, "static") || mType == "s" {
				tVal = "static"
			}
			if !strings.HasPrefix(strings.ToLower(port), "port") {
				port = "Port " + port
			}
			out = append(out, MACEntry{
				MAC:  config.FormatMAC(mac),
				Type: tVal,
				Port: port,
				VLAN: vlan,
			})
		}
	}
	return out
}

func parseMACTableRows(doc *html.Node) []MACEntry {
	var out []MACEntry
	tables := extractTables(doc)
	for _, t := range tables {
		if len(t.Rows) < 2 {
			continue
		}
		firstRow := t.Rows[0]
		macIdx, typeIdx, portIdx, vlanIdx := -1, -1, -1, -1
		for idx, c := range firstRow {
			h := strings.ToLower(strings.TrimSpace(c.Text))
			switch {
			case strings.Contains(h, "mac address") || h == "mac":
				macIdx = idx
			case strings.Contains(h, "type"):
				typeIdx = idx
			case strings.Contains(h, "port"):
				portIdx = idx
			case strings.Contains(h, "vlan"):
				vlanIdx = idx
			}
		}
		if macIdx != -1 && portIdx != -1 {
			for _, row := range t.Rows[1:] {
				if len(row) > maxInt(macIdx, portIdx, typeIdx, vlanIdx) {
					mac := strings.TrimSpace(row[macIdx].Text)
					port := strings.TrimSpace(row[portIdx].Text)
					mType := "dynamic"
					if typeIdx != -1 {
						mType = strings.TrimSpace(row[typeIdx].Text)
					}
					vlan := "1"
					if vlanIdx != -1 {
						vlan = strings.TrimSpace(row[vlanIdx].Text)
					}
					if strings.Contains(mac, ":") && len(mac) >= 12 {
						out = append(out, MACEntry{
							MAC:  strings.ToUpper(mac),
							Type: mType,
							Port: port,
							VLAN: vlan,
						})
					}
				}
			}
			break
		}
	}
	return out
}

func deduplicateMACs(entries []MACEntry) []MACEntry {
	seen := make(map[string]bool, len(entries))
	var out []MACEntry
	for _, e := range entries {
		k := strings.ToUpper(e.MAC)
		if !seen[k] {
			seen[k] = true
			out = append(out, e)
		}
	}
	return out
}

func (s *HCSwitchScraper) ScrapeTransceiver() (map[string]any, error) {
	lock := getSwitchLock(s.IP)
	lock.Lock()
	defer lock.Unlock()

	if err := s.login(); err != nil {
		return nil, err
	}
	rawHTML, err := s.fetch("/transceiver.cgi")
	if err != nil || rawHTML == "" {
		return nil, errors.New("no transceiver data available")
	}

	cleaned := strings.ReplaceAll(strings.ReplaceAll(rawHTML, "<th", "<td"), "</th>", "</td>")
	doc := parseHTMLDoc(cleaned)
	tables := extractTables(doc)
	if len(tables) == 0 {
		return nil, errors.New("no transceiver table found")
	}

	info := make(map[string]any)
	for _, row := range tables[0].Rows {
		if len(row) >= 2 {
			k := strings.TrimSuffix(row[0].Text, ":")
			v := row[1].Text
			if id, ok := row[1].Attrs["id"]; ok && id != "" {
				info[id+"_raw"] = v
			} else {
				normKey := strings.ToLower(strings.NewReplacer(" ", "_", "(", "", ")", "", "/", "_").Replace(k))
				info[normKey] = v
			}
		}
	}

	decodeDDMI := func(rawStr string, mult float64, isPower bool) any {
		parts := strings.Split(rawStr, "-")
		if len(parts) == 2 {
			p0, e0 := strconv.Atoi(strings.TrimSpace(parts[0]))
			p1, e1 := strconv.Atoi(strings.TrimSpace(parts[1]))
			if e0 == nil && e1 == nil {
				decoded := float64(p0*256+p1) * mult
				if isPower {
					if decoded <= 0 {
						return "-inf dBm"
					}
					dbm := 10 * math.Log10(decoded)
					return fmt.Sprintf("%.2f dBm", dbm)
				}
				return decoded
			}
		}
		return rawStr
	}

	if raw, ok := info["temp_raw"].(string); ok {
		if v, ok := decodeDDMI(raw, 0.00391, false).(float64); ok {
			info["temperature"] = fmt.Sprintf("%.2f °C", v)
		} else {
			info["temperature"] = raw
		}
	}
	if raw, ok := info["voltage_raw"].(string); ok {
		if v, ok := decodeDDMI(raw, 0.0001, false).(float64); ok {
			info["voltage"] = fmt.Sprintf("%.2f V", v)
		} else {
			info["voltage"] = raw
		}
	}
	if raw, ok := info["current_raw"].(string); ok {
		if v, ok := decodeDDMI(raw, 0.002, false).(float64); ok {
			info["current"] = fmt.Sprintf("%.2f mA", v)
		} else {
			info["current"] = raw
		}
	}
	if raw, ok := info["txpower_raw"].(string); ok {
		info["tx_power"] = decodeDDMI(raw, 0.0001, true)
	}
	if raw, ok := info["rxpower_raw"].(string); ok {
		info["rx_power"] = decodeDDMI(raw, 0.0001, true)
	}

	if len(info) == 0 {
		return nil, errors.New("SFP module not inserted or no DDMI telemetry")
	}
	return info, nil
}

func (s *HCSwitchScraper) DownloadBackup() ([]byte, error) {
	lock := getSwitchLock(s.IP)
	lock.Lock()
	defer lock.Unlock()

	if err := s.login(); err != nil {
		return nil, err
	}

	urlPath := "/config_back.cgi?cmd=conf_backup"
	refererPath := "/config_back.cgi"
	method := http.MethodGet
	var postBody []byte

	if tpl := s.loadTemplate(); tpl != nil && tpl.Backup.URL != "" {
		urlPath = tpl.Backup.URL
		refererPath = defaultStr(tpl.Backup.RefererPath, "/config_back.cgi")
		method = strings.ToUpper(defaultStr(tpl.Backup.Method, "GET"))
		if method == http.MethodPost && len(tpl.Backup.PostData) > 0 {
			vals := url.Values{}
			for k, v := range tpl.Backup.PostData {
				vals.Set(k, v)
			}
			postBody = []byte(vals.Encode())
		}
	}

	headers := map[string]string{
		"Referer": s.BaseURL + refererPath,
	}
	if len(postBody) > 0 {
		headers["Content-Type"] = "application/x-www-form-urlencoded"
	}
	body, _, err := s.doRequestWithRetry(method, s.BaseURL+urlPath, headers, postBody)
	return body, err
}

func (s *HCSwitchScraper) RebootSwitch() error {
	lock := getSwitchLock(s.IP)
	lock.Lock()
	defer lock.Unlock()

	if err := s.login(); err != nil {
		return err
	}

	urlPath := "/reboot.cgi"
	refererPath := "/reboot.cgi"
	method := http.MethodPost
	vals := url.Values{"cmd": []string{"reboot"}}

	if tpl := s.loadTemplate(); tpl != nil && tpl.Reboot.URL != "" {
		urlPath = tpl.Reboot.URL
		refererPath = defaultStr(tpl.Reboot.RefererPath, "/reboot.cgi")
		method = strings.ToUpper(defaultStr(tpl.Reboot.Method, "POST"))
		if len(tpl.Reboot.PostData) > 0 {
			vals = url.Values{}
			for k, v := range tpl.Reboot.PostData {
				vals.Set(k, v)
			}
		}
	}

	headers := map[string]string{
		"Referer":      s.BaseURL + refererPath,
		"Content-Type": "application/x-www-form-urlencoded",
	}
	var body []byte
	if method == http.MethodPost {
		body = []byte(vals.Encode())
	}
	_, _, err := s.doRequestWithRetry(method, s.BaseURL+urlPath, headers, body)
	return err
}

func getCol(m map[string]int, key string, def int) int {
	if m == nil {
		return def
	}
	if v, ok := m[key]; ok {
		return v
	}
	return def
}

func getMapKey(m map[string]string, key, def string) string {
	if m == nil {
		return def
	}
	if v, ok := m[key]; ok && v != "" {
		return v
	}
	return def
}

func mapStr(m map[string]any, key, def string) string {
	if v, ok := m[key]; ok && v != nil {
		return fmt.Sprintf("%v", v)
	}
	return def
}

func defaultStr(val, def string) string {
	if strings.TrimSpace(val) == "" {
		return def
	}
	return val
}

func containsAny(s string, terms []string) bool {
	for _, t := range terms {
		if strings.Contains(s, strings.ToLower(t)) {
			return true
		}
	}
	return false
}

func maxInt(nums ...int) int {
	m := -1
	for _, n := range nums {
		if n > m {
			m = n
		}
	}
	return m
}
