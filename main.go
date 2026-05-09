package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/anovimous/masser/internal/hit"
	"github.com/anovimous/masser/internal/httpx"
	"github.com/anovimous/masser/internal/logger"
	"github.com/anovimous/masser/internal/matcher"
	"github.com/anovimous/masser/internal/output"
	"github.com/anovimous/masser/internal/request"
	"github.com/anovimous/masser/internal/tester"
	"github.com/anovimous/masser/internal/wordlist"

	"github.com/anovimous/masser/internal/config"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Parse()
	if err != nil {
		return err
	}

	log, err := logger.New(cfg.LogPath)
	if err != nil {
		return fmt.Errorf("logger: %w", err)
	}
	defer log.Close()

	// Wordlists.
	words, err := wordlist.Load(cfg.WordlistPath, cfg.CustomPath)
	if err != nil {
		return fmt.Errorf("wordlist: %w", err)
	}
	if len(words) == 0 {
		return fmt.Errorf("wordlist is empty")
	}
	log.Info("loaded %d unique words", len(words))

	// Requests.
	var requests []*request.ParsedRequest
	if cfg.RequestFile != "" {
		r, err := request.LoadSingle(cfg.RequestFile)
		if err != nil {
			return fmt.Errorf("load request: %w", err)
		}
		requests = []*request.ParsedRequest{r}
	} else {
		rs, err := request.LoadMulti(cfg.RequestsFile)
		if err != nil {
			return fmt.Errorf("load requests: %w", err)
		}
		requests = rs
	}
	log.Info("loaded %d request(s)", len(requests))

	// HTTP clients.
	client, err := httpx.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("client: %w", err)
	}
	replayClient, err := httpx.NewReplayClient(cfg)
	if err != nil {
		return fmt.Errorf("replay client: %w", err)
	}

	// Matcher / hit engines.
	mEngine, err := matcher.New(cfg)
	if err != nil {
		return err
	}
	hEngine, err := hit.New(cfg)
	if err != nil {
		return err
	}

	t := tester.New(cfg, client, replayClient, mEngine, hEngine, log)

	// Cancellable context for clean shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Warn("interrupt received, cancelling")
		cancel()
	}()

	// Run sequentially across requests; concurrency could be added later as a
	// flag, but for now staying within the rate limit is the priority and the
	// limiter inside the client already serialises calls.
	results := make([]tester.Result, 0, len(requests))
	for _, req := range requests {
		select {
		case <-ctx.Done():
			break
		default:
		}
		results = append(results, t.Run(ctx, req, words))
	}

	if err := output.Write(cfg, results); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}
