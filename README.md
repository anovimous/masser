# massassign

A command-line tool for testing mass assignment vulnerabilities in HTTP APIs.

## Build

```bash
cd massassign
go mod tidy
go build -o massassign .
```

## Flags

### Required

- `-request <path>` or `-requests <path>` — single raw HTTP request file, or JSONL file of `{"requestId": <long>, "base64Request": "<base64>"}` entries.
- `-wordlist <path>` — generic wordlist of field names.
- `-rate <int>` — maximum requests per second.

### Optional

- `-custom <path>` — additional custom wordlist; merged + deduplicated with `-wordlist`.
- `-n <int>` — fields injected per request. Defaults to `1`. If specified, must be ≥ `4`. When > 1, a hit triggers binary-search narrowing down to the responsible field.
- `-value <json>` — value used for each injected field. Any valid JSON. Defaults to `[{}]`.
- `-output <path>` — output JSON file. Defaults to stdout.
- `-log <path>` — log file. Defaults to stderr.
- `-verbosity <SUMMARIZED|NORMAL|VERBOSE>` — defaults to `NORMAL`.
  - `SUMMARIZED`: only the fields that triggered a hit.
  - `NORMAL`: fields + status code.
  - `VERBOSE`: fields + status code + full response body.
- `-H "Name: value"` — add a header. Repeatable.
- `-C "name=value"` — add a cookie (merged with originals). Repeatable.
- `-request-proto <http|https>` — defaults to `https`.
- `-proxy <url>` — proxy for all outgoing requests.
- `-replay-proxy <url>` — proxy through which hit requests are replayed.

### Matcher / Filter (ffuf-style)

Filters are applied **before** matchers.

- `-mc` status codes (e.g. `200-299,301,302`). If unspecified, all are matched.
- `-ml`, `-ms`, `-mw` line / size / word counts.
- `-mr` regexp against response body.
- `-mmode <and|or>` — defaults to `or`.
- `-fc`, `-fl`, `-fs`, `-fw`, `-fr`, `-fmode` — same shape as matchers.

### Hit detection

If no hit flag is set, anything that passes the matcher/filter is a hit.

- `-hc` — status code differs from baseline.
- `-hs` — response size differs from baseline.
- `-hl` — response line count differs from baseline.
- `-hr <regexp>` — body matches regexp.
- `-hrf` — injected field name appears in response body.
- `-hmode <and|or>` — defaults to `or`.

## Behaviour

- Bodies are auto-detected as JSON or `application/x-www-form-urlencoded` from `Content-Type`.
- For JSON bodies, fields are injected into every object at every depth, plus the **first** object element of every array (recursively).
- Cookies from `-C` are merged with cookies present in the original request.
- Redirects are not followed.
- Each source request gets a baseline (un-injected) request first; subsequent injected responses are compared against it.
- A `-rate` token-bucket limiter caps outgoing requests; replay-proxy traffic does not count towards it.

## Output

Top-level JSON array, one entry per source `requestId`, containing the baseline status and the list of confirmed hits at the requested verbosity.
