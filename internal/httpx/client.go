package httpx

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"github.com/anovimous/masser/internal/config"
	"github.com/anovimous/masser/internal/request"
)

// Client wraps a rate-limited *http.Client with a baked-in protocol setting
// and helpers for converting our internal request representation into a real
// HTTP round-trip.
type Client struct {
	cli     *http.Client
	limiter *rate.Limiter
	proto   string
	headers []header
	cookies []string // already in "name=value" form
}

type header struct{ k, v string }

func NewClient(cfg *config.Config) (*Client, error) {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	if cfg.Proxy != "" {
		u, err := url.Parse(cfg.Proxy)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy: %w", err)
		}
		tr.Proxy = http.ProxyURL(u)
	}

	cli := &http.Client{
		Transport: tr,
		Timeout:   30 * time.Second,
		// Don't follow redirects.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	c := &Client{
		cli:     cli,
		limiter: rate.NewLimiter(rate.Limit(cfg.Rate), cfg.Rate),
		proto:   cfg.RequestProto,
	}

	for _, h := range cfg.Headers {
		i := strings.Index(h, ":")
		if i < 0 {
			return nil, fmt.Errorf("invalid -H value %q (expected 'Name: value')", h)
		}
		c.headers = append(c.headers, header{
			k: strings.TrimSpace(h[:i]),
			v: strings.TrimSpace(h[i+1:]),
		})
	}
	for _, ck := range cfg.Cookies {
		ck = strings.TrimSpace(ck)
		if ck == "" {
			continue
		}
		c.cookies = append(c.cookies, ck)
	}

	return c, nil
}

// ReplayClient builds a second client (no rate limiter) that routes through
// the replay proxy. Returns nil if -replay-proxy was not set.
func NewReplayClient(cfg *config.Config) (*Client, error) {
	if cfg.ReplayProxy == "" {
		return nil, nil
	}

	u, err := url.Parse(cfg.ReplayProxy)
	if err != nil {
		return nil, fmt.Errorf("invalid replay proxy: %w", err)
	}
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		Proxy:           http.ProxyURL(u),
	}
	cli := &http.Client{
		Transport: tr,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	c := &Client{
		cli:     cli,
		limiter: rate.NewLimiter(rate.Inf, 1),
		proto:   cfg.RequestProto,
	}
	for _, h := range cfg.Headers {
		i := strings.Index(h, ":")
		if i < 0 {
			continue
		}
		c.headers = append(c.headers, header{
			k: strings.TrimSpace(h[:i]),
			v: strings.TrimSpace(h[i+1:]),
		})
	}
	for _, ck := range cfg.Cookies {
		c.cookies = append(c.cookies, strings.TrimSpace(ck))
	}
	return c, nil
}

// Do sends the parsed request, applying global headers/cookies, and returns
// the status code and body.
func (c *Client) Do(ctx context.Context, p *request.ParsedRequest) (int, []byte, error) {
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx); err != nil {
			return 0, nil, err
		}
	}

	req, err := c.toHTTPRequest(ctx, p)
	if err != nil {
		return 0, nil, err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, body, err
	}
	return resp.StatusCode, body, nil
}

func (c *Client) toHTTPRequest(ctx context.Context, p *request.ParsedRequest) (*http.Request, error) {
	host := p.Host
	if host == "" {
		return nil, fmt.Errorf("request has no Host header")
	}
	target := fmt.Sprintf("%s://%s%s", c.proto, host, p.Path)

	var bodyReader io.Reader
	if len(p.Body) > 0 {
		bodyReader = bytes.NewReader(p.Body)
	}
	req, err := http.NewRequestWithContext(ctx, p.Method, target, bodyReader)
	if err != nil {
		return nil, err
	}

	// Copy headers from parsed request.
	for k, vs := range p.Headers {
		// Host header is set on req.Host directly by net/http.
		if strings.EqualFold(k, "Host") {
			req.Host = vs[0]
			continue
		}
		// We rebuild Cookie below to ensure merging.
		if strings.EqualFold(k, "Cookie") {
			continue
		}
		// Content-Length is recomputed by net/http.
		if strings.EqualFold(k, "Content-Length") {
			continue
		}
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	// Apply -H additions (overriding originals if same name).
	for _, h := range c.headers {
		req.Header.Set(h.k, h.v)
	}

	// Merge cookies from original request and -C flag.
	merged := mergeCookies(p.Headers.Get("Cookie"), c.cookies)
	if merged != "" {
		req.Header.Set("Cookie", merged)
	}

	// Force Content-Length based on actual body.
	if len(p.Body) > 0 {
		req.ContentLength = int64(len(p.Body))
	}

	return req, nil
}

func mergeCookies(orig string, extra []string) string {
	seen := map[string]string{}
	order := []string{}

	add := func(name, value string) {
		if _, ok := seen[name]; !ok {
			order = append(order, name)
		}
		seen[name] = value
	}

	for _, part := range strings.Split(orig, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		i := strings.Index(part, "=")
		if i < 0 {
			add(part, "")
		} else {
			add(part[:i], part[i+1:])
		}
	}
	for _, e := range extra {
		i := strings.Index(e, "=")
		if i < 0 {
			add(e, "")
		} else {
			add(e[:i], e[i+1:])
		}
	}

	parts := make([]string, 0, len(order))
	for _, n := range order {
		v := seen[n]
		if v == "" {
			parts = append(parts, n)
		} else {
			parts = append(parts, n+"="+v)
		}
	}
	return strings.Join(parts, "; ")
}
