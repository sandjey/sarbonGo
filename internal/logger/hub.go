package logger

import (
	"regexp"
	"sync"
)

// LogHub is an in-memory ring buffer of recent log lines plus a pub/sub fanout
// for live subscribers (the /terminal WS). It plugs into zap as a sink: see
// NewWithHub. Every log line written by ANY logger created through this
// package is captured here, including the HTTP request logs produced by
// mw.RequestLogger.
//
// Thread-safety: all methods are safe for concurrent use.
//
// Slow subscribers never block the hub: if a subscriber channel is full, the
// line is dropped for that subscriber only.
type LogHub struct {
	mu    sync.RWMutex
	buf   [][]byte // ring
	head  int      // next write index
	count int      // number of valid entries, ≤ cap(buf)
	subs  map[chan []byte]struct{}
}

// NewLogHub creates a hub with the given ring capacity (number of LINES, not
// bytes). 2000 is a reasonable default — roughly covers the last 5-10 minutes
// of a busy prod server.
func NewLogHub(capacity int) *LogHub {
	if capacity <= 0 {
		capacity = 2000
	}
	return &LogHub{
		buf:  make([][]byte, capacity),
		subs: make(map[chan []byte]struct{}),
	}
}

// Write appends a single log line to the ring and fans it out to live subs.
// Implements io.Writer, so it can be wired into zap as a WriteSyncer.
//
// Note: zap calls Write once per log entry with the fully formatted line
// (including trailing newline). We sanitize first so tokens / OTPs / passwords
// never leak through the /terminal stream to the browser.
func (h *LogHub) Write(p []byte) (int, error) {
	n := len(p)
	if n == 0 {
		return 0, nil
	}
	// Defensive copy + sanitize.
	line := sanitizeLogLine(p)

	h.mu.Lock()
	h.buf[h.head] = line
	h.head = (h.head + 1) % len(h.buf)
	if h.count < len(h.buf) {
		h.count++
	}
	// Snapshot subscribers to avoid holding the lock during send.
	subs := make([]chan []byte, 0, len(h.subs))
	for c := range h.subs {
		subs = append(subs, c)
	}
	h.mu.Unlock()

	for _, c := range subs {
		select {
		case c <- line:
		default:
			// drop — consumer is slow
		}
	}
	return n, nil
}

// Sync is required by zapcore.WriteSyncer; we're purely in-memory, nothing to flush.
func (h *LogHub) Sync() error { return nil }

// Snapshot returns a copy of the ring contents in chronological order (oldest
// first). Used to replay history to a newly-connected terminal subscriber so
// it doesn't start with an empty screen.
func (h *LogHub) Snapshot() [][]byte {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([][]byte, 0, h.count)
	start := 0
	if h.count == len(h.buf) {
		start = h.head
	}
	for i := 0; i < h.count; i++ {
		idx := (start + i) % len(h.buf)
		if len(h.buf[idx]) == 0 {
			continue
		}
		out = append(out, h.buf[idx])
	}
	return out
}

// Subscribe registers a channel that receives every new log line from now on.
// Returns an unsubscribe function that must be called on disconnect.
//
// Buffer size is deliberately small (256): under heavy log pressure we drop
// the oldest messages for this subscriber rather than backpressure the hub
// for everyone.
func (h *LogHub) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 256)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	cancel := func() {
		h.mu.Lock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
	return ch, cancel
}

// --- sanitization ---------------------------------------------------------
//
// Log lines contain enough sensitive data to be dangerous when exposed over
// an unauthenticated /terminal socket: bearer tokens, OTP codes, passwords.
// We mask the most common offenders before broadcasting. The masking is
// deliberately aggressive — false positives are harmless ("***" in a log
// line), false negatives are a real security issue.

var (
	reBearer   = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._\-]{10,}`)
	reJWTish   = regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]+`)
	reTokHdr   = regexp.MustCompile(`(?i)(x-(?:user|client|admin|refresh|access)-token\s*[:=]\s*)[^\s",}]+`)
	reOTPKey   = regexp.MustCompile(`(?i)("?(?:otp|code|password|passwd|pwd|secret|pin)"?\s*[:=]\s*"?)([^"\s,}]+)`)
	rePhoneLog = regexp.MustCompile(`\+\d{9,15}`)
)

func sanitizeLogLine(p []byte) []byte {
	s := string(p)
	s = reBearer.ReplaceAllString(s, "${1}***")
	s = reJWTish.ReplaceAllString(s, "***.jwt.***")
	s = reTokHdr.ReplaceAllString(s, "${1}***")
	s = reOTPKey.ReplaceAllString(s, "${1}***")
	// Phones appear in many flows (send OTP, register) and are PII; mask all but the last 2 digits.
	s = rePhoneLog.ReplaceAllStringFunc(s, func(m string) string {
		if len(m) < 4 {
			return "***"
		}
		return "+***" + m[len(m)-2:]
	})
	return []byte(s)
}
