package request

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"os"
	"strconv"
	"strings"
)

// ParsedRequest is the canonical in-memory representation of a request that
// the rest of the tool operates on.
type ParsedRequest struct {
	ID      int64
	Method  string
	Path    string
	Proto   string
	Host    string
	Headers http.Header
	Body    []byte
}

// ContentType returns the Content-Type header lower-cased without parameters.
func (p *ParsedRequest) ContentType() string {
	ct := p.Headers.Get("Content-Type")
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = ct[:i]
	}
	return strings.ToLower(strings.TrimSpace(ct))
}

// ParseRaw parses a raw HTTP request (Burp-style) from a byte slice.
func ParseRaw(raw []byte) (*ParsedRequest, error) {
	// Normalise line endings to CRLF since textproto expects CRLF.
	raw = normalizeCRLF(raw)
	br := bufio.NewReader(bytes.NewReader(raw))

	// Request line
	requestLine, err := br.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("read request line: %w", err)
	}
	requestLine = strings.TrimRight(requestLine, "\r\n")
	parts := strings.SplitN(requestLine, " ", 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("malformed request line: %q", requestLine)
	}

	// Headers
	tp := textproto.NewReader(br)
	mimeHeader, err := tp.ReadMIMEHeader()
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("read headers: %w", err)
	}
	headers := http.Header(mimeHeader)

	host := headers.Get("Host")

	// Body — bounded by Content-Length if present, otherwise read everything.
	var body []byte
	if cl := headers.Get("Content-Length"); cl != "" {
		n, err := strconv.Atoi(strings.TrimSpace(cl))
		if err == nil && n > 0 {
			body = make([]byte, n)
			if _, err := io.ReadFull(br, body); err != nil && err != io.ErrUnexpectedEOF {
				return nil, fmt.Errorf("read body: %w", err)
			}
		}
	} else {
		body, _ = io.ReadAll(br)
	}

	return &ParsedRequest{
		Method:  parts[0],
		Path:    parts[1],
		Proto:   parts[2],
		Host:    host,
		Headers: headers,
		Body:    body,
	}, nil
}

// LoadSingle reads a single raw HTTP request from a file.
func LoadSingle(path string) (*ParsedRequest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseRaw(data)
}

// jsonlEntry mirrors the Java record: { requestId, base64Request }.
type jsonlEntry struct {
	RequestID     int64  `json:"requestId"`
	Base64Request string `json:"base64Request"`
}

// LoadMulti reads a JSONL file of {requestId, base64Request} entries.
func LoadMulti(path string) ([]*ParsedRequest, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)

	var out []*ParsedRequest
	lineNum := 0
	for sc.Scan() {
		lineNum++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e jsonlEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("line %d: parse json: %w", lineNum, err)
		}
		raw, err := base64.StdEncoding.DecodeString(e.Base64Request)
		if err != nil {
			return nil, fmt.Errorf("line %d: decode base64: %w", lineNum, err)
		}
		pr, err := ParseRaw(raw)
		if err != nil {
			return nil, fmt.Errorf("line %d (id=%d): %w", lineNum, e.RequestID, err)
		}
		pr.ID = e.RequestID
		out = append(out, pr)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// normalizeCRLF ensures that line breaks are CRLF as required by textproto.
func normalizeCRLF(b []byte) []byte {
	// Replace lone \n (not preceded by \r) with \r\n.
	if !bytes.Contains(b, []byte("\r\n")) {
		return bytes.ReplaceAll(b, []byte("\n"), []byte("\r\n"))
	}
	// Mixed: convert \r\n to \n first, then back to \r\n.
	tmp := bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(tmp, []byte("\n"), []byte("\r\n"))
}

// Clone returns a deep copy so a request can be mutated without affecting the
// original ParsedRequest.
func (p *ParsedRequest) Clone() *ParsedRequest {
	cp := *p
	cp.Headers = p.Headers.Clone()
	if p.Body != nil {
		cp.Body = append([]byte(nil), p.Body...)
	}
	return &cp
}
