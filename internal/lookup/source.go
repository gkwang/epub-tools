package lookup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	userAgent      = "epub-tools/0.1 (https://github.com/gkwang/epub-tools)"
	requestTimeout = 10 * time.Second
	maxBody        = 4 << 20
)

// Query is what a Source is asked to find.
type Query struct {
	Title   string   // title with any volume part removed
	Authors []string // as found in the epub, may be empty
	ISBN    string   // digits only, may be empty
	Lang    string   // effective BCP 47 tag of the book
}

// Candidate is one catalog entry returned by a Source.
type Candidate struct {
	Title     string
	Lang      string // ISO 639-1 (optionally with region); "" if unknown
	Authors   []string
	ISBNMatch bool // found by an exact ISBN query
}

// Source is one online catalog. A source with nothing to say about the query
// (for example Bangumi for an English book) returns no candidates and no error.
type Source interface {
	Name() string
	Search(ctx context.Context, q Query) ([]Candidate, error)
}

// httpStatusError is returned for non-2xx responses.
type httpStatusError struct{ Code int }

func (e *httpStatusError) Error() string { return fmt.Sprintf("HTTP %d", e.Code) }

func defaultClient() *http.Client { return &http.Client{Timeout: requestTimeout} }

// doJSON sends req (adding the User-Agent) and decodes a 2xx JSON body into v.
func doJSON(hc *http.Client, req *http.Request, v any) error {
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
		return &httpStatusError{Code: resp.StatusCode}
	}
	return json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(v)
}

// limiter spaces requests at least min apart.
type limiter struct {
	mu   sync.Mutex
	min  time.Duration
	last time.Time
}

func (l *limiter) wait(ctx context.Context) error {
	if l == nil || l.min <= 0 {
		return ctx.Err()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if d := l.min - time.Since(l.last); d > 0 {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	l.last = time.Now()
	return ctx.Err()
}
