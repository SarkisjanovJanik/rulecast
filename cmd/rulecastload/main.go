package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type request struct {
	FlagKey    string         `json:"flagKey"`
	Attributes map[string]any `json:"attributes"`
}

type result struct {
	Variation string `json:"variation"`
	Reason    string `json:"reason"`
	RuleID    string `json:"ruleId"`
}

type counters struct {
	mu       sync.Mutex
	byResult map[string]int64
	failures atomic.Int64
	sent     atomic.Int64
}

func main() {
	var (
		baseURL = flag.String("url", "http://localhost:8090", "service base URL")
		key     = flag.String("key", "sdk-local-key", "SDK API key")
		flagKey = flag.String("flag", "pricing-banner-copy", "flag to evaluate")
		users   = flag.Int("users", 5000, "distinct users to simulate")
		workers = flag.Int("workers", 16, "concurrent workers")
	)
	flag.Parse()

	if err := run(*baseURL, *key, *flagKey, *users, *workers); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(baseURL, key, flagKey string, users, workers int) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        workers * 2,
			MaxIdleConnsPerHost: workers * 2,
		},
	}

	stats := &counters{byResult: map[string]int64{}}
	queue := make(chan int, workers*4)
	started := time.Now()

	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for id := range queue {
				if ctx.Err() != nil {
					return
				}
				evaluate(ctx, client, baseURL, key, flagKey, id, stats)
			}
		}()
	}

	for id := range users {
		select {
		case queue <- id:
		case <-ctx.Done():
		}
	}
	close(queue)
	group.Wait()

	report(stats, flagKey, time.Since(started))
	if stats.failures.Load() > 0 {
		return fmt.Errorf("%d requests failed", stats.failures.Load())
	}
	return nil
}

func evaluate(
	ctx context.Context,
	client *http.Client,
	baseURL, key, flagKey string,
	id int,
	stats *counters,
) {
	body, err := json.Marshal(request{
		FlagKey:    flagKey,
		Attributes: attributes(id),
	})
	if err != nil {
		stats.failures.Add(1)
		return
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/api/v1/sdk/evaluate", bytes.NewReader(body))
	if err != nil {
		stats.failures.Add(1)
		return
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("X-API-Key", key)

	response, err := client.Do(httpRequest)
	if err != nil {
		stats.failures.Add(1)
		return
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		stats.failures.Add(1)
		return
	}

	var decoded result
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		stats.failures.Add(1)
		return
	}

	stats.sent.Add(1)
	label := decoded.Reason
	if decoded.Variation != "" {
		label = decoded.Reason + " → " + decoded.Variation
	}

	stats.mu.Lock()
	stats.byResult[label]++
	stats.mu.Unlock()
}

func attributes(id int) map[string]any {
	countries := []string{"DE", "AT", "CH", "PL", "NL", "FI", "SE", "GB", "BR", "IN"}
	tiers := []string{"free", "pro", "vip", "enterprise"}
	languages := []string{"en", "de", "pl", "pt"}
	systems := []string{"ios", "android", "web"}
	versions := []string{"2.9.0", "3.4.1", "4.12.0", "5.0.0"}

	source := rand.New(rand.NewPCG(uint64(id), 0x9E3779B97F4A7C15))

	return map[string]any{
		"user.key":      fmt.Sprintf("user-%06d", id),
		"user.country":  countries[source.IntN(len(countries))],
		"user.tier":     tiers[source.IntN(len(tiers))],
		"user.language": languages[source.IntN(len(languages))],
		"user.spend":    float64(source.IntN(2000)),
		"user.age":      float64(18 + source.IntN(50)),
		"user.beta":     source.IntN(100) < 5,
		"device.os":     systems[source.IntN(len(systems))],
		"app.version":   versions[source.IntN(len(versions))],
	}
}

func report(stats *counters, flagKey string, elapsed time.Duration) {
	stats.mu.Lock()
	defer stats.mu.Unlock()

	sent := stats.sent.Load()
	fmt.Printf("flag %s: %d evaluations in %s", flagKey, sent, elapsed.Round(time.Millisecond))
	if elapsed > 0 {
		fmt.Printf(" (%.0f/s)", float64(sent)/elapsed.Seconds())
	}
	fmt.Println()

	if failures := stats.failures.Load(); failures > 0 {
		fmt.Printf("  failures: %d\n", failures)
	}

	for label, count := range stats.byResult {
		share := 0.0
		if sent > 0 {
			share = float64(count) / float64(sent) * 100
		}
		fmt.Printf("  %-28s %7d  %5.1f%%\n", label, count, share)
	}
}
