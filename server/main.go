package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"plugin"
	"strconv"
	"strings"
)

// GaCookieVersion is the version prefix embedded in generated _ga cookie values.
const GaCookieVersion = "1"

// HookFunc is the signature for plugin hook functions.
type HookFunc func(*http.ResponseWriter, *http.Request, *int, *[]byte)

// pluginSystem holds loaded plugins and their registered event hooks.
type pluginSystem struct {
	plugins    []*plugin.Plugin
	dispatcher map[string][]HookFunc
}

// config holds all runtime configuration derived from environment variables.
type config struct {
	EnableDebugOutput         bool
	EndpointURI               string
	JsSubdirectory            string
	GaCacheTime               int64
	GtmCacheTime              int64
	JsEnableMinify            bool
	GtmFilename               string
	GtmAFilename              string
	GaFilename                string
	GaDebugFilename           string
	GaPluginsDirectoryname    string
	GtagFilename              string
	GaCollectEndpoint         string
	GaCollectEndpointRedirect string
	GaCollectEndpointJ        string
	Ga4CollectEndpoint        string
	RestrictGtmIds            bool
	AllowedGtmIds             []string
	EnableServerSideGaCookies bool
	ServerSideGaCookieName    string
	CookieDomain              string
	CookieSecure              bool
	ClientSideGaCookieName    string
	PluginsEnabled            bool
	pluginEngine              pluginSystem
}

// cfg is the package-level configuration, populated once at startup.
var cfg config

// logger is the structured logger used throughout the application.
var logger *slog.Logger

// envBool returns true when the named environment variable is "true" or "1" (case-insensitive).
func envBool(name string) bool {
	v := strings.ToLower(os.Getenv(name))
	return v == "true" || v == "1"
}

// envInt64 parses the named environment variable as a base-10 int64.
// Returns 0 when the variable is unset. Logs a warning only when the
// variable is set but cannot be parsed as an integer.
func envInt64(name string) int64 {
	raw := os.Getenv(name)
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		logger.Warn("could not parse environment variable as int64", "name", name, "value", raw)
	}
	return v
}

// envRequired returns the value of the named environment variable or exits the
// process with a clear error message when the variable is unset or empty.
func envRequired(name string) string {
	v := os.Getenv(name)
	if v == "" {
		logger.Error("required environment variable is missing", "name", name)
		os.Exit(1)
	}
	return v
}

// containsString reports whether val is present in slice.
func containsString(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

// setResponseHeaders copies a curated set of upstream headers to the client
// response and appends the X-Powered-By header.
func setResponseHeaders(w http.ResponseWriter, headers http.Header) {
	allowed := map[string]struct{}{
		"Age":           {},
		"Cache-Control": {},
		"Content-Type":  {},
		"Date":          {},
		"Expires":       {},
		"Last-Modified": {},
	}
	for name, values := range headers {
		if _, ok := allowed[name]; ok {
			w.Header().Set(name, values[0])
		}
	}
	w.Header().Set("X-Powered-By", "GoGtmGaProxy "+os.Getenv("APP_VERSION"))
}

// javascriptFilesHandle routes JS-file requests to the appropriate handler.
func javascriptFilesHandle(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	jsDir := "/" + cfg.JsSubdirectory + "/"
	pluginPrefix := jsDir + cfg.GaPluginsDirectoryname + "/"

	switch p {
	case jsDir + cfg.GtmFilename:
		googleTagManagerHandle(w, r, "default")
	case jsDir + cfg.GtmAFilename:
		googleTagManagerHandle(w, r, "default_a")
	case jsDir + cfg.GtagFilename:
		googleTagManagerHandle(w, r, "gtag")
	case jsDir + cfg.GaFilename:
		googleAnalyticsJsHandle(w, r, "default")
	case jsDir + cfg.GaDebugFilename:
		googleAnalyticsJsHandle(w, r, "debug")
	default:
		if strings.HasPrefix(p, pluginPrefix) {
			googleAnalyticsJsHandle(w, r, p)
		} else {
			logger.Warn("404 - unknown path accessed", "path", p)
			http.Error(w, "Not found", http.StatusNotFound)
		}
	}
}

// collectHandle routes collect-endpoint requests to the UA GA handler.
func collectHandle(w http.ResponseWriter, r *http.Request) {
	googleAnalyticsCollectHandle(w, r)
}

// ga4CollectHandle routes collect-endpoint requests to the GA4 handler.
func ga4CollectHandle(w http.ResponseWriter, r *http.Request) {
	googleAnalytics4CollectHandle(w, r)
}

func main() {
	// Initialise structured logger (text format for human-readable container logs).
	logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Validate and load all required environment variables.
	requiredVars := []string{
		"JS_SUBDIRECTORY",
		"GA_PLUGINS_DIRECTORYNAME",
		"GTM_FILENAME",
		"GTM_A_FILENAME",
		"GTAG_FILENAME",
		"GA_FILENAME",
		"GADEBUG_FILENAME",
		"GA_COLLECT_ENDPOINT",
		"GA_COLLECT_REDIRECT_ENDPOINT",
		"GA_COLLECT_J_ENDPOINT",
		"GA4_COLLECT_ENDPOINT",
	}
	for _, v := range requiredVars {
		envRequired(v)
	}

	cfg = config{
		EnableDebugOutput:         envBool("ENABLE_DEBUG_OUTPUT"),
		EndpointURI:               os.Getenv("ENDPOINT_URI"),
		JsSubdirectory:            os.Getenv("JS_SUBDIRECTORY"),
		GaCacheTime:               envInt64("GA_CACHE_TIME"),
		GtmCacheTime:              envInt64("GTM_CACHE_TIME"),
		JsEnableMinify:            envBool("JS_MINIFY"),
		GtmFilename:               os.Getenv("GTM_FILENAME"),
		GtmAFilename:              os.Getenv("GTM_A_FILENAME"),
		GaFilename:                os.Getenv("GA_FILENAME"),
		GaDebugFilename:           os.Getenv("GADEBUG_FILENAME"),
		GaPluginsDirectoryname:    os.Getenv("GA_PLUGINS_DIRECTORYNAME"),
		GtagFilename:              os.Getenv("GTAG_FILENAME"),
		GaCollectEndpoint:         os.Getenv("GA_COLLECT_ENDPOINT"),
		GaCollectEndpointRedirect: os.Getenv("GA_COLLECT_REDIRECT_ENDPOINT"),
		GaCollectEndpointJ:        os.Getenv("GA_COLLECT_J_ENDPOINT"),
		Ga4CollectEndpoint:        os.Getenv("GA4_COLLECT_ENDPOINT"),
		RestrictGtmIds:            envBool("RESTRICT_GTM_IDS"),
		AllowedGtmIds:             strings.Split(os.Getenv("GTM_IDS"), ","),
		EnableServerSideGaCookies: envBool("ENABLE_SERVER_SIDE_GA_COOKIES"),
		ServerSideGaCookieName:    os.Getenv("GA_SERVER_SIDE_COOKIE"),
		CookieDomain:              os.Getenv("COOKIE_DOMAIN"),
		CookieSecure:              envBool("COOKIE_SECURE"),
		ClientSideGaCookieName:    "_ga",
		PluginsEnabled:            envBool("ENABLE_PLUGINS"),
		pluginEngine: pluginSystem{
			dispatcher: make(map[string][]HookFunc),
		},
	}

	// Allow overriding the client-side GA cookie name.
	if v := os.Getenv("GA_CLIENT_SIDE_COOKIE"); v != "" {
		cfg.ClientSideGaCookieName = v
	}

	// Load plugins if enabled.
	if cfg.PluginsEnabled {
		if err := loadPlugins("/app/plugins"); err != nil {
			logger.Error("failed to load plugins", "err", err)
			os.Exit(1)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/"+cfg.JsSubdirectory+"/", javascriptFilesHandle)
	mux.HandleFunc(cfg.GaCollectEndpoint, collectHandle)
	mux.HandleFunc(cfg.GaCollectEndpointRedirect, collectHandle)
	mux.HandleFunc(cfg.GaCollectEndpointJ, collectHandle)
	mux.HandleFunc(cfg.Ga4CollectEndpoint, ga4CollectHandle)

	addr := ":8080"
	logger.Info("server starting", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		logger.Error("server exited with error", "err", err)
		os.Exit(1)
	}
}

// loadPlugins walks dir, opens every .so file as a Go plugin, calls its Main()
// function, and registers all hooks from its Dispatcher map.
func loadPlugins(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("plugins directory %q does not exist – use the plugin-enabled image or set ENABLE_PLUGINS=false", dir)
	}

	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".so") {
			return nil
		}

		logger.Info("loading plugin", "path", path)
		p, err := plugin.Open(path)
		if err != nil {
			return fmt.Errorf("open plugin %q: %w", path, err)
		}

		mainSym, err := p.Lookup("Main")
		if err != nil {
			return fmt.Errorf("plugin %q missing Main symbol: %w", path, err)
		}
		mainSym.(func())()

		dispatcherSym, err := p.Lookup("Dispatcher")
		if err != nil {
			return fmt.Errorf("plugin %q missing Dispatcher symbol: %w", path, err)
		}

		dispatcher := *dispatcherSym.(*map[string][]HookFunc)
		for event, hooks := range dispatcher {
			cfg.pluginEngine.dispatcher[event] = append(cfg.pluginEngine.dispatcher[event], hooks...)
		}

		cfg.pluginEngine.plugins = append(cfg.pluginEngine.plugins, p)
		return nil
	})
}
