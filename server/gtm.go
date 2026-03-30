package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/tdewolff/minify/v2"
)

// gtmCacheEntry holds a cached GTM JS response. It is always stored and
// accessed via pointer so the embedded mutex is never copied.
type gtmCacheEntry struct {
	mu         sync.Mutex
	lastUpdate int64
	src        []byte
	headers    http.Header
}

var (
	gtmCacheMu sync.RWMutex
	gtmCache   = make(map[string]*gtmCacheEntry)
)

// getOrCreateGTMEntry returns the existing cache entry for key, or creates and
// stores a new zeroed entry. The returned pointer is always valid.
func getOrCreateGTMEntry(key string) *gtmCacheEntry {
	gtmCacheMu.RLock()
	entry, ok := gtmCache[key]
	gtmCacheMu.RUnlock()
	if ok {
		return entry
	}

	gtmCacheMu.Lock()
	defer gtmCacheMu.Unlock()
	// Double-check after acquiring the write lock.
	if entry, ok = gtmCache[key]; ok {
		return entry
	}
	entry = &gtmCacheEntry{}
	gtmCache[key] = entry
	return entry
}

func googleTagManagerHandle(w http.ResponseWriter, r *http.Request, path string) {
	endpointURI := cfg.EndpointURI
	if endpointURI == "" {
		endpointURI = r.Host
	}

	// Extract and normalise the GTM container ID from ?id=.
	idParam, ok := r.URL.Query()["id"]
	if !ok || len(idParam[0]) == 0 {
		logger.Warn("GTM request missing 'id' query parameter")
		http.Error(w, "missing 'id' query parameter", http.StatusBadRequest)
		return
	}

	containerID := idParam[0]
	if strings.HasPrefix(containerID, "GTM-") {
		containerID = containerID[4:]
	} else if len(containerID) < 4 {
		logger.Warn("GTM request has malformed 'id' parameter", "id", containerID)
		http.Error(w, fmt.Sprintf("malformed 'id' parameter: %s", containerID), http.StatusBadRequest)
		return
	}

	// Build extra URL query parameters (everything except 'id').
	var extraParams strings.Builder
	for key, vals := range r.URL.Query() {
		if key != "id" {
			extraParams.WriteString("&")
			extraParams.WriteString(key)
			extraParams.WriteString("=")
			extraParams.WriteString(vals[0])
		}
	}
	urlAddition := extraParams.String()

	// Enforce container ID whitelist when configured.
	rawID := idParam[0]
	if cfg.RestrictGtmIds &&
		!containsString(cfg.AllowedGtmIds, rawID) &&
		!containsString(cfg.AllowedGtmIds, containerID) {
		logger.Warn("blocked disallowed GTM container ID", "id", rawID)
		http.Error(w, fmt.Sprintf("GTM ID %q is not allowed", rawID), http.StatusForbidden)
		return
	}

	// Collect gtm_* cookies for GTM preview mode pass-through.
	var gtmCookies []string
	for _, cookie := range r.Cookies() {
		if strings.HasPrefix(cookie.Name, "gtm_") {
			gtmCookies = append(gtmCookies, cookie.Name+"="+cookie.Value)
		}
	}

	cacheKey := endpointURI + "/" + containerID + urlAddition
	entry := getOrCreateGTMEntry(cacheKey)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	now := time.Now().Unix()
	cacheValid := len(gtmCookies) == 0 && entry.lastUpdate > now-cfg.GtmCacheTime

	var (
		body        []byte
		respHeaders http.Header
		statusCode  = http.StatusOK
		usedCache   bool
	)

	if cacheValid {
		if cfg.EnableDebugOutput {
			logger.Debug("GTM JS served from cache", "containerID", containerID)
		}
		body = entry.src
		respHeaders = entry.headers
		usedCache = true
	} else {
		var upstream string
		switch path {
		case "default":
			upstream = "https://www.googletagmanager.com/gtm.js?id=GTM-" + containerID + urlAddition
		case "default_a":
			upstream = "https://www.googletagmanager.com/a?id=GTM-" + containerID + urlAddition
		case "gtag":
			upstream = "https://www.googletagmanager.com/gtag/js?id=" + containerID + urlAddition
		default:
			logger.Warn("unknown GTM path variant", "path", path)
			http.Error(w, "unknown GTM path", http.StatusBadRequest)
			return
		}

		if cfg.EnableDebugOutput {
			logger.Debug("fetching GTM JS from upstream", "url", upstream)
		}

		req, err := http.NewRequest(http.MethodGet, upstream, nil)
		if err != nil {
			logger.Error("failed to create GTM upstream request", "err", err)
			http.Error(w, "upstream request error", http.StatusBadGateway)
			return
		}
		req.Header.Set("User-Agent", "GoGtmGaProxy "+os.Getenv("APP_VERSION")+"; github.com/blaumedia/go-gtm-ga-proxy")
		if len(gtmCookies) > 0 {
			req.Header.Set("Cookie", strings.Join(gtmCookies, "; "))
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			logger.Error("GTM upstream request failed", "err", err)
			http.Error(w, "upstream request failed", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		body, err = io.ReadAll(resp.Body)
		if err != nil {
			logger.Error("failed to read GTM upstream response body", "err", err)
			http.Error(w, "failed to read upstream response", http.StatusBadGateway)
			return
		}

		body = rewriteGTMBody(body, endpointURI)

		if cfg.JsEnableMinify {
			body, err = minifyJS(body)
			if err != nil {
				logger.Error("JS minification failed", "err", err)
				// Non-fatal: serve unminified body.
			} else if cfg.EnableDebugOutput {
				logger.Debug("GTM JS minified", "containerID", containerID)
			}
		}

		statusCode = resp.StatusCode
		respHeaders = resp.Header

		// Only cache clean (non-preview) successful responses.
		if resp.StatusCode == http.StatusOK && len(gtmCookies) == 0 {
			entry.src = body
			entry.headers = resp.Header
			entry.lastUpdate = now
		}
		usedCache = false
	}

	setResponseHeaders(w, respHeaders)
	if usedCache {
		w.Header().Set("X-Cache-Hit", "true")
	} else {
		w.Header().Set("X-Cache-Hit", "false")
	}

	// Run registered post-GTM-JS hooks.
	for _, hook := range cfg.pluginEngine.dispatcher["after_gtm_js"] {
		hook(&w, r, &statusCode, &body)
	}

	w.WriteHeader(statusCode)
	w.Write(body) //nolint:errcheck
}

// rewriteGTMBody performs all URL-rewriting substitutions on the raw GTM JS body.
func rewriteGTMBody(body []byte, endpointURI string) []byte {
	// Replace all googletagmanager.com references with the proxy host.
	body = regexp.MustCompile(`(www\.)?googletagmanager\.com`).ReplaceAll(body, []byte(endpointURI))

	// The /a endpoint serves a tracking pixel – keep it pointing at Google.
	body = regexp.MustCompile(regexp.QuoteMeta(endpointURI)+`/a`).
		ReplaceAll(body, []byte(`www.googletagmanager.com/a`))

	body = regexp.MustCompile(`/gtm\.js`).
		ReplaceAll(body, []byte("/"+cfg.JsSubdirectory+"/"+cfg.GtmFilename))

	body = regexp.MustCompile(`www\.google-analytics\.com`).
		ReplaceAll(body, []byte(endpointURI))

	body = regexp.MustCompile(`(/?)(analytics\.js)`).
		ReplaceAll(body, []byte("/"+cfg.JsSubdirectory+"/"+cfg.GaFilename))

	body = regexp.MustCompile(`u/analytics_debug\.js`).
		ReplaceAll(body, []byte("/"+cfg.JsSubdirectory+"/"+cfg.GaDebugFilename))

	body = regexp.MustCompile(`/gtag/js`).
		ReplaceAll(body, []byte("/"+cfg.JsSubdirectory+"/"+cfg.GtagFilename))

	// Rewrite GA4 collect endpoint so hits are routed through the proxy.
	body = regexp.MustCompile(`"/g/collect`).
		ReplaceAll(body, []byte(`"`+cfg.Ga4CollectEndpoint))

	return body
}

// minifyJS runs the body through uglifyjs via the tdewolff/minify adapter.
func minifyJS(body []byte) ([]byte, error) {
	m := minify.New()
	m.AddCmd("application/javascript", exec.Command("uglifyjs"))
	return m.Bytes("application/javascript", body)
}
