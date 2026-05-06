package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/router-for-me/CLIProxyAPIHome/internal/config"
	"github.com/router-for-me/CLIProxyAPIHome/internal/home"
	"github.com/router-for-me/CLIProxyAPIHome/internal/logging"
	"github.com/router-for-me/CLIProxyAPIHome/internal/respserver"
	log "github.com/sirupsen/logrus"
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

	server := respserver.New(addr, rt)
	if errServe := server.ListenAndServe(ctx); errServe != nil && errServe != context.Canceled {
		log.Errorf("resp server error: %v", errServe)
		os.Exit(1)
	}
}
