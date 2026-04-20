package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime"
	rtmetrics "runtime/metrics"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	applogger "sarbonNew/internal/logger"
	"sarbonNew/internal/server/mw"
)

// The /terminal screen is a public, no-auth live view of the server. It:
//   1. Replays the last ~2000 log lines from the in-memory hub on connect.
//   2. Streams every new log line in real time (same as stdout/stderr, but
//      with sensitive values masked by applogger.LogHub).
//   3. Every 1 second pushes a JSON "stats" frame with CPU%, RAM, goroutines,
//      heap usage, GC pauses and uptime — so you see at a glance how much
//      the backend is eating.
//
// There is intentionally NO authentication and NO buttons: the browser opens
// the page, the WS connects automatically, stats + logs start scrolling. If
// the connection drops it reconnects with small backoff.

var terminalUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// processStart is fixed at process boot; we report uptime from here.
var processStart = time.Now()

type TerminalStreamHandler struct {
	logger *zap.Logger
	hub    *applogger.LogHub
}

func NewTerminalStreamHandler(logger *zap.Logger, hub *applogger.LogHub) *TerminalStreamHandler {
	return &TerminalStreamHandler{logger: logger, hub: hub}
}

// Page serves the HTML console. Unconditionally — no auth, no login form.
func (h *TerminalStreamHandler) Page(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, terminalScreenHTML)
}

// StreamWS is the public live socket. Client messages are ignored (the
// terminal is view-only) — we only use the read side to detect disconnects.
func (h *TerminalStreamHandler) StreamWS(c *gin.Context) {
	conn, err := terminalUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Debug("terminal ws upgrade failed", zap.Error(err))
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	// Detect client disconnect by reading; we don't actually care about the
	// payload (the terminal is read-only from the client POV).
	go func() {
		defer cancel()
		for {
			if _, _, rerr := conn.ReadMessage(); rerr != nil {
				return
			}
		}
	}()

	// 1) Replay history so the user doesn't stare at a blank screen.
	if h.hub != nil {
		for _, line := range h.hub.Snapshot() {
			if err := sendLogFrame(conn, line); err != nil {
				return
			}
		}
	}
	_ = sendCtrlFrame(conn, "connected")

	// 2) Subscribe to live log stream.
	var logCh <-chan []byte
	var unsubscribe func()
	if h.hub != nil {
		logCh, unsubscribe = h.hub.Subscribe()
		defer unsubscribe()
	}

	// 3) Stats sampler.
	statsTicker := time.NewTicker(1 * time.Second)
	defer statsTicker.Stop()
	pingTicker := time.NewTicker(20 * time.Second)
	defer pingTicker.Stop()

	sampler := newCPUStatsSampler()
	// Push one stats frame immediately so the UI isn't empty for a second.
	_ = sendStatsFrame(conn, sampler.Sample())

	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-logCh:
			if !ok {
				return
			}
			if err := sendLogFrame(conn, line); err != nil {
				return
			}
		case <-statsTicker.C:
			if err := sendStatsFrame(conn, sampler.Sample()); err != nil {
				return
			}
		case <-pingTicker.C:
			_ = conn.WriteMessage(websocket.PingMessage, []byte("ping"))
		}
	}
}

// --- wire format ----------------------------------------------------------
//
// Every WS message is a JSON object with a "t" (type) discriminator:
//   {"t":"log",   "msg": "...zap line..."}
//   {"t":"stats", "cpu": ..., "ram_mb": ..., "goroutines": ..., ...}
//   {"t":"ctrl",  "msg": "connected"}
// This keeps the client simple and avoids mixing text and binary frames.

type wsLogFrame struct {
	T   string `json:"t"`
	Msg string `json:"msg"`
}

type wsCtrlFrame struct {
	T   string `json:"t"`
	Msg string `json:"msg"`
}

func sendLogFrame(conn *websocket.Conn, line []byte) error {
	// Trim trailing newline for nicer rendering.
	s := string(line)
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	b, _ := json.Marshal(wsLogFrame{T: "log", Msg: s})
	return conn.WriteMessage(websocket.TextMessage, b)
}

func sendCtrlFrame(conn *websocket.Conn, msg string) error {
	b, _ := json.Marshal(wsCtrlFrame{T: "ctrl", Msg: msg})
	return conn.WriteMessage(websocket.TextMessage, b)
}

func sendStatsFrame(conn *websocket.Conn, s StatsFrame) error {
	b, _ := json.Marshal(s)
	return conn.WriteMessage(websocket.TextMessage, b)
}

// StatsFrame is a snapshot of process resource usage. Sent once per second.
type StatsFrame struct {
	T            string  `json:"t"` // "stats"
	UptimeSec    float64 `json:"uptime_sec"`
	NumCPU       int     `json:"num_cpu"`
	Goroutines   int     `json:"goroutines"`
	GoVersion    string  `json:"go_version"`
	CPUPct       float64 `json:"cpu_pct"`        // 0..100 (one core)
	CPUPctNorm   float64 `json:"cpu_pct_norm"`   // 0..100 normalized by NumCPU
	HeapAllocMB  float64 `json:"heap_alloc_mb"`  // live objects
	HeapSysMB    float64 `json:"heap_sys_mb"`    // heap virtual size
	TotalAllocMB float64 `json:"total_alloc_mb"` // cumulative allocation since start
	RSSMB        float64 `json:"rss_mb"`         // best-effort resident mem (go runtime view)
	StackMB      float64 `json:"stack_mb"`
	NumGC        uint32  `json:"num_gc"`
	LastGCPauseMs float64 `json:"last_gc_pause_ms"`
	HTTPTotal    uint64  `json:"http_total"`
}

// cpuStatsSampler tracks the CPU-seconds counter exposed by runtime/metrics
// to compute per-second CPU% for the current process. This avoids needing
// cgo or platform-specific APIs (works the same on Linux / macOS / Windows).
type cpuStatsSampler struct {
	lastWallNs    int64
	lastCPUSecs   float64
	sample        []rtmetrics.Sample
	idxCPUTotal   int
	idxRSSTotal   int
	idxRSSHeap    int
	idxRSSStacks  int
	hasCPUTotal   bool
	hasRSS        bool
}

func newCPUStatsSampler() *cpuStatsSampler {
	// We subscribe to the metrics we care about. All are standard since Go 1.20+.
	descs := []string{
		"/cpu/classes/total:cpu-seconds",    // total CPU time consumed by process
		"/memory/classes/total:bytes",       // all memory charged to the Go runtime
		"/memory/classes/heap/objects:bytes",
		"/memory/classes/os-stacks:bytes",
	}
	samples := make([]rtmetrics.Sample, len(descs))
	for i, d := range descs {
		samples[i].Name = d
	}
	s := &cpuStatsSampler{
		sample:       samples,
		idxCPUTotal:  0,
		idxRSSTotal:  1,
		idxRSSHeap:   2,
		idxRSSStacks: 3,
	}
	// Probe once to see which metrics this Go version supports.
	rtmetrics.Read(samples)
	if samples[0].Value.Kind() == rtmetrics.KindFloat64 {
		s.hasCPUTotal = true
		s.lastCPUSecs = samples[0].Value.Float64()
	}
	if samples[1].Value.Kind() == rtmetrics.KindUint64 {
		s.hasRSS = true
	}
	s.lastWallNs = time.Now().UnixNano()
	return s
}

func (s *cpuStatsSampler) Sample() StatsFrame {
	rtmetrics.Read(s.sample)
	nowNs := time.Now().UnixNano()
	elapsed := float64(nowNs-s.lastWallNs) / float64(time.Second)
	if elapsed <= 0 {
		elapsed = 1
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	f := StatsFrame{
		T:             "stats",
		UptimeSec:     time.Since(processStart).Seconds(),
		NumCPU:        runtime.NumCPU(),
		Goroutines:    runtime.NumGoroutine(),
		GoVersion:     runtime.Version(),
		HeapAllocMB:   float64(memStats.HeapAlloc) / 1024 / 1024,
		HeapSysMB:     float64(memStats.HeapSys) / 1024 / 1024,
		TotalAllocMB:  float64(memStats.TotalAlloc) / 1024 / 1024,
		StackMB:       float64(memStats.StackSys) / 1024 / 1024,
		NumGC:         memStats.NumGC,
		LastGCPauseMs: 0,
		HTTPTotal:     mw.HTTPRequestsTotal.Load(),
	}
	if memStats.NumGC > 0 {
		// PauseNs is a circular buffer of the 256 most recent pauses.
		last := memStats.PauseNs[(memStats.NumGC+255)%256]
		f.LastGCPauseMs = float64(last) / 1e6
	}

	if s.hasCPUTotal && s.sample[s.idxCPUTotal].Value.Kind() == rtmetrics.KindFloat64 {
		cur := s.sample[s.idxCPUTotal].Value.Float64()
		delta := cur - s.lastCPUSecs
		if delta < 0 {
			delta = 0
		}
		// CPU% relative to one core, and normalized by NumCPU.
		pct := (delta / elapsed) * 100
		f.CPUPct = pct
		f.CPUPctNorm = pct / float64(f.NumCPU)
		s.lastCPUSecs = cur
	}

	if s.hasRSS && s.sample[s.idxRSSTotal].Value.Kind() == rtmetrics.KindUint64 {
		f.RSSMB = float64(s.sample[s.idxRSSTotal].Value.Uint64()) / 1024 / 1024
	}

	s.lastWallNs = nowNs
	return f
}

// ---------------------------------------------------------------------------
// HTML page. Single file, zero dependencies. Auto-connect, auto-reconnect,
// no buttons, no login.

const terminalScreenHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width,initial-scale=1"/>
  <title>Sarbon Live Console</title>
  <style>
    :root{
      --bg:#050910;--panel:#0b1220;--panel2:#0f172a;--line:#1f2937;
      --text:#e5e7eb;--muted:#94a3b8;--good:#22c55e;--warn:#f59e0b;--bad:#ef4444;--acc:#38bdf8;
    }
    *{box-sizing:border-box}
    html,body{margin:0;padding:0;background:var(--bg);color:var(--text);
      font-family:ui-sans-serif,system-ui,-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;min-height:100vh;}
    .wrap{max-width:1400px;margin:0 auto;padding:16px;}
    .head{display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;margin-bottom:12px;}
    .title{font-weight:800;font-size:20px;letter-spacing:.2px}
    .sub{color:var(--muted);font-size:12px}
    .dot{display:inline-block;width:8px;height:8px;border-radius:50%;background:var(--bad);margin-right:6px;vertical-align:middle;box-shadow:0 0 8px currentColor;color:var(--bad)}
    .dot.ok{background:var(--good);color:var(--good)}
    .dot.warn{background:var(--warn);color:var(--warn)}
    .grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:10px;margin-bottom:12px;}
    .card{
      background:linear-gradient(180deg,rgba(17,24,39,.85),rgba(2,6,23,.95));
      border:1px solid var(--line);border-radius:12px;padding:12px 14px;
      box-shadow:0 6px 20px rgba(0,0,0,.25) inset,0 1px 0 rgba(255,255,255,.02);
    }
    .k{font-size:11px;color:var(--muted);text-transform:uppercase;letter-spacing:.6px}
    .v{font-size:22px;font-weight:800;margin-top:4px;font-variant-numeric:tabular-nums}
    .unit{font-size:12px;color:var(--muted);margin-left:4px;font-weight:500}
    .bar{height:6px;background:#111827;border-radius:999px;overflow:hidden;margin-top:8px;border:1px solid #1f2937;}
    .bar > span{display:block;height:100%;background:linear-gradient(90deg,var(--good),var(--warn) 70%,var(--bad));transition:width .4s}
    #term{
      background:#05080f;border:1px solid var(--line);border-radius:12px;padding:12px 14px;height:62vh;
      overflow:auto;white-space:pre-wrap;word-break:break-word;
      font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace;font-size:12.5px;line-height:1.55;
    }
    .l{padding:0;margin:0}
    .lg-info{color:#93c5fd}.lg-err{color:#fca5a5}.lg-warn{color:#fcd34d}.lg-ok{color:#86efac}.lg-ctrl{color:#67e8f9}
    .foot{margin-top:8px;font-size:11px;color:var(--muted);text-align:right}
    /* subtle anim on stats update */
    .v.flash{animation:flash .35s ease-out}
    @keyframes flash{from{color:#fff}to{}}
  </style>
</head>
<body>
  <div class="wrap">
    <div class="head">
      <div>
        <div class="title"><span id="dot" class="dot"></span><span id="status">connecting…</span> — Sarbon Live Console</div>
        <div class="sub">Logs + process stats, no auth. Tokens / OTP / phones are automatically masked.</div>
      </div>
      <div class="sub" id="ver"></div>
    </div>

    <div class="grid">
      <div class="card"><div class="k">CPU (process)</div><div><span id="cpu" class="v">—</span><span class="unit">%</span></div><div class="bar"><span id="cpuBar" style="width:0%"></span></div></div>
      <div class="card"><div class="k">CPU / core avg</div><div><span id="cpuN" class="v">—</span><span class="unit">%</span></div><div class="bar"><span id="cpuNBar" style="width:0%"></span></div></div>
      <div class="card"><div class="k">RAM (Go)</div><div><span id="ram" class="v">—</span><span class="unit">MB</span></div></div>
      <div class="card"><div class="k">Heap alloc</div><div><span id="heap" class="v">—</span><span class="unit">MB</span></div></div>
      <div class="card"><div class="k">Goroutines</div><div><span id="gor" class="v">—</span></div></div>
      <div class="card"><div class="k">GC pause (last)</div><div><span id="gc" class="v">—</span><span class="unit">ms</span></div></div>
      <div class="card"><div class="k">HTTP requests</div><div><span id="req" class="v">—</span></div></div>
      <div class="card"><div class="k">Uptime</div><div><span id="up" class="v">—</span></div></div>
    </div>

    <div id="term"></div>
    <div class="foot">Auto-scrolls. Connection auto-reconnects. History: last 2000 lines.</div>
  </div>

<script>
(function(){
  var term = document.getElementById("term");
  var dot = document.getElementById("dot");
  var status = document.getElementById("status");
  var ver = document.getElementById("ver");
  var MAX_LINES = 3000;
  var ws = null, backoff = 500;

  function setStatus(s, cls){
    status.textContent = s;
    dot.className = "dot " + (cls || "");
  }

  function appendLine(text, kind){
    var p = document.createElement("div");
    p.className = "l lg-" + (kind || "info");
    p.textContent = text;
    term.appendChild(p);
    while (term.childElementCount > MAX_LINES) term.removeChild(term.firstChild);
    var atBottom = (term.scrollHeight - term.scrollTop - term.clientHeight) < 120;
    if (atBottom) term.scrollTop = term.scrollHeight;
  }

  function classifyLog(msg){
    var m = (msg || "").toLowerCase();
    if (/\berror\b|\bfatal\b|\bpanic\b|err=|stacktrace/i.test(m)) return "err";
    if (/\bwarn\b|warning/i.test(m)) return "warn";
    if (/\b2\d\d\b.*http request/i.test(m) || /\binfo\b/i.test(m)) return "info";
    return "info";
  }

  function fmtUptime(sec){
    sec = Math.max(0, Math.floor(sec));
    var d = Math.floor(sec/86400); sec %= 86400;
    var h = Math.floor(sec/3600); sec %= 3600;
    var m = Math.floor(sec/60); var s = sec%60;
    if (d) return d+"d "+h+"h";
    if (h) return h+"h "+m+"m";
    if (m) return m+"m "+s+"s";
    return s+"s";
  }

  function setVal(id, text){
    var el = document.getElementById(id);
    if (!el) return;
    if (el.textContent !== text){
      el.textContent = text;
      el.classList.remove("flash"); void el.offsetWidth; el.classList.add("flash");
    }
  }
  function setBar(id, pct){
    var el = document.getElementById(id);
    if (!el) return;
    el.style.width = Math.max(0, Math.min(100, pct)).toFixed(1) + "%";
  }

  function handleStats(s){
    setVal("cpu", s.cpu_pct.toFixed(1));
    setVal("cpuN", s.cpu_pct_norm.toFixed(1));
    setBar("cpuBar", s.cpu_pct);
    setBar("cpuNBar", s.cpu_pct_norm);
    var ram = s.rss_mb || (s.heap_sys_mb + s.stack_mb);
    setVal("ram", ram.toFixed(1));
    setVal("heap", s.heap_alloc_mb.toFixed(1));
    setVal("gor", s.goroutines.toString());
    setVal("gc", s.last_gc_pause_ms.toFixed(2));
    setVal("req", s.http_total.toString());
    setVal("up", fmtUptime(s.uptime_sec));
    ver.textContent = s.go_version + " · " + s.num_cpu + " vCPU";
  }

  function wsURL(){
    var proto = location.protocol === "https:" ? "wss://" : "ws://";
    return proto + location.host + "/terminal/ws";
  }

  function connect(){
    setStatus("connecting…", "warn");
    try { ws = new WebSocket(wsURL()); } catch(e){ scheduleReconnect(); return; }
    ws.onopen = function(){ setStatus("live", "ok"); backoff = 500; };
    ws.onmessage = function(ev){
      var frame; try { frame = JSON.parse(ev.data); } catch(e){ appendLine(ev.data, "info"); return; }
      if (!frame || !frame.t) return;
      if (frame.t === "log") { appendLine(frame.msg, classifyLog(frame.msg)); return; }
      if (frame.t === "stats") { handleStats(frame); return; }
      if (frame.t === "ctrl") { appendLine("[" + frame.msg + "]", "ctrl"); return; }
    };
    ws.onerror = function(){ /* handled in onclose */ };
    ws.onclose = function(ev){
      setStatus("disconnected (reconnecting…)", "warn");
      appendLine("[disconnected code=" + ev.code + "]", "warn");
      scheduleReconnect();
    };
  }
  function scheduleReconnect(){
    setTimeout(connect, backoff);
    backoff = Math.min(backoff * 2, 5000);
  }

  connect();
})();
</script>
</body>
</html>`
