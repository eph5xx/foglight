package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/eph5xx/foglight/pkg/gateway"
)

func main() {
	path := flag.String("config", defaultConfigPath(), "path to config file")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg, err := gateway.LoadConfig(*path)
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	if err := gateway.Run(ctx, cfg); err != nil {
		log.Fatalf("run error: %v", err)
	}
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".foglight", "config.yaml")
	}
	return filepath.Join(home, ".foglight", "config.yaml")
}
