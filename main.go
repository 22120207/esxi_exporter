package main

import (
	"esxi_exporter/internal/metrics"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	port := "10424"

	// Hours to rescan
	rescanInterval := 24 // 1 days

	// Create PercMetrics instance and run it
	pm := metrics.NewMetrics()

	// Run metrics collection in a goroutine
	go func() {
		ticker := time.NewTicker(time.Duration(rescanInterval) * time.Hour)
		defer ticker.Stop()

		for runtime := range ticker.C {
			log.Printf("Rescan metrics at: %v", runtime)
			pm.CollectMetrics()
		}
	}()

	// Set up the /metrics endpoint
	http.Handle("/metrics", promhttp.Handler())
	log.Printf("Starting server on port %s", port)
	if err := http.ListenAndServe("0.0.0.0:"+port, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
