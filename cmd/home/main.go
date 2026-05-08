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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/router-for-me/CLIProxyAPIHome/internal/config"
	"github.com/router-for-me/CLIProxyAPIHome/internal/home"
	"github.com/router-for-me/CLIProxyAPIHome/internal/logging"
	"github.com/router-for-me/CLIProxyAPIHome/internal/managementhttp"
	"github.com/router-for-me/CLIProxyAPIHome/internal/protocolmux"
	"github.com/router-for-me/CLIProxyAPIHome/internal/respserver"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/http2"
)

func main() {
	logging.SetupBaseLogger()

	var configPath string
	var addr string
	flag.StringVar(&configPath, "config", "config.yaml", "Config file path")
	flag.StringVar(&addr, "addr", "", "Override RESP listen address (host:port)")
	flag.Parse()

	cfg, errLoad := config.LoadConfigOptional(configPath, false)
	if errLoad != nil {
		log.Errorf("failed to load config %s: %v", configPath, errLoad)
		os.Exit(1)
	}

	currentLevel := log.GetLevel()
	if cfg.Debug {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}
	if nextLevel := log.GetLevel(); nextLevel != currentLevel {
		log.Infof("log level changed from %s to %s (debug=%t)", currentLevel, nextLevel, cfg.Debug)
	}

	rt, errRuntime := home.NewRuntime(cfg)
	if errRuntime != nil {
		log.Errorf("failed to init runtime: %v", errRuntime)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	defer rt.Stop()

	if errStart := rt.Start(ctx, configPath); errStart != nil {
		log.Errorf("failed to start runtime: %v", errStart)
		os.Exit(1)
	}

	if strings.TrimSpace(addr) == "" {
		host := strings.TrimSpace(cfg.Host)
		port := cfg.Port
		if host == "" {
			addr = ":" + strconv.Itoa(port)
		} else {
			addr = fmt.Sprintf("%s:%d", host, port)
		}
	}

	respSrv := respserver.New(addr, rt)

	cfgPath := strings.TrimSpace(rt.ConfigPath())
	if cfgPath == "" {
		cfgPath = strings.TrimSpace(configPath)
	}
	mgmtBuild, errMgmt := managementhttp.Build(cfgPath)
	if errMgmt != nil {
		log.Errorf("failed to init management http: %v", errMgmt)
		os.Exit(1)
	}

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           mgmtBuild.Engine,
		ReadHeaderTimeout: 5 * time.Second,
	}

	baseListener, errListen := net.Listen("tcp", addr)
	if errListen != nil {
		log.Errorf("failed to listen on %s: %v", addr, errListen)
		os.Exit(1)
	}

	if cfg.TLS.Enable {
		certPath := strings.TrimSpace(cfg.TLS.Cert)
		keyPath := strings.TrimSpace(cfg.TLS.Key)
		if certPath == "" || keyPath == "" {
			log.Errorf("failed to start HTTPS server: tls.cert or tls.key is empty")
			_ = baseListener.Close()
			os.Exit(1)
		}
		certPair, errLoad := tls.LoadX509KeyPair(certPath, keyPath)
		if errLoad != nil {
			log.Errorf("failed to start HTTPS server: %v", errLoad)
			_ = baseListener.Close()
			os.Exit(1)
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

	httpErrCh := make(chan error, 1)
	acceptErrCh := make(chan error, 1)

	go func() {
		httpErrCh <- httpSrv.Serve(httpListener)
	}()
	go func() {
		acceptErrCh <- protocolmux.Serve(ctx, baseListener, httpListener, func(conn net.Conn) {
			go respSrv.HandleConn(ctx, conn)
		}, nil)
	}()

	go func() {
		<-ctx.Done()
		_ = httpListener.Close()
		_ = baseListener.Close()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	select {
	case errServe := <-httpErrCh:
		errServe = protocolmux.NormalizeServeError(errServe)
		errAccept := <-acceptErrCh
		errAccept = protocolmux.NormalizeServeError(errAccept)
		if errServe != nil {
			log.Errorf("http server error: %v", errServe)
			os.Exit(1)
		}
		if errAccept != nil {
			log.Errorf("listener accept error: %v", errAccept)
			os.Exit(1)
		}
		return
	case errAccept := <-acceptErrCh:
		errAccept = protocolmux.NormalizeServeError(errAccept)
		errServe := <-httpErrCh
		errServe = protocolmux.NormalizeServeError(errServe)
		if errAccept != nil {
			log.Errorf("listener accept error: %v", errAccept)
			os.Exit(1)
		}
		if errServe != nil {
			log.Errorf("http server error: %v", errServe)
			os.Exit(1)
		}
		return
	}
}
