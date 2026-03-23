package handlers

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const (
	terminalLoginValue    = "sarbonterminal"
	terminalPasswordValue = "sarbonterminal"
	terminalAuthCookie    = "sarbon_terminal_auth"
)

var terminalUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type TerminalStreamHandler struct {
	logger *zap.Logger
	logMu  sync.Mutex
}

func NewTerminalStreamHandler(logger *zap.Logger) *TerminalStreamHandler {
	return &TerminalStreamHandler{logger: logger}
}

func (h *TerminalStreamHandler) Page(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	if !h.isAuthorized(c) {
		c.String(http.StatusOK, terminalLoginHTML)
		return
	}
	c.String(http.StatusOK, terminalScreenHTML)
}

func (h *TerminalStreamHandler) Login(c *gin.Context) {
	login := strings.TrimSpace(c.PostForm("login"))
	password := strings.TrimSpace(c.PostForm("password"))
	if login != terminalLoginValue || password != terminalPasswordValue {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusUnauthorized, terminalLoginHTML)
		return
	}
	c.SetCookie(terminalAuthCookie, "ok", 86400*14, "/", "", false, true)
	c.Redirect(http.StatusFound, "/terminal")
}

func (h *TerminalStreamHandler) Logout(c *gin.Context) {
	c.SetCookie(terminalAuthCookie, "", -1, "/", "", false, true)
	c.Redirect(http.StatusFound, "/terminal")
}

func (h *TerminalStreamHandler) StreamWS(c *gin.Context) {
	if !h.isAuthorized(c) {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	conn, err := terminalUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Debug("terminal ws upgrade failed", zap.Error(err))
		return
	}
	defer conn.Close()

	cmdStr := strings.TrimSpace(os.Getenv("TERMINAL_SCREEN_CMD"))
	if cmdStr == "" {
		cmdStr = h.defaultTerminalCommand()
	}

	_ = conn.WriteMessage(websocket.TextMessage, []byte("$ "+cmdStr))
	_ = h.sendTodayLog(conn)

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-lc", cmdStr)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("failed to open stdout pipe"))
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("failed to open stderr pipe"))
		return
	}
	if err := cmd.Start(); err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("failed to start command: %v", err)))
		return
	}

	errCh := make(chan error, 2)
	lines := make(chan string, 256)

	streamReader := func(prefix string, r io.Reader) {
		sc := bufio.NewScanner(r)
		buf := make([]byte, 0, 64*1024)
		sc.Buffer(buf, 2*1024*1024)
		for sc.Scan() {
			lines <- prefix + sc.Text()
		}
		if scanErr := sc.Err(); scanErr != nil {
			errCh <- scanErr
			return
		}
		errCh <- nil
	}

	go streamReader("", stdout)
	go streamReader("ERR: ", stderr)

	go func() {
		defer cancel()
		for {
			if _, _, rerr := conn.ReadMessage(); rerr != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	doneReaders := 0
	for {
		select {
		case line := <-lines:
			_ = h.appendDailyLog(line)
			if werr := conn.WriteMessage(websocket.TextMessage, []byte(line)); werr != nil {
				cancel()
				_ = cmd.Process.Kill()
				return
			}
		case <-ticker.C:
			_ = conn.WriteMessage(websocket.PingMessage, []byte("ping"))
		case rerr := <-errCh:
			if rerr != nil && !errors.Is(rerr, io.EOF) {
				msg := "stream read error: " + rerr.Error()
				_ = h.appendDailyLog("ERR: " + msg)
				_ = conn.WriteMessage(websocket.TextMessage, []byte(msg))
			}
			doneReaders++
			if doneReaders >= 2 {
				waitErr := cmd.Wait()
				if waitErr != nil {
					msg := "command exited with error: " + waitErr.Error()
					_ = h.appendDailyLog("ERR: " + msg)
					_ = conn.WriteMessage(websocket.TextMessage, []byte(msg))
				} else {
					_ = h.appendDailyLog("command exited")
					_ = conn.WriteMessage(websocket.TextMessage, []byte("command exited"))
				}
				return
			}
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return
		}
	}
}

func (h *TerminalStreamHandler) isAuthorized(c *gin.Context) bool {
	v, err := c.Cookie(terminalAuthCookie)
	return err == nil && strings.TrimSpace(v) == "ok"
}

func (h *TerminalStreamHandler) todayLogPath() string {
	dir := strings.TrimSpace(os.Getenv("TERMINAL_LOG_DIR"))
	if dir == "" {
		dir = "storage/terminal-logs"
	}
	return filepath.Join(dir, time.Now().Format("2006-01-02")+".log")
}

func (h *TerminalStreamHandler) appendDailyLog(line string) error {
	h.logMu.Lock()
	defer h.logMu.Unlock()

	p := h.todayLogPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	stamped := fmt.Sprintf("%s %s\n", time.Now().Format("15:04:05"), line)
	_, err = f.WriteString(stamped)
	return err
}

func (h *TerminalStreamHandler) sendTodayLog(conn *websocket.Conn) error {
	p := h.todayLogPath()
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 2*1024*1024)

	_ = conn.WriteMessage(websocket.TextMessage, []byte("---- today log replay start ----"))
	for sc.Scan() {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(sc.Text())); err != nil {
			return err
		}
	}
	_ = conn.WriteMessage(websocket.TextMessage, []byte("---- today log replay end ----"))
	return sc.Err()
}

func (h *TerminalStreamHandler) defaultTerminalCommand() string {
	// 1) Prefer Cursor terminal output files (exactly what user sees in IDE terminal).
	if p, ok := latestCursorTerminalFile(); ok {
		return "tail -n 500 -F " + shellQuote(p)
	}

	// Prefer local text logs for non-docker setups.
	fileCandidates := []string{
		"app.log",
		"logs/app.log",
		"storage/app.log",
	}
	for _, p := range fileCandidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return "tail -n 500 -F " + shellQuote(p)
		}
	}

	// If docker exists and compose file is present, use compose logs.
	if _, err := exec.LookPath("docker"); err == nil {
		if _, err := os.Stat("docker-compose.yml"); err == nil {
			return "docker compose logs -f --tail=500"
		}
	}

	// Fallback: keep connection alive with a clear instruction only.
	return `bash -lc 'echo "No terminal log source found."; echo "Set TERMINAL_SCREEN_CMD, e.g. tail -n 500 -F ~/.cursor/projects/home-sandjey-SarbonGo/terminals/1.txt"; while true; do sleep 60; done'`
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func latestCursorTerminalFile() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", false
	}

	base := filepath.Join(home, ".cursor", "projects")
	projectDirs, err := filepath.Glob(filepath.Join(base, "*"))
	if err != nil || len(projectDirs) == 0 {
		return "", false
	}

	type item struct {
		path string
		mod  time.Time
	}
	var files []item
	for _, pdir := range projectDirs {
		matches, _ := filepath.Glob(filepath.Join(pdir, "terminals", "*.txt"))
		for _, f := range matches {
			st, serr := os.Stat(f)
			if serr != nil || st.IsDir() {
				continue
			}
			files = append(files, item{path: f, mod: st.ModTime()})
		}
	}
	if len(files) == 0 {
		return "", false
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].mod.After(files[j].mod)
	})
	return files[0].path, true
}

const terminalLoginHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width,initial-scale=1"/>
  <title>Sarbon Terminal Login</title>
  <style>
    *{box-sizing:border-box}
    body{
      margin:0;min-height:100vh;display:grid;place-items:center;
      background:radial-gradient(circle at top,#1f2937,#020617 70%);
      font-family:ui-sans-serif,system-ui,-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;
      color:#e5e7eb;
    }
    .card{
      width:min(420px,92vw);background:#0f172a;border:1px solid #334155;border-radius:14px;
      padding:20px;box-shadow:0 24px 50px rgba(2,6,23,.45);
    }
    h1{font-size:22px;margin:0 0 6px}
    .sub{color:#94a3b8;font-size:13px;margin-bottom:16px}
    label{display:block;font-size:13px;color:#cbd5e1;margin-bottom:6px}
    input{
      width:100%;padding:10px 12px;border-radius:10px;border:1px solid #475569;background:#111827;
      color:#e5e7eb;font-size:14px;margin-bottom:12px;outline:none;
    }
    input:focus{border-color:#38bdf8}
    button{
      width:100%;border:none;border-radius:10px;padding:11px 12px;background:#2563eb;color:#fff;font-weight:700;cursor:pointer;
    }
    .hint{margin-top:10px;color:#94a3b8;font-size:12px}
  </style>
</head>
<body>
  <form class="card" method="post" action="/terminal/login">
    <h1>Terminal Login</h1>
    <div class="sub">Enter login and password</div>
    <label>Login</label>
    <input name="login" autocomplete="username" required />
    <label>Password</label>
    <input name="password" type="password" autocomplete="current-password" required />
    <button type="submit">Sign in</button>
    <div class="hint">Demo credentials are configured on server side.</div>
  </form>
</body>
</html>`

const terminalScreenHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width,initial-scale=1"/>
  <title>Sarbon Terminal Screen</title>
  <style>
    :root{
      --panel:#111827;--panel2:#0f172a;--text:#e5e7eb;--muted:#94a3b8;--line:#1f2937;
    }
    *{box-sizing:border-box}
    body{
      margin:0;background:radial-gradient(circle at top,#111827,#020617 65%);color:var(--text);
      min-height:100vh;padding:20px;
      font-family:ui-sans-serif,system-ui,-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;
    }
    .wrap{max-width:1200px;margin:0 auto}
    .top{display:flex;justify-content:space-between;align-items:center;gap:10px;flex-wrap:wrap;margin-bottom:12px}
    .title{font-weight:700;font-size:22px}
    .sub{font-size:13px;color:var(--muted)}
    .actions{display:flex;gap:8px;align-items:center}
    button,a{
      border:none;border-radius:10px;padding:10px 14px;font-weight:700;cursor:pointer;text-decoration:none;color:#fff;
    }
    .primary{background:#2563eb}
    .danger{background:#dc2626}
    .soft{background:#334155}
    .card{
      background:linear-gradient(180deg,rgba(17,24,39,.95),rgba(2,6,23,.95));
      border:1px solid var(--line);border-radius:14px;padding:14px;
    }
    .status{font-size:12px;font-weight:700;border-radius:999px;padding:8px 12px}
    .closed{background:rgba(239,68,68,.13);color:#fca5a5}
    .connecting{background:rgba(245,158,11,.14);color:#fcd34d}
    .open{background:rgba(34,197,94,.14);color:#86efac}
    #term{
      margin-top:12px;background:var(--panel2);border:1px solid var(--line);border-radius:12px;height:74vh;padding:14px;
      overflow:auto;white-space:pre-wrap;word-break:break-word;
      font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace;font-size:13px;line-height:1.5;
    }
    .line-info{color:#93c5fd}.line-err{color:#fca5a5}.line-cmd{color:#86efac}
  </style>
</head>
<body>
  <div class="wrap">
    <div class="top">
      <div>
        <div class="title">Terminal Screen</div>
        <div class="sub">Live logs + daily file archive</div>
      </div>
      <div class="actions">
        <button class="primary" onclick="connectWS()">Connect</button>
        <button class="danger" onclick="disconnectWS()">Disconnect</button>
        <button class="soft" onclick="clearTerm()">Clear</button>
        <a class="soft" href="/terminal/logout">Logout</a>
        <span id="status" class="status closed">CLOSED</span>
      </div>
    </div>
    <div class="card">
      <div id="term"></div>
    </div>
  </div>
<script>
let ws = null;
const $ = (id) => document.getElementById(id);

function setStatus(v){
  const el = $("status");
  el.textContent = v;
  el.className = "status " + v.toLowerCase();
}
function appendLine(text){
  const term = $("term");
  const row = document.createElement("div");
  if (text.startsWith("$ ")) row.className = "line-cmd";
  else if (text.startsWith("ERR:") || text.toLowerCase().includes("error")) row.className = "line-err";
  else row.className = "line-info";
  row.textContent = text;
  term.appendChild(row);
  term.scrollTop = term.scrollHeight;
}
function wsURL(){
  const proto = location.protocol === "https:" ? "wss://" : "ws://";
  return proto + location.host + "/terminal/ws";
}
function connectWS(){
  disconnectWS();
  setStatus("CONNECTING");
  appendLine("Connecting: " + wsURL());
  ws = new WebSocket(wsURL());
  ws.onopen = () => { setStatus("OPEN"); appendLine("Connected."); };
  ws.onmessage = (e) => appendLine(e.data);
  ws.onerror = () => appendLine("ERR: websocket error");
  ws.onclose = (e) => {
    setStatus("CLOSED");
    appendLine("Disconnected. code=" + e.code + " reason=" + (e.reason || "-"));
    ws = null;
  };
}
function disconnectWS(){ if (ws) { ws.close(); ws = null; } setStatus("CLOSED"); }
function clearTerm(){ $("term").innerHTML = ""; }
</script>
</body>
</html>`
