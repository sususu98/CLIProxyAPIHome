package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/router-for-me/CLIProxyAPIHome/internal/cluster"
	"github.com/router-for-me/CLIProxyAPIHome/internal/config"
	"github.com/router-for-me/CLIProxyAPIHome/internal/home"
	"github.com/router-for-me/CLIProxyAPIHome/internal/logging"
	"github.com/router-for-me/CLIProxyAPIHome/internal/managementhttp"
	"github.com/router-for-me/CLIProxyAPIHome/internal/protocolmux"
	"github.com/router-for-me/CLIProxyAPIHome/internal/respserver"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/http2"
)

// main starts the home command entrypoint.
func main() {
	os.Exit(run())
}

// run executes the home command and returns the process exit code.
func run() int {
	// Keep validation before state changes so failures leave existing data intact.
	logging.SetupBaseLogger()

	var configPath string
	var addr string
	var disbandCluster bool
	flag.StringVar(&configPath, "config", "config.yaml", "Config file path")
	flag.StringVar(&addr, "addr", "", "Override RESP listen address (host:port)")
	flag.BoolVar(&disbandCluster, "disband-cluster", false, "Restore config and auth files from cluster database, then exit")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	clusterCfg, clusterExists, errClusterCfg := cluster.LoadConfigOptional("")
	if errClusterCfg != nil {
		log.Errorf("failed to load cluster config: %v", errClusterCfg)
		return 1
	}
	if disbandCluster {
		if !clusterExists {
			log.Infof("cluster config not found; nothing to disband")
			return 0
		}
		return runDisbandCluster(runCtx, clusterCfg, configPath)
	}

	var cfg *config.Config
	var rt *home.Runtime
	var coordinator *cluster.Coordinator
	var clusterRepo *cluster.Repository
	var clusterStartedAt time.Time
	var clusterClientAddr string
	var clusterAdapter *cluster.RuntimeAdapter
	var coordinatorErrCh <-chan error
	var eventWatcher *cluster.EventWatcher
	var eventWatcherErrCh <-chan error
	var clusterRESPHandler *cluster.RESPHandler
	if clusterExists {
		startedAt := time.Now().UTC()
		clusterStartedAt = startedAt
		clusterDB, errClusterOpen := cluster.Open(runCtx, clusterCfg.PGSQL)
		if errClusterOpen != nil {
			log.Errorf("failed to open cluster database: %v", errClusterOpen)
			return 1
		}
		sqlDB, errSQLDB := clusterDB.DB()
		if errSQLDB != nil {
			log.Errorf("failed to get cluster sql db: %v", errSQLDB)
			return 1
		}
		defer func() {
			if errCloseSQL := sqlDB.Close(); errCloseSQL != nil {
				log.Warnf("failed to close cluster sql db: %v", errCloseSQL)
			}
		}()
		if errMigrate := cluster.AutoMigrate(clusterDB); errMigrate != nil {
			log.Errorf("failed to migrate cluster database: %v", errMigrate)
			return 1
		}
		clusterClientAddr = strings.TrimSpace(clusterCfg.Node.ExternalIP)
		if clusterClientAddr == "" {
			clientAddr, errClientAddr := cluster.ClientAddr(runCtx, clusterDB)
			if errClientAddr != nil {
				log.Errorf("failed to get cluster client address: %v", errClientAddr)
				return 1
			}
			clusterClientAddr = clientAddr
		}
		localCfg, errLocalConfig := loadLocalConfigForCluster(configPath)
		if errLocalConfig != nil {
			log.Errorf("failed to load local config %s: %v", configPath, errLocalConfig)
			return 1
		}
		localAuthDir := ""
		if localCfg != nil {
			localAuthDir = localCfg.AuthDir
		}

		repo := cluster.NewRepository(clusterDB)
		clusterRepo = repo
		if errBootstrap := cluster.Bootstrap(runCtx, cluster.BootstrapOptions{
			ConfigPath: configPath,
			AuthDir:    localAuthDir,
			Config:     localCfg,
			Repository: repo,
			Now:        startedAt,
		}); errBootstrap != nil {
			log.Errorf("failed to bootstrap cluster database: %v", errBootstrap)
			return 1
		}

		runtimeCfg, _, errRuntimeConfig := repo.LoadConfigAsRuntimeConfig(runCtx)
		if errRuntimeConfig != nil {
			log.Errorf("failed to load cluster runtime config: %v", errRuntimeConfig)
			return 1
		}
		cfg = runtimeCfg
		applyLogLevel(cfg)

		var errRuntime error
		rt, errRuntime = home.NewRuntime(cfg)
		if errRuntime != nil {
			log.Errorf("failed to init runtime: %v", errRuntime)
			return 1
		}

		adapter := cluster.NewRuntimeAdapter(repo)
		clusterAdapter = adapter
		rt.SetClusterAdapter(adapter)
		lastSeenID, errMaxEventID := repo.MaxEventID(runCtx)
		if errMaxEventID != nil {
			log.Errorf("failed to get cluster max event id: %v", errMaxEventID)
			return 1
		}
		eventWatcher = cluster.NewEventWatcherFrom(repo, clusterCfg.Node.EventPollInterval, lastSeenID, func(eventCtx context.Context, event cluster.ClusterEventRecord) error {
			if strings.EqualFold(strings.TrimSpace(event.Scope), "config") {
				nextConfig, payload, errConfig := repo.LoadConfigAsRuntimeConfig(eventCtx)
				if errConfig != nil {
					return errConfig
				}
				if reflect.DeepEqual(rt.Config(), nextConfig) {
					return nil
				}
				if errApply := rt.ApplyConfigFromCluster(eventCtx, nextConfig); errApply != nil {
					return errApply
				}
				rt.PublishConfigYAML(payload)
				return nil
			}
			if errApplyEvent := adapter.ApplyEvent(eventCtx, event); errApplyEvent != nil {
				return errApplyEvent
			}
			return rt.ReloadAuths(eventCtx)
		})
	} else {
		loadedConfig, errLoad := config.LoadConfigOptional(configPath, false)
		if errLoad != nil {
			log.Errorf("failed to load config %s: %v", configPath, errLoad)
			return 1
		}
		cfg = loadedConfig
		applyLogLevel(cfg)

		var errRuntime error
		rt, errRuntime = home.NewRuntime(cfg)
		if errRuntime != nil {
			log.Errorf("failed to init runtime: %v", errRuntime)
			return 1
		}
	}
	if rt == nil {
		log.Errorf("failed to init runtime")
		return 1
	}
	defer rt.Stop()

	listenAddr, listenPort, errListenAddr := resolveListenAddress(addr, cfg, clusterCfg, clusterExists)
	if errListenAddr != nil {
		log.Errorf("failed to resolve listen address: %v", errListenAddr)
		return 1
	}
	addr = listenAddr

	if clusterRepo != nil {
		coordinator = cluster.NewCoordinator(clusterRepo, cluster.NodeIdentity{
			IP:        clusterClientAddr,
			Port:      listenPort,
			StartedAt: clusterStartedAt,
		}, cluster.CoordinatorOptions{
			HeartbeatInterval: clusterCfg.Node.HeartbeatInterval,
			HeartbeatTimeout:  clusterCfg.Node.HeartbeatTimeout,
		})
		refreshController := cluster.NewRefreshController(coordinator, rt, clusterRepo)
		coordinator.SetOnMasterChanged(refreshController.OnMasterChanged)
		rt.SetClusterRefreshHandler(refreshController.RefreshNow)
		clusterRESPHandler = cluster.NewRESPHandler(coordinator, refreshController, clusterRepo)
		if clusterAdapter == nil {
			log.Errorf("failed to init cluster runtime adapter")
			return 1
		}
	}

	if errStart := rt.Start(runCtx, configPath); errStart != nil {
		log.Errorf("failed to start runtime: %v", errStart)
		return 1
	}

	respSrv := respserver.New(addr, rt)
	respSrv.SetClusterHandler(clusterRESPHandler)

	cfgPath := strings.TrimSpace(rt.ConfigPath())
	if cfgPath == "" {
		cfgPath = strings.TrimSpace(configPath)
	}
	mgmtOpts := make([]managementhttp.RouteOption, 0, 1)
	if clusterRepo != nil {
		mgmtOpts = append(mgmtOpts, managementhttp.WithClusterManagement(managementhttp.ClusterManagementOption{
			Enabled:    true,
			Repository: clusterRepo,
			Runtime:    rt,
		}))
	}
	mgmtBuild, errMgmt := managementhttp.Build(cfgPath, mgmtOpts...)
	if errMgmt != nil {
		log.Errorf("failed to init management http: %v", errMgmt)
		return 1
	}

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           mgmtBuild.Engine,
		ReadHeaderTimeout: 5 * time.Second,
	}

	baseListener, errListen := net.Listen("tcp", addr)
	if errListen != nil {
		log.Errorf("failed to listen on %s: %v", addr, errListen)
		return 1
	}

	if cfg.TLS.Enable {
		certPath := strings.TrimSpace(cfg.TLS.Cert)
		keyPath := strings.TrimSpace(cfg.TLS.Key)
		if certPath == "" || keyPath == "" {
			log.Errorf("failed to start HTTPS server: tls.cert or tls.key is empty")
			_ = baseListener.Close()
			return 1
		}
		certPair, errLoad := tls.LoadX509KeyPair(certPath, keyPath)
		if errLoad != nil {
			log.Errorf("failed to start HTTPS server: %v", errLoad)
			_ = baseListener.Close()
			return 1
		}
		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{certPair},
			NextProtos:   []string{"h2", "http/1.1"},
		}
		httpSrv.TLSConfig = tlsConfig
		if errHTTP2 := http2.ConfigureServer(httpSrv, &http2.Server{}); errHTTP2 != nil {
			log.Warnf("failed to configure HTTP/2: %v", errHTTP2)
		}
		baseListener = tls.NewListener(baseListener, tlsConfig)
	}

	httpListener := protocolmux.NewListener(baseListener.Addr(), 1024)
	var shutdownOnce sync.Once
	shutdownServers := func() {
		shutdownOnce.Do(func() {
			cancelRun()
			_ = httpListener.Close()
			_ = baseListener.Close()
			shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelShutdown()
			_ = httpSrv.Shutdown(shutdownCtx)
		})
	}

	httpErrCh := make(chan error, 1)
	acceptErrCh := make(chan error, 1)

	if coordinator != nil {
		errCh := make(chan error, 1)
		coordinatorErrCh = errCh
		go func() {
			if errCoordinator := coordinator.Start(runCtx); errCoordinator != nil {
				errCh <- errCoordinator
			}
		}()
	}
	if eventWatcher != nil {
		errCh := make(chan error, 1)
		eventWatcherErrCh = errCh
		go func() {
			if errWatcher := eventWatcher.Start(runCtx); errWatcher != nil {
				errCh <- errWatcher
			}
		}()
	}

	go func() {
		httpErrCh <- httpSrv.Serve(httpListener)
	}()
	go func() {
		acceptErrCh <- protocolmux.Serve(runCtx, baseListener, httpListener, func(conn net.Conn) {
			go respSrv.HandleConn(runCtx, conn)
		}, nil)
	}()

	go func() {
		<-ctx.Done()
		shutdownServers()
	}()

	select {
	case errServe := <-httpErrCh:
		errServe = protocolmux.NormalizeServeError(errServe)
		shutdownServers()
		errAccept := collectServeError(acceptErrCh, 2*time.Second)
		if errServe != nil {
			log.Errorf("http server error: %v", errServe)
			return 1
		}
		if errAccept != nil {
			log.Errorf("listener accept error: %v", errAccept)
			return 1
		}
		return 0
	case errAccept := <-acceptErrCh:
		errAccept = protocolmux.NormalizeServeError(errAccept)
		shutdownServers()
		errServe := collectServeError(httpErrCh, 2*time.Second)
		if errAccept != nil {
			log.Errorf("listener accept error: %v", errAccept)
			return 1
		}
		if errServe != nil {
			log.Errorf("http server error: %v", errServe)
			return 1
		}
		return 0
	case errCoordinator := <-coordinatorErrCh:
		shutdownServers()
		errServe := collectServeError(httpErrCh, 2*time.Second)
		errAccept := collectServeError(acceptErrCh, 2*time.Second)
		if errCoordinator != nil {
			log.Errorf("cluster coordinator error: %v", errCoordinator)
			return 1
		}
		if errServe != nil {
			log.Errorf("http server error: %v", errServe)
			return 1
		}
		if errAccept != nil {
			log.Errorf("listener accept error: %v", errAccept)
			return 1
		}
		return 0
	case errWatcher := <-eventWatcherErrCh:
		shutdownServers()
		errServe := collectServeError(httpErrCh, 2*time.Second)
		errAccept := collectServeError(acceptErrCh, 2*time.Second)
		if errWatcher != nil {
			log.Errorf("cluster event watcher error: %v", errWatcher)
			return 1
		}
		if errServe != nil {
			log.Errorf("http server error: %v", errServe)
			return 1
		}
		if errAccept != nil {
			log.Errorf("listener accept error: %v", errAccept)
			return 1
		}
		return 0
	}
}

// resolveListenAddress resolves a listen address.
func resolveListenAddress(addr string, cfg *config.Config, clusterCfg *cluster.Config, clusterEnabled bool) (string, int, error) {
	// Keep validation before state changes so failures leave existing data intact.
	addr = strings.TrimSpace(addr)
	if addr == "" {
		host := ""
		port := 0
		if cfg != nil {
			host = strings.TrimSpace(cfg.Host)
			port = cfg.Port
		}
		if clusterEnabled {
			if clusterCfg == nil {
				return "", 0, fmt.Errorf("cluster config is nil")
			}
			port = clusterCfg.Node.Port
		}
		if host == "" {
			addr = ":" + strconv.Itoa(port)
		} else {
			addr = fmt.Sprintf("%s:%d", host, port)
		}
	}

	port, errPort := listenPortFromAddress(addr)
	if clusterEnabled {
		if errPort != nil {
			return "", 0, errPort
		}
		if port <= 0 {
			return "", 0, fmt.Errorf("cluster listen port must be greater than 0")
		}
	}
	return addr, port, nil
}

// listenPortFromAddress derives listen port from address.
func listenPortFromAddress(addr string) (int, error) {
	_, portValue, errSplitHostPort := net.SplitHostPort(addr)
	if errSplitHostPort != nil {
		return 0, errSplitHostPort
	}
	port, errPort := strconv.Atoi(strings.TrimSpace(portValue))
	if errPort != nil {
		return 0, errPort
	}
	return port, nil
}

// collectServeError handles a collect serve error.
func collectServeError(ch <-chan error, timeout time.Duration) error {
	if ch == nil {
		return nil
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case errServe := <-ch:
		return protocolmux.NormalizeServeError(errServe)
	case <-timer.C:
		return nil
	}
}

// runDisbandCluster runs a disband cluster.
func runDisbandCluster(ctx context.Context, clusterCfg *cluster.Config, configPath string) int {
	// Keep validation before state changes so failures leave existing data intact.
	if clusterCfg == nil {
		log.Errorf("failed to disband cluster: cluster config is nil")
		return 1
	}
	clusterDB, errClusterOpen := cluster.Open(ctx, clusterCfg.PGSQL)
	if errClusterOpen != nil {
		log.Errorf("failed to open cluster database: %v", errClusterOpen)
		return 1
	}
	sqlDB, errSQLDB := clusterDB.DB()
	if errSQLDB != nil {
		log.Errorf("failed to get cluster sql db: %v", errSQLDB)
		return 1
	}
	defer func() {
		if errCloseSQL := sqlDB.Close(); errCloseSQL != nil {
			log.Warnf("failed to close cluster sql db: %v", errCloseSQL)
		}
	}()
	if errMigrate := cluster.AutoMigrate(clusterDB); errMigrate != nil {
		log.Errorf("failed to migrate cluster database: %v", errMigrate)
		return 1
	}

	result, errDisband := cluster.Disband(ctx, cluster.DisbandOptions{
		ConfigPath: configPath,
		Repository: cluster.NewRepository(clusterDB),
	})
	if errDisband != nil {
		log.Errorf("failed to disband cluster: %v", errDisband)
		return 1
	}
	clusterConfigBackup, errBackupClusterConfig := backupClusterConfig()
	if errBackupClusterConfig != nil {
		log.Errorf("failed to backup cluster config: %v", errBackupClusterConfig)
		return 1
	}
	log.Infof(
		"cluster disband restored config=%s bytes=%d auth_dir=%s auth_files=%d gemini_keys=%d vertex_keys=%d codex_keys=%d claude_keys=%d openai_compatibility=%d cluster_config_backup=%s",
		result.ConfigPath,
		result.ConfigBytes,
		result.AuthDir,
		result.AuthFiles,
		result.GeminiKeys,
		result.VertexKeys,
		result.CodexKeys,
		result.ClaudeKeys,
		result.OpenAICompatibility,
		clusterConfigBackup,
	)
	return 0
}

// backupClusterConfig handles a backup cluster config.
func backupClusterConfig() (string, error) {
	// Normalize source data before building the derived payload.
	sourcePath := cluster.DefaultConfigPath
	info, errStat := os.Stat(sourcePath)
	if os.IsNotExist(errStat) {
		return "", nil
	}
	if errStat != nil {
		return "", errStat
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", sourcePath)
	}
	backupPath := fmt.Sprintf("%s.bak-%s", sourcePath, time.Now().Format("2006-01-02"))
	if _, errBackupStat := os.Stat(backupPath); errBackupStat == nil {
		return "", fmt.Errorf("backup file already exists: %s", backupPath)
	} else if !os.IsNotExist(errBackupStat) {
		return "", errBackupStat
	}
	if errRename := os.Rename(sourcePath, backupPath); errRename != nil {
		return "", errRename
	}
	return backupPath, nil
}

// applyLogLevel applies a log level.
func applyLogLevel(cfg *config.Config) {
	currentLevel := log.GetLevel()
	debugEnabled := cfg != nil && cfg.Debug
	if debugEnabled {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}
	if nextLevel := log.GetLevel(); nextLevel != currentLevel {
		log.Infof("log level changed from %s to %s (debug=%t)", currentLevel, nextLevel, debugEnabled)
	}
}

// loadLocalConfigForCluster loads a local config for cluster.
func loadLocalConfigForCluster(configPath string) (*config.Config, error) {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		configPath = "config.yaml"
	}
	info, errStat := os.Stat(configPath)
	if os.IsNotExist(errStat) {
		return nil, nil
	}
	if errStat != nil {
		return nil, errStat
	}
	if info.IsDir() {
		return nil, fmt.Errorf("config path is a directory")
	}
	return config.LoadConfigOptional(configPath, false)
}
