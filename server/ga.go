package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// gaCacheEntry holds a cached GA JS response. Always accessed via pointer so
// the embedded mutex is never copied.
type gaCacheEntry struct {
	mu         sync.Mutex
	lastUpdate int64
	src        []byte
	headers    http.Header
}

var (
	gaCacheMu sync.RWMutex
	gaCache   = make(map[string]*gaCacheEntry)
)

// getOrCreateGAEntry returns the existing cache entry for key, or creates and
// stores a new zeroed one. The returned pointer is always valid.
func getOrCreateGAEntry(key string) *gaCacheEntry {
	gaCacheMu.RLock()
	entry, ok := gaCache[key]
	gaCacheMu.RUnlock()
	if ok {
		return entry
	}

	gaCacheMu.Lock()
	defer gaCacheMu.Unlock()
	if entry, ok = gaCache[key]; ok {
		return entry
	}
	entry = &gaCacheEntry{}
	gaCache[key] = entry
	return entry
}

// generateGACookie returns a new random GA client ID in the standard format:
// GA<version>.2.<9-digit-random>.<unix-timestamp>
func generateGACookie() string {
	random := rand.IntN(888888888) + 111111111
	return "GA" + GaCookieVersion + ".2." +
		strconv.FormatInt(int64(random), 10) + "." +
		strconv.FormatInt(time.Now().Unix(), 10)
}

func googleAnalyticsJsHandle(w http.ResponseWriter, r *http.Request, path string) {
	endpointURI := cfg.EndpointURI
	cookieDomain := cfg.CookieDomain
	cacheKey := path

	if endpointURI == "" {
		endpointURI = r.Host
		cacheKey = endpointURI + "/" + path
	}
	if cookieDomain == "" {
		cookieDomain = r.Host
	}

	entry := getOrCreateGAEntry(cacheKey)
	entry.mu.Lock()
	defer entry.mu.Unlock()

	now := time.Now().Unix()
	cacheValid := entry.lastUpdate > now-cfg.GaCacheTime

	var (
		body        []byte
		respHeaders http.Header
		statusCode  = http.StatusOK
		usedCache   bool
	)

	if cacheValid {
		if cfg.EnableDebugOutput {
			logger.Debug("GA JS served from cache", "path", path)
		}
		body = entry.src
		respHeaders = entry.headers
		usedCache = true
	} else {
		upstreamURL, err := resolveGAUpstreamURL(path)
		if err != nil {
			logger.Error("could not resolve GA upstream URL", "path", path, "err", err)
			http.Error(w, "invalid GA path", http.StatusBadRequest)
			return
		}

		if cfg.EnableDebugOutput {
			logger.Debug("fetching GA JS from upstream", "url", upstreamURL)
		}

		req, err := http.NewRequest(http.MethodGet, upstreamURL, nil)
		if err != nil {
			logger.Error("failed to create GA upstream request", "err", err)
			http.Error(w, "upstream request error", http.StatusBadGateway)
			return
		}
		req.Header.Set("User-Agent", "GoGtmGaProxy "+os.Getenv("APP_VERSION")+"; github.com/blaumedia/go-gtm-ga-proxy")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			logger.Error("GA upstream request failed", "err", err)
			http.Error(w, "upstream request failed", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		body, err = io.ReadAll(resp.Body)
		if err != nil {
			logger.Error("failed to read GA upstream response body", "err", err)
			http.Error(w, "failed to read upstream response", http.StatusBadGateway)
			return
		}

		body = rewriteGABody(body, endpointURI)

		if cfg.JsEnableMinify {
			minified, merr := minifyJS(body)
			if merr != nil {
				logger.Error("JS minification failed for GA", "err", merr)
				// Non-fatal: serve unminified.
			} else {
				if cfg.EnableDebugOutput {
					logger.Debug("GA JS minified", "path", path)
				}
				body = minified
			}
		}

		statusCode = resp.StatusCode
		respHeaders = resp.Header

		if resp.StatusCode == http.StatusOK {
			entry.src = body
			entry.headers = resp.Header
			entry.lastUpdate = now
		}
		usedCache = false
	}

	// Manage server-side GA cookies (ITP bypass).
	if cfg.EnableServerSideGaCookies {
		setGACookies(w, r, cookieDomain)
	}

	setResponseHeaders(w, respHeaders)
	if usedCache {
		w.Header().Set("X-Cache-Hit", "true")
	} else {
		w.Header().Set("X-Cache-Hit", "false")
	}

	// Run registered post-GA-JS hooks.
	for _, hook := range cfg.pluginEngine.dispatcher["after_ga_js"] {
		hook(&w, r, &statusCode, &body)
	}

	w.WriteHeader(statusCode)
	w.Write(body) //nolint:errcheck
}

// resolveGAUpstreamURL maps a path token to the appropriate upstream GA URL.
func resolveGAUpstreamURL(path string) (string, error) {
	switch path {
	case "default":
		return "https://www.google-analytics.com/analytics.js", nil
	case "debug":
		return "https://www.google-analytics.com/analytics_debug.js", nil
	default:
		// Plugin file – translate the obfuscated directory name back to /plugins/.
		translated := strings.ReplaceAll(path, "/"+cfg.JsSubdirectory+"/"+cfg.GaPluginsDirectoryname+"/", "/plugins/")
		match := regexp.MustCompile(`(/plugins/[^?#]+\.js)`).FindString(translated)
		if match == "" {
			return "", fmt.Errorf("could not extract plugin path from %q", path)
		}
		return "https://www.google-analytics.com" + match, nil
	}
}

// rewriteGABody performs all URL-rewriting substitutions on the raw GA JS body.
func rewriteGABody(body []byte, endpointURI string) []byte {
	body = regexp.MustCompile(`googletagmanager\.com`).
		ReplaceAll(body, []byte(endpointURI))

	body = regexp.MustCompile(`/gtm\.js`).
		ReplaceAll(body, []byte("/"+cfg.JsSubdirectory+"/"+cfg.GtmFilename))

	body = regexp.MustCompile(`www\.google-analytics\.com`).
		ReplaceAll(body, []byte(endpointURI))

	body = regexp.MustCompile(`analytics\.js`).
		ReplaceAll(body, []byte("/"+cfg.JsSubdirectory+"/"+cfg.GaFilename))

	body = regexp.MustCompile(`u/analytics_debug\.js`).
		ReplaceAll(body, []byte("/"+cfg.JsSubdirectory+"/"+cfg.GaDebugFilename))

	// Rewrite collect endpoints (order matters: most-specific first).
	body = regexp.MustCompile(`"/r/collect`).
		ReplaceAll(body, []byte(`"`+cfg.GaCollectEndpointRedirect))

	body = regexp.MustCompile(`"/j/collect`).
		ReplaceAll(body, []byte(`"`+cfg.GaCollectEndpointJ))

	body = regexp.MustCompile(`"/collect`).
		ReplaceAll(body, []byte(`"`+cfg.GaCollectEndpoint))

	body = regexp.MustCompile(`/plugins/`).
		ReplaceAll(body, []byte("/"+cfg.JsSubdirectory+"/"+cfg.GaPluginsDirectoryname+"/"))

	return body
}

// setGACookies reads or generates the GA client ID and writes both the
// HttpOnly server-side cookie and the plain client-side cookie.
func setGACookies(w http.ResponseWriter, r *http.Request, cookieDomain string) {
	var encoded, decoded string

	if serverCookie, err := r.Cookie(cfg.ServerSideGaCookieName); err == nil {
		// Re-use the existing server-side cookie if it decodes cleanly.
		if raw, decErr := base64.StdEncoding.DecodeString(serverCookie.Value); decErr == nil {
			if isSafeCookieValue(string(raw)) {
				encoded = serverCookie.Value
				decoded = string(raw)
			}
		}
	} else if gaCookie, err := r.Cookie("_ga"); err == nil {
		// Fall back to an existing plain _ga cookie.
		if isSafeCookieValue(gaCookie.Value) {
			decoded = gaCookie.Value
			encoded = base64.StdEncoding.EncodeToString([]byte(decoded))
		}
	}

	if encoded == "" || decoded == "" {
		decoded = generateGACookie()
		encoded = base64.StdEncoding.EncodeToString([]byte(decoded))
	}

	secure := ""
	if cfg.CookieSecure {
		secure = "; Secure"
	}

	w.Header().Add("Set-Cookie",
		cfg.ServerSideGaCookieName+"="+encoded+
			"; Domain="+cookieDomain+secure+"; HttpOnly; SameSite=Lax; Path=/; Max-Age=63072000")
	w.Header().Add("Set-Cookie",
		cfg.ClientSideGaCookieName+"="+decoded+
			"; Domain="+cookieDomain+secure+"; SameSite=Lax; Path=/; Max-Age=63072000")

	if cfg.EnableDebugOutput {
		logger.Debug("GA cookies set",
			"server_cookie", cfg.ServerSideGaCookieName,
			"domain", cookieDomain,
			"secure", cfg.CookieSecure,
		)
	}
}

// isSafeCookieValue reports whether the string contains only alphanumeric
// characters and dots, which is sufficient for GA cookie values.
func isSafeCookieValue(v string) bool {
	if v == "" {
		return false
	}
	for _, c := range v {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.') {
			return false
		}
	}
	return true
}

func googleAnalyticsCollectHandle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		logger.Warn("collect endpoint called with unsupported method", "method", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Resolve the upstream GA collect URL.
	var upstreamURL string
	switch r.URL.Path {
	case cfg.GaCollectEndpointRedirect:
		upstreamURL = "https://www.google-analytics.com/r/collect"
	case cfg.GaCollectEndpointJ:
		upstreamURL = "https://www.google-analytics.com/j/collect"
	default:
		upstreamURL = "https://www.google-analytics.com/collect"
	}

	// Parse the incoming payload.
	payload, err := parseCollectPayload(r)
	if err != nil {
		logger.Error("failed to parse collect payload", "err", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Strip client-supplied ua / uip and inject real values server-side.
	delete(payload, "uip")
	delete(payload, "ua")

	clientIP := resolveClientIP(r)
	payload["uip"] = clientIP
	payload["ua"] = r.Header.Get("User-Agent")

	queryString := buildQueryString(payload)

	if cfg.EnableDebugOutput {
		logger.Debug("collect forwarding payload", "url", upstreamURL, "payload", queryString)
	}

	// Build the upstream request.
	var req *http.Request
	switch r.Method {
	case http.MethodGet:
		req, err = http.NewRequest(http.MethodGet, upstreamURL+"?"+queryString, nil)
	case http.MethodPost:
		req, err = http.NewRequest(http.MethodPost, upstreamURL, bytes.NewBufferString(queryString))
	}
	if err != nil {
		logger.Error("failed to create collect upstream request", "err", err)
		http.Error(w, "upstream request error", http.StatusBadGateway)
		return
	}

	req.Header.Set("User-Agent", "GoGtmGaProxy "+os.Getenv("APP_VERSION")+"; github.com/blaumedia/go-gtm-ga-proxy")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")

	// Do not follow redirects so we can relay Google Ads 302s to the client.
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("redirect")
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		// A redirect is not an error we should abort on.
		if resp != nil && resp.StatusCode == http.StatusFound {
			loc, locErr := resp.Location()
			if locErr == nil {
				logger.Info("relaying Google Ads redirect", "location", loc.String())
				http.Redirect(w, r, loc.String(), http.StatusFound)
				return
			}
		}
		logger.Error("collect upstream request failed", "err", err)
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("failed to read collect upstream response body", "err", err)
		http.Error(w, "failed to read upstream response", http.StatusBadGateway)
		return
	}

	setResponseHeaders(w, resp.Header)
	w.WriteHeader(resp.StatusCode)
	w.Write(body) //nolint:errcheck
}

// googleAnalytics4CollectHandle proxies GA4 Measurement Protocol hits
// (POST /g/collect) to Google. GA4 encodes protocol parameters on the query
// string and hit parameters in the POST body; both are forwarded as-is with
// only the client IP and user-agent overridden server-side.
func googleAnalytics4CollectHandle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		logger.Warn("GA4 collect endpoint called with unsupported method", "method", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// GA4 passes protocol params on the query string (v, tid, gtm, etc.).
	// Rebuild them, stripping and re-injecting uip/ua server-side.
	qs := r.URL.Query()
	qs.Del("uip")
	qs.Del("ua")
	qs.Set("uip", resolveClientIP(r))
	qs.Set("ua", r.Header.Get("User-Agent"))

	upstreamURL := "https://www.google-analytics.com/g/collect?" + qs.Encode()

	if cfg.EnableDebugOutput {
		logger.Debug("GA4 collect forwarding", "url", upstreamURL)
	}

	// Forward the body unchanged – it contains the hit payload.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error("failed to read GA4 collect request body", "err", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	req, err := http.NewRequest(http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		logger.Error("failed to create GA4 upstream request", "err", err)
		http.Error(w, "upstream request error", http.StatusBadGateway)
		return
	}

	req.Header.Set("User-Agent", "GoGtmGaProxy "+os.Getenv("APP_VERSION")+"; github.com/blaumedia/go-gtm-ga-proxy")
	req.Header.Set("Accept", "*/*")
	// Preserve the original Content-Type (GA4 uses application/x-www-form-urlencoded).
	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	} else {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Error("GA4 collect upstream request failed", "err", err)
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("failed to read GA4 collect upstream response body", "err", err)
		http.Error(w, "failed to read upstream response", http.StatusBadGateway)
		return
	}

	setResponseHeaders(w, resp.Header)
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody) //nolint:errcheck
}

// parseCollectPayload extracts key=value pairs from a GET query string or a
// POST body into a plain map.
func parseCollectPayload(r *http.Request) (map[string]string, error) {
	out := make(map[string]string)
	switch r.Method {
	case http.MethodGet:
		for k, v := range r.URL.Query() {
			out[k] = v[0]
		}
	case http.MethodPost:
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		for _, pair := range strings.Split(string(raw), "&") {
			if pair == "" {
				continue
			}
			k, v, _ := strings.Cut(pair, "=")
			dk, err := url.QueryUnescape(k)
			if err != nil {
				dk = k
			}
			dv, err := url.QueryUnescape(v)
			if err != nil {
				dv = v
			}
			out[dk] = dv
		}
	}
	return out, nil
}

// resolveClientIP returns the client IP address, preferring the configured
// proxy header when set.
func resolveClientIP(r *http.Request) string {
	headerName := os.Getenv("PROXY_IP_HEADER")
	if headerName == "" {
		// Fall back to TCP remote address, stripping the port.
		ip, _, _ := strings.Cut(r.RemoteAddr, ":")
		return ip
	}

	parts := strings.Split(r.Header.Get(headerName), ",")
	idx := 0
	if raw := os.Getenv("PROXY_IP_HEADER_INDEX"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			logger.Warn("PROXY_IP_HEADER_INDEX is not a valid integer, using 0", "value", raw)
		} else {
			idx = n
		}
	}

	if idx < 0 || idx >= len(parts) {
		logger.Warn("PROXY_IP_HEADER_INDEX out of range, falling back to 0",
			"index", idx, "count", len(parts))
		idx = 0
	}

	return strings.TrimSpace(parts[idx])
}

// buildQueryString joins a map into a URL-encoded query string.
func buildQueryString(params map[string]string) string {
	var b strings.Builder
	first := true
	for k, v := range params {
		if !first {
			b.WriteByte('&')
		}
		b.WriteString(url.QueryEscape(k))
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(v))
		first = false
	}
	return b.String()
}

// minifyJS is defined in gtm.go and shared across this package.
