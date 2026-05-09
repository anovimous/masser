package config

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

type Verbosity int

const (
	VerbositySummarized Verbosity = iota
	VerbosityNormal
	VerbosityVerbose
)

func ParseVerbosity(s string) (Verbosity, error) {
	switch strings.ToUpper(s) {
	case "SUMMARIZED":
		return VerbositySummarized, nil
	case "NORMAL":
		return VerbosityNormal, nil
	case "VERBOSE":
		return VerbosityVerbose, nil
	default:
		return 0, fmt.Errorf("invalid verbosity: %s (must be SUMMARIZED, NORMAL, or VERBOSE)", s)
	}
}

type Mode int

const (
	ModeOr Mode = iota
	ModeAnd
)

func ParseMode(s string) (Mode, error) {
	switch strings.ToLower(s) {
	case "or":
		return ModeOr, nil
	case "and":
		return ModeAnd, nil
	default:
		return 0, fmt.Errorf("invalid mode: %s (must be 'and' or 'or')", s)
	}
}

// stringSlice supports a flag that may be specified multiple times.
type stringSlice []string

func (s *stringSlice) String() string     { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error { *s = append(*s, v); return nil }

type Config struct {
	// Input
	RequestFile  string
	RequestsFile string

	// Wordlists
	WordlistPath string
	CustomPath   string

	// Injection
	N         int
	Rate      int
	ValueRaw  string
	ValueJSON interface{}

	// Output / logging
	OutputPath string
	LogPath    string
	Verbosity  Verbosity

	// Headers / cookies
	Headers []string
	Cookies []string

	// Proto / proxies
	RequestProto string
	Proxy        string
	ReplayProxy  string

	// Matcher
	MC    string
	ML    string
	MR    string
	MS    string
	MW    string
	MMode Mode

	// Filter
	FC    string
	FL    string
	FR    string
	FS    string
	FW    string
	FMode Mode

	// Hit
	HC    bool
	HS    bool
	HL    bool
	HR    string
	HRF   bool
	HMode Mode
}

func Parse() (*Config, error) {
	cfg := &Config{}

	var (
		headers      stringSlice
		cookies      stringSlice
		verbosityStr string
		mModeStr     string
		fModeStr     string
		hModeStr     string
		nFlag        int
		mcDefault    = "200-299,301,302,307,401,403,405,500"
	)

	fs := flag.NewFlagSet("massassign", flag.ContinueOnError)

	fs.StringVar(&cfg.RequestFile, "request", "", "Path to a single raw HTTP request file")
	fs.StringVar(&cfg.RequestsFile, "requests", "", "Path to a JSONL file of {requestId, base64Request} entries")

	fs.StringVar(&cfg.WordlistPath, "wordlist", "", "Path to generic wordlist (required)")
	fs.StringVar(&cfg.CustomPath, "custom", "", "Path to custom wordlist")

	fs.IntVar(&nFlag, "n", 1, "Number of fields injected per request (default 1, min 4 if specified)")
	fs.IntVar(&cfg.Rate, "rate", 0, "Maximum requests per second (required)")
	fs.StringVar(&cfg.ValueRaw, "value", "[{}]", "Value injected for each field (any valid JSON)")

	fs.StringVar(&cfg.OutputPath, "output", "", "Output file path (defaults to stdout)")
	fs.StringVar(&cfg.LogPath, "log", "", "Log file path")
	fs.StringVar(&verbosityStr, "verbosity", "NORMAL", "Result verbosity: SUMMARIZED, NORMAL, VERBOSE")

	fs.Var(&headers, "H", "Header to add to requests (repeatable). Format: 'Name: value'")
	fs.Var(&cookies, "C", "Cookie to add to requests (repeatable). Format: 'name=value'")

	fs.StringVar(&cfg.RequestProto, "request-proto", "https", "Request protocol: http or https")
	fs.StringVar(&cfg.Proxy, "proxy", "", "HTTP proxy for all requests")
	fs.StringVar(&cfg.ReplayProxy, "replay-proxy", "", "HTTP proxy where hit requests are replayed")

	fs.StringVar(&cfg.MC, "mc", "", "Match HTTP status codes; if unspecified, all are matched")
	fs.StringVar(&cfg.ML, "ml", "", "Match number of lines in response")
	fs.StringVar(&cfg.MR, "mr", "", "Match response body regexp")
	fs.StringVar(&cfg.MS, "ms", "", "Match response size in bytes")
	fs.StringVar(&cfg.MW, "mw", "", "Match number of words in response")
	fs.StringVar(&mModeStr, "mmode", "or", "Matcher set operator: and|or")

	fs.StringVar(&cfg.FC, "fc", "", "Filter HTTP status codes")
	fs.StringVar(&cfg.FL, "fl", "", "Filter by number of lines")
	fs.StringVar(&cfg.FR, "fr", "", "Filter response body regexp")
	fs.StringVar(&cfg.FS, "fs", "", "Filter response size")
	fs.StringVar(&cfg.FW, "fw", "", "Filter by number of words")
	fs.StringVar(&fModeStr, "fmode", "or", "Filter set operator: and|or")

	fs.BoolVar(&cfg.HC, "hc", false, "Hit if status code differs from baseline")
	fs.BoolVar(&cfg.HS, "hs", false, "Hit if response size differs from baseline")
	fs.BoolVar(&cfg.HL, "hl", false, "Hit if number of lines differs from baseline")
	fs.StringVar(&cfg.HR, "hr", "", "Hit if response body matches this regexp")
	fs.BoolVar(&cfg.HRF, "hrf", false, "Hit if injected field name is reflected in response body")
	fs.StringVar(&hModeStr, "hmode", "or", "Hit set operator: and|or")

	if err := fs.Parse(os.Args[1:]); err != nil {
		return nil, err
	}

	cfg.Headers = headers
	cfg.Cookies = cookies

	// --- Validation ---

	if cfg.RequestFile == "" && cfg.RequestsFile == "" {
		return nil, errors.New("one of -request or -requests is required")
	}
	if cfg.RequestFile != "" && cfg.RequestsFile != "" {
		return nil, errors.New("only one of -request or -requests may be specified")
	}
	if cfg.WordlistPath == "" {
		return nil, errors.New("-wordlist is required")
	}
	if cfg.Rate <= 0 {
		return nil, errors.New("-rate is required and must be > 0")
	}

	// -n defaults to 1; if specified by user, minimum is 4.
	cfg.N = nFlag
	nWasSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "n" {
			nWasSet = true
		}
	})
	if nWasSet && cfg.N < 4 {
		return nil, errors.New("-n must be >= 4 when explicitly specified")
	}
	if cfg.N <= 0 {
		return nil, errors.New("-n must be > 0")
	}

	// MC default applies only when user did not set it explicitly... but per
	// spec: if -mc is not specified at all, all status codes are matched.
	// So we keep cfg.MC empty here (meaning "match all") and the matcher
	// package treats empty MC as "match anything".
	_ = mcDefault

	// Parse verbosity
	v, err := ParseVerbosity(verbosityStr)
	if err != nil {
		return nil, err
	}
	cfg.Verbosity = v

	// Parse modes
	if cfg.MMode, err = ParseMode(mModeStr); err != nil {
		return nil, err
	}
	if cfg.FMode, err = ParseMode(fModeStr); err != nil {
		return nil, err
	}
	if cfg.HMode, err = ParseMode(hModeStr); err != nil {
		return nil, err
	}

	// Proto
	switch strings.ToLower(cfg.RequestProto) {
	case "http", "https":
		cfg.RequestProto = strings.ToLower(cfg.RequestProto)
	default:
		return nil, fmt.Errorf("invalid -request-proto: %s (must be http or https)", cfg.RequestProto)
	}

	// Parse value as JSON; if it fails, fall back to plain string.
	var parsed interface{}
	if err := json.Unmarshal([]byte(cfg.ValueRaw), &parsed); err == nil {
		cfg.ValueJSON = parsed
	} else {
		cfg.ValueJSON = cfg.ValueRaw
	}

	return cfg, nil
}
