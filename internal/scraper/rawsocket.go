package scraper

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"
)

type rawResponse struct {
	StatusLine string
	Headers    map[string]string
	Cookies    map[string]string
	Body       []byte
	FinalURL   string
}

func doRawSocketRequest(targetURL, method string, headers map[string]string, cookies map[string]string, body []byte, proto string, timeout time.Duration) (*rawResponse, error) {
	u, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}
	path := u.Path
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "80"
	}
	if proto == "" {
		proto = "HTTP/1.1"
	}

	hdrs := make(map[string]string)
	for k, v := range headers {
		hdrs[k] = v
	}
	if _, ok := hdrs["Host"]; !ok {
		hdrs["Host"] = u.Host
	}
	if _, ok := hdrs["User-Agent"]; !ok {
		hdrs["User-Agent"] = "Mozilla/5.0 (X11; Linux x86_64) SwitchDashboard/2.0"
	}
	hdrs["Connection"] = "close"
	if len(body) > 0 {
		hdrs["Content-Length"] = fmt.Sprintf("%d", len(body))
	}
	if len(cookies) > 0 {
		var cParts []string
		for k, v := range cookies {
			cParts = append(cParts, fmt.Sprintf("%s=%s", k, v))
		}
		hdrs["Cookie"] = strings.Join(cParts, "; ")
	}

	var reqBuf bytes.Buffer
	reqBuf.WriteString(fmt.Sprintf("%s %s %s\r\n", method, path, proto))
	for k, v := range hdrs {
		reqBuf.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	reqBuf.WriteString("\r\n")
	if len(body) > 0 {
		reqBuf.Write(body)
	}

	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), timeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	if _, err := conn.Write(reqBuf.Bytes()); err != nil {
		return nil, err
	}

	rawBytes, err := io.ReadAll(io.LimitReader(conn, 16*1024*1024))
	if err != nil && len(rawBytes) == 0 {
		return nil, err
	}

	resp := &rawResponse{
		Headers:  make(map[string]string),
		Cookies:  make(map[string]string),
		FinalURL: targetURL,
	}

	sep := []byte("\r\n\r\n")
	idx := bytes.Index(rawBytes, sep)
	if idx == -1 {
		// Some ultra-minimal uIP responses might omit headers or use \n\n
		sepLF := []byte("\n\n")
		if idxLF := bytes.Index(rawBytes, sepLF); idxLF != -1 {
			headerPart := string(rawBytes[:idxLF])
			resp.Body = rawBytes[idxLF+2:]
			parseRawHeaders(headerPart, resp)
			return resp, nil
		}
		resp.Body = rawBytes
		return resp, nil
	}

	headerPart := string(rawBytes[:idx])
	resp.Body = rawBytes[idx+4:]
	parseRawHeaders(headerPart, resp)

	if loc, ok := resp.Headers["location"]; ok && loc != "" {
		if strings.HasPrefix(loc, "http://") || strings.HasPrefix(loc, "https://") {
			resp.FinalURL = loc
		} else {
			if !strings.HasPrefix(loc, "/") {
				loc = "/" + loc
			}
			resp.FinalURL = fmt.Sprintf("http://%s%s", u.Host, loc)
		}
	}

	return resp, nil
}

func parseRawHeaders(headerPart string, resp *rawResponse) {
	lines := strings.Split(strings.ReplaceAll(headerPart, "\r\n", "\n"), "\n")
	if len(lines) > 0 {
		resp.StatusLine = strings.TrimSpace(lines[0])
	}
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(parts[0]))
		v := strings.TrimSpace(parts[1])
		resp.Headers[k] = v
		if k == "set-cookie" {
			semi := strings.Split(v, ";")
			if len(semi) > 0 {
				kv := strings.SplitN(semi[0], "=", 2)
				if len(kv) == 2 {
					resp.Cookies[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
				}
			}
		}
	}
}
