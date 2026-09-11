// Command grant-store serves the grant registry on 127.0.0.1:8315.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	grantstore "github.com/kayushkin/grant-store"
)

func main() {
	addr := os.Getenv("GRANT_STORE_ADDR")
	if addr == "" {
		// Loopback, deliberately, like principal-store. This service has no
		// auth of its own; dash is the front door that adds it. A wildcard bind
		// would put grant editing on the network for anything that can route
		// to this host.
		addr = "127.0.0.1:8315"
	}
	dataDir := os.Getenv("GRANT_STORE_DATA_DIR")

	store, err := grantstore.Open(dataDir)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer store.Close()

	// The owners a grant is checked against before it is written: the
	// principal with principal-store, the resource with whichever store
	// assigned its id.
	principalStoreURL := grantstore.PrincipalStoreURL()
	llmBridgeServerURL := grantstore.LLMBridgeServerURL()
	skillStoreURL := grantstore.SkillStoreURL()
	toolStoreURL := grantstore.ToolStoreURL()
	directory := grantstore.NewHTTPPrincipalDirectory(principalStoreURL)
	checker := grantstore.NewHTTPResourceChecker(llmBridgeServerURL, skillStoreURL, toolStoreURL)
	log.Printf("owners: principal-store=%s llm-bridge-server=%s skill-store=%s tool-store=%s",
		principalStoreURL, llmBridgeServerURL, skillStoreURL, toolStoreURL)

	mux := http.NewServeMux()
	grantstore.RegisterHandlers(mux, store, directory, checker)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("grant-store listening on %s (data=%s)", addr, store.DataDir())
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("shutting down…")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
