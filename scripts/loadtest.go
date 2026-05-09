//go:build ignore

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const (
	baseURL     = "http://localhost:8080"
	totalEvents = 10_000
	concurrency = 50
)

func main() {
	host := os.Getenv("TARGET_URL")
	if host == "" {
		host = baseURL
	}

	// Get or create a creator to use
	creatorID := getOrCreateCreator(host)
	fmt.Printf("load target creator: %s\n", creatorID)
	fmt.Printf("firing %d events with %d concurrent workers...\n\n", totalEvents, concurrency)

	client := &http.Client{Timeout: 10 * time.Second}

	var (
		success  atomic.Int64
		failures atomic.Int64
		backpres atomic.Int64
	)
	latencies := make([]time.Duration, 0, totalEvents)
	var mu sync.Mutex

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	start := time.Now()

	for i := 0; i < totalEvents; i++ {
		sem <- struct{}{}
		wg.Add(1)
		go func(n int) {
			defer func() { <-sem; wg.Done() }()

			t0 := time.Now()
			code := fireEvent(client, host, creatorID, n)
			dur := time.Since(t0)

			mu.Lock()
			latencies = append(latencies, dur)
			mu.Unlock()

			switch {
			case code == 202:
				success.Add(1)
			case code == 503:
				backpres.Add(1)
			default:
				failures.Add(1)
			}
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p50 := latencies[len(latencies)/2]
	p99 := latencies[int(float64(len(latencies))*0.99)]

	rps := float64(totalEvents) / elapsed.Seconds()
	fmt.Printf("results\n")
	fmt.Printf("  total events   : %d\n", totalEvents)
	fmt.Printf("  elapsed        : %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("  throughput     : %.0f events/sec\n", rps)
	fmt.Printf("  accepted (202) : %d\n", success.Load())
	fmt.Printf("  backpressure   : %d\n", backpres.Load())
	fmt.Printf("  failures       : %d\n", failures.Load())
	fmt.Printf("  p50 latency    : %s\n", p50.Round(time.Microsecond))
	fmt.Printf("  p99 latency    : %s\n", p99.Round(time.Microsecond))
}

func fireEvent(client *http.Client, host, creatorID string, n int) int {
	var (
		path string
		body any
	)

	switch n % 3 {
	case 0:
		path = "/creators/" + creatorID + "/subscribe"
		body = map[string]int{"tier": rand.Intn(3) + 1}
	case 1:
		path = "/creators/" + creatorID + "/gift"
		body = map[string]int{"quantity": rand.Intn(10) + 1}
	default:
		path = "/creators/" + creatorID + "/tip"
		body = map[string]int64{"amount_cents": int64(rand.Intn(9999) + 1)}
	}

	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, host+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Viewer-Id", fmt.Sprintf("viewer-%d", rand.Intn(1000)))

	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	resp.Body.Close()
	return resp.StatusCode
}

func getOrCreateCreator(host string) string {
	body, _ := json.Marshal(map[string]string{"display_name": "loadtest_creator"})
	req, _ := http.NewRequest(http.MethodPost, host+"/creators", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("create creator: %v", err)
	}
	defer resp.Body.Close()

	var env struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		log.Fatalf("decode creator: %v", err)
	}
	if env.Data.ID == "" {
		log.Fatal("empty creator ID from server")
	}
	return env.Data.ID
}

func init() {
	// suppress unused import error for os
	_ = os.Stderr
}
