// hello-service is a minimal example of an Anito-compliant service.
//
// It satisfies the three-rule service contract:
//   1. Reads PORT from the environment
//   2. Serves HTTP on that port
//   3. Exposes GET /health → 200 OK
//
// Deploy it:
//
//	anito deploy
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080" // fallback for running outside Anito
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleRoot)
	mux.HandleFunc("/health", handleHealth)

	addr := ":" + port
	log.Printf("hello-service listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	hostname, _ := os.Hostname()
	writeJSON(w, map[string]any{
		"service": "hello-service",
		"host":    hostname,
		"time":    time.Now().Format(time.RFC3339),
		"message": "anito is watching over me",
	})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		http.Error(w, fmt.Sprintf("encode error: %v", err), http.StatusInternalServerError)
	}
}
