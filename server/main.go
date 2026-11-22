package main

import (
	"context"
	"embed"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"frmaeo/internal/api"
	"frmaeo/internal/mdns"
	"frmaeo/internal/store"
)

//go:embed all:web
var webFS embed.FS

func main() {
	defaultAddr := envOr("FRMAEO_ADDR", ":8080")
	defaultData := envOr("FRMAEO_DATA", "./data")
	defaultToken := os.Getenv("FRMAEO_TOKEN")
	defaultMdnsHost := envOr("FRMAEO_MDNS_HOST", "frame")

	addr := flag.String("addr", defaultAddr, "HTTP listen address")
	dataDir := flag.String("data", defaultData, "Data directory for photos and metadata")
	token := flag.String("token", defaultToken, "Optional shared family token override")
	mdnsHost := flag.String("mdns-host", defaultMdnsHost, "mDNS hostname to advertise (empty to disable)")
	flag.Parse()

	st, err := store.Open(*dataDir, *token)
	if err != nil {
		log.Fatalf("failed to open store at %s: %v", *dataDir, err)
	}

	port := api.ParsePort(*addr)
	srv := api.NewServer(st, webFS, port)

	mdnsCtx, mdnsCancel := context.WithCancel(context.Background())
	defer mdnsCancel()
	if *mdnsHost != "" {
		go mdns.New(*mdnsHost, port).Run(mdnsCtx)
	}

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
	}

	go func() {
		log.Printf("frmaeo %s listening on %s (data: %s, token: %s)", api.Version, *addr, *dataDir, st.GetSettings().FamilyToken)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server error: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
}

func envOr(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
