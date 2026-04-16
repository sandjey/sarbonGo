package swaggerui

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"

	"sarbonNew/internal/server/resp"
)

// Minimal swagger UI without codegen, using docs/openapi.yaml.
func Register(r *gin.Engine) {
	r.GET("/openapi.yaml", func(c *gin.Context) {
		if p, ok := findUp("docs/openapi.yaml", 10); ok {
			c.File(p)
			return
		}
		resp.ErrorLang(c, http.StatusNotFound, "openapi_not_found")
	})

	r.GET("/docs", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, swaggerHTML)
	})

	r.GET("/docs/flow", func(c *gin.Context) {
		if p, ok := findUp("docs/swagger-flow.html", 10); ok {
			c.File(p)
			return
		}
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusNotFound, "<html><body><h3>docs/swagger-flow.html not found</h3><p><a href=\"/docs\">/docs</a></p></body></html>")
	})

	r.GET("/ws-test", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, wsTestHTML)
	})
	r.GET("/calls-test", func(c *gin.Context) {
		if p, ok := findUp("docs/calls-test.html", 10); ok {
			c.File(p)
			return
		}
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, "<html><body><h3>calls-test page not found</h3></body></html>")
	})
	r.GET("/calls-webrtc", func(c *gin.Context) {
		if p, ok := findUp("docs/calls-webrtc.html", 10); ok {
			c.File(p)
			return
		}
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, "<html><body><h3>calls-webrtc page not found</h3></body></html>")
	})
}

func findUp(rel string, maxDepth int) (string, bool) {
	if maxDepth <= 0 {
		maxDepth = 6
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for i := 0; i <= maxDepth; i++ {
		p := filepath.Join(dir, rel)
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", false
}

const swaggerHTML = `<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Sarbon API — Документация</title>
    <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" />
    <style>
      .topbar { display: none; }

      /* Theme tokens */
      :root {
        --bg: #ffffff;
        --fg: #111827;
        --muted: #64748b;
        --card: #ffffff;
        --border: rgba(0,0,0,.12);
        --menu-bg: rgba(255,255,255,.92);
        --menu-border: rgba(0,0,0,.08);
        --btn-bg: #ffffff;
        --btn-fg: #111827;
        --btn-hover: #f9fafb;
        --btn-active-bg: #111827;
        --btn-active-fg: #ffffff;
        --hint-bg: #f0f9ff;
        --hint-border: #bae6fd;
        --hint-fg: #0c4a6e;
      }
      html[data-theme="dark"] {
        --bg: #0b1220;
        --fg: #e5e7eb;
        --muted: #94a3b8;
        --card: #0f172a;
        --border: rgba(255,255,255,.14);
        --menu-bg: rgba(15,23,42,.82);
        --menu-border: rgba(255,255,255,.10);
        --btn-bg: rgba(255,255,255,.06);
        --btn-fg: #e5e7eb;
        --btn-hover: rgba(255,255,255,.10);
        --btn-active-bg: #e5e7eb;
        --btn-active-fg: #0b1220;
        --hint-bg: rgba(59,130,246,.12);
        --hint-border: rgba(59,130,246,.22);
        --hint-fg: #bfdbfe;
      }

      html, body { background: var(--bg); color: var(--fg); }

      /* Top menu */
      body { padding-top: 58px; }
      .sarbon-topmenu {
        position: fixed;
        top: 0;
        left: 0;
        right: 0;
        min-height: 58px;
        height: auto;
        display: flex;
        align-items: center;
        justify-content: center;
        flex-wrap: wrap;
        gap: 10px;
        padding: 8px 14px;
        background: var(--menu-bg);
        backdrop-filter: blur(10px);
        border-bottom: 1px solid var(--menu-border);
        z-index: 9999;
      }
      .sarbon-topmenu .brand {
        font-family: ui-sans-serif, system-ui, -apple-system, Segoe UI, Roboto, Helvetica, Arial;
        font-weight: 700;
        letter-spacing: .2px;
        margin-right: 8px;
        color: var(--fg);
      }
      .sarbon-topmenu .btn {
        appearance: none;
        border: 1px solid var(--border);
        background: var(--btn-bg);
        color: var(--btn-fg);
        border-radius: 999px;
        padding: 10px 14px;
        font-size: 14px;
        line-height: 1;
        cursor: pointer;
        transition: background .15s ease, border-color .15s ease, box-shadow .15s ease;
      }
      .sarbon-topmenu .btn:hover { background: var(--btn-hover); }
      .sarbon-topmenu .btn.active {
        background: var(--btn-active-bg);
        color: var(--btn-active-fg);
        border-color: var(--btn-active-bg);
        box-shadow: 0 6px 18px rgba(0,0,0,.18);
      }

      /* Theme toggle (distinct) */
      .sarbon-theme-toggle {
        display: inline-flex;
        align-items: center;
        gap: 10px;
        padding: 8px 10px;
        border-radius: 999px;
        border: 1px solid var(--border);
        background: var(--btn-bg);
        color: var(--btn-fg);
        cursor: pointer;
        user-select: none;
        transition: background .15s ease, border-color .15s ease, transform .08s ease;
      }
      .sarbon-theme-toggle:hover { background: var(--btn-hover); }
      .sarbon-theme-toggle:active { transform: translateY(1px); }
      .sarbon-theme-toggle .label {
        font-size: 13px;
        font-weight: 700;
        letter-spacing: .2px;
      }
      .sarbon-theme-toggle .icons {
        display: inline-flex;
        align-items: center;
        gap: 6px;
        font-size: 14px;
        opacity: .9;
      }
      .sarbon-theme-toggle .switch {
        width: 38px;
        height: 22px;
        border-radius: 999px;
        position: relative;
        background: rgba(148,163,184,.25);
        border: 1px solid rgba(148,163,184,.35);
        flex: 0 0 auto;
        box-shadow: inset 0 1px 2px rgba(0,0,0,.12);
      }
      html[data-theme="dark"] .sarbon-theme-toggle .switch {
        background: rgba(59,130,246,.22);
        border-color: rgba(59,130,246,.34);
      }
      .sarbon-theme-toggle .knob {
        position: absolute;
        top: 50%;
        left: 3px;
        width: 16px;
        height: 16px;
        border-radius: 999px;
        transform: translateY(-50%);
        background: var(--card);
        border: 1px solid var(--border);
        transition: left .18s ease;
      }
      html[data-theme="dark"] .sarbon-theme-toggle .knob { left: 19px; }

      /* Keep content width comfortable */
      .swagger-ui .wrapper { max-width: 1240px; }

      .sarbon-docs-hint {
        padding: 10px 14px;
        margin: 0 0 12px 0;
        background: var(--hint-bg);
        border: 1px solid var(--hint-border);
        border-radius: 8px;
        font-size: 13px;
        color: var(--hint-fg);
      }

      /* Swagger UI dark overrides (readable + consistent) */
      html[data-theme="dark"] .swagger-ui,
      html[data-theme="dark"] .swagger-ui * {
        color: var(--fg);
      }
      html[data-theme="dark"] .swagger-ui a { color: #93c5fd !important; }
      html[data-theme="dark"] .swagger-ui a:hover { color: #bfdbfe !important; }

      html[data-theme="dark"] .swagger-ui .info,
      html[data-theme="dark"] .swagger-ui .scheme-container,
      html[data-theme="dark"] .swagger-ui .wrapper,
      html[data-theme="dark"] .swagger-ui .renderedMarkdown,
      html[data-theme="dark"] .swagger-ui .markdown,
      html[data-theme="dark"] .swagger-ui .markdown p,
      html[data-theme="dark"] .swagger-ui .markdown li {
        color: var(--fg) !important;
      }

      /* Cards/containers */
      html[data-theme="dark"] .swagger-ui .scheme-container,
      html[data-theme="dark"] .swagger-ui section.models,
      html[data-theme="dark"] .swagger-ui .opblock-tag-section,
      html[data-theme="dark"] .swagger-ui .opblock .opblock-section-header,
      html[data-theme="dark"] .swagger-ui .opblock .opblock-body,
      html[data-theme="dark"] .swagger-ui .responses-wrapper,
      html[data-theme="dark"] .swagger-ui .responses-inner,
      html[data-theme="dark"] .swagger-ui .model-box,
      html[data-theme="dark"] .swagger-ui .model-container,
      html[data-theme="dark"] .swagger-ui .tab,
      html[data-theme="dark"] .swagger-ui .auth-container,
      html[data-theme="dark"] .swagger-ui .dialog-ux .modal-ux,
      html[data-theme="dark"] .swagger-ui .dialog-ux .modal-ux-content,
      html[data-theme="dark"] .swagger-ui .dialog-ux .modal-ux-header {
        background: var(--card) !important;
      }
      html[data-theme="dark"] .swagger-ui .scheme-container { border: 1px solid rgba(255,255,255,.10); box-shadow: none; }
      html[data-theme="dark"] .swagger-ui section.models { border: 1px solid rgba(255,255,255,.10); }

      /* Borders */
      html[data-theme="dark"] .swagger-ui .opblock,
      html[data-theme="dark"] .swagger-ui .opblock-tag,
      html[data-theme="dark"] .swagger-ui .opblock .opblock-section-header,
      html[data-theme="dark"] .swagger-ui section.models,
      html[data-theme="dark"] .swagger-ui .model-container,
      html[data-theme="dark"] .swagger-ui .tab li,
      html[data-theme="dark"] .swagger-ui table,
      html[data-theme="dark"] .swagger-ui thead tr th,
      html[data-theme="dark"] .swagger-ui tbody tr td,
      html[data-theme="dark"] .swagger-ui .responses-inner,
      html[data-theme="dark"] .swagger-ui .authorization__btn,
      html[data-theme="dark"] .swagger-ui .dialog-ux .modal-ux {
        border-color: rgba(255,255,255,.12) !important;
      }

      /* Inputs */
      html[data-theme="dark"] .swagger-ui input[type=text],
      html[data-theme="dark"] .swagger-ui input[type=password],
      html[data-theme="dark"] .swagger-ui input[type=search],
      html[data-theme="dark"] .swagger-ui select,
      html[data-theme="dark"] .swagger-ui textarea {
        background: rgba(255,255,255,.06) !important;
        color: var(--fg) !important;
        border-color: rgba(255,255,255,.16) !important;
      }
      html[data-theme="dark"] .swagger-ui input::placeholder,
      html[data-theme="dark"] .swagger-ui textarea::placeholder { color: rgba(229,231,235,.55) !important; }

      /* Buttons */
      html[data-theme="dark"] .swagger-ui .btn,
      html[data-theme="dark"] .swagger-ui .btn.execute,
      html[data-theme="dark"] .swagger-ui .btn.authorize,
      html[data-theme="dark"] .swagger-ui .btn.try-out__btn,
      html[data-theme="dark"] .swagger-ui .btn.cancel {
        background: rgba(255,255,255,.07) !important;
        color: var(--fg) !important;
        border-color: rgba(255,255,255,.16) !important;
      }
      html[data-theme="dark"] .swagger-ui .btn:hover { background: rgba(255,255,255,.11) !important; }

      /* Code blocks */
      html[data-theme="dark"] .swagger-ui pre,
      html[data-theme="dark"] .swagger-ui code,
      html[data-theme="dark"] .swagger-ui .microlight {
        background: rgba(255,255,255,.06) !important;
        color: #e5e7eb !important;
        border-color: rgba(255,255,255,.12) !important;
      }

      /* Opblock headers (keep method colors but readable text) */
      html[data-theme="dark"] .swagger-ui .opblock-summary,
      html[data-theme="dark"] .swagger-ui .opblock-summary-method,
      html[data-theme="dark"] .swagger-ui .opblock-summary-path,
      html[data-theme="dark"] .swagger-ui .opblock-summary-description {
        color: var(--fg) !important;
      }
      html[data-theme="dark"] .swagger-ui .opblock-summary-path { color: #e2e8f0 !important; }
      html[data-theme="dark"] .swagger-ui .opblock-summary-description { color: #cbd5e1 !important; }
      html[data-theme="dark"] .swagger-ui .opblock-tag { background: transparent !important; }

      /* Icons (many are inline SVG) */
      html[data-theme="dark"] .swagger-ui svg,
      html[data-theme="dark"] .swagger-ui svg path,
      html[data-theme="dark"] .swagger-ui svg polygon,
      html[data-theme="dark"] .swagger-ui svg circle {
        fill: currentColor !important;
      }
    </style>
  </head>
  <body>
    <div class="sarbon-docs-hint" id="sarbon-docs-hint">
      Заголовки задаются в кнопке <b>Authorize</b>: X-Device-Type (web / ios / android), X-Language (ru / uz / en / tr / zh), X-Client-Token. Один раз выбранные значения действуют для <b>всех разделов</b> (Drivers, Cargo Manager, Driver Manager, Admin, Chat, Company). Можно выбрать любой язык и любой тип устройства.
      Группа <b>Driver Manager</b> помечена как отдельный блок API в верхнем меню.
    </div>
    <div class="sarbon-topmenu" role="navigation" aria-label="API groups">
      <div class="brand">Sarbon API</div>
      <button class="btn" data-group="drivers">Drivers Mobile</button>
      <button class="btn" data-group="cargo">Cargo Manager</button>
      <button class="btn" data-group="dispatchers">Driver Manager API</button>
      <button class="btn" data-group="company">Company</button>
      <button class="btn" data-group="admin">Admin</button>
      <button class="btn" data-group="chat">Chat</button>
      <button class="btn" data-group="reference">Reference</button>
      <a class="btn" href="/docs/flow" style="text-decoration:none;display:inline-flex;align-items:center;gap:4px;background:#0d9488;color:#fff;border-color:#0d9488">&#128396; Белая доска</a>
      <a class="btn" href="/ws-test" style="text-decoration:none;display:inline-flex;align-items:center;gap:4px;background:#0f172a;color:#fff;border-color:#0f172a">&#9889; WS Test</a>
      <a class="btn" href="/calls-test" style="text-decoration:none;display:inline-flex;align-items:center;gap:4px;background:#7c3aed;color:#fff;border-color:#7c3aed">&#128222; Calls Test Lab</a>
      <a class="btn" href="/calls-webrtc" style="text-decoration:none;display:inline-flex;align-items:center;gap:4px;background:#1d4ed8;color:#fff;border-color:#1d4ed8">&#127908; WebRTC Call</a>
      <a class="btn" href="/terminal" style="text-decoration:none;display:inline-flex;align-items:center;gap:4px;background:#065f46;color:#fff;border-color:#065f46">&#128187; Terminal</a>
      <button class="sarbon-theme-toggle" id="sarbon-theme-toggle" type="button" aria-label="Toggle theme" aria-pressed="false">
        <span class="icons" aria-hidden="true"><span class="sun">☀</span><span class="moon">🌙</span></span>
        <span class="label" id="sarbon-theme-label">Light</span>
        <span class="switch" aria-hidden="true"><span class="knob"></span></span>
      </button>
    </div>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
    <script>
      window.onload = () => {
        const LS_PREFIX = 'sarbon_auth_';
        const DOCS_GROUP_KEY = LS_PREFIX + 'docs_group';
        const THEME_KEY = LS_PREFIX + 'theme';
          const K = {
          DeviceTypeHeader: LS_PREFIX + 'DeviceTypeHeader',
          LanguageHeader: LS_PREFIX + 'LanguageHeader',
          ClientTokenHeader: LS_PREFIX + 'ClientTokenHeader',
          UserTokenHeader: LS_PREFIX + 'UserTokenHeader',
          UserIDHeader: LS_PREFIX + 'UserIDHeader',
        };

        function getLS(key, defVal) {
          const v = localStorage.getItem(key);
          if (v === null || v === undefined || v === '') return defVal;
          return v;
        }
        function setLS(key, val) {
          try { localStorage.setItem(key, val); } catch(e) {}
        }

        // Theme (persist to localStorage)
        function applyTheme(theme) {
          const t = (theme === 'dark') ? 'dark' : 'light';
          document.documentElement.setAttribute('data-theme', t);
          const label = document.getElementById('sarbon-theme-label');
          if (label) label.textContent = (t === 'dark') ? 'Dark' : 'Light';
          const btn = document.getElementById('sarbon-theme-toggle');
          if (btn) btn.setAttribute('aria-pressed', t === 'dark' ? 'true' : 'false');
        }
        function getInitialTheme() {
          const saved = getLS(THEME_KEY, '');
          if (saved === 'dark' || saved === 'light') return saved;
          try {
            if (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) return 'dark';
          } catch(e) {}
          return 'light';
        }
        const initialTheme = getInitialTheme();
        applyTheme(initialTheme);

        const SarbonAuthPlugin = function() {
          return {
            wrapComponents: {
              apiKeyAuth: function(Original, system) {
                return function(props) {
                  const React = system.React;
                  const Row = system.getComponent('Row');
                  const Col = system.getComponent('Col');
                  const Markdown = system.getComponent('Markdown', true);

                  const name = props.name;
                  const schema = props.schema;
                  const authorized = props.authorized;

                  const currentVal = (authorized && authorized.getIn([name, 'value'])) || '';

                  const onChangeProxy = function(value) {
                    setLS(K[name] || (LS_PREFIX + name), value);
                    if (props.onChange) {
                      props.onChange({ name: name, schema: schema, value: value });
                    }
                  };

                  // Render select for device/language — same options in all sections (any language, any device).
                  if (name === 'DeviceTypeHeader' || name === 'LanguageHeader') {
                    const options = (name === 'DeviceTypeHeader')
                      ? ['web', 'ios', 'android']
                      : ['ru', 'uz', 'en', 'tr', 'zh'];
                    const label = (name === 'DeviceTypeHeader')
                      ? 'X-Device-Type (web | ios | android)'
                      : 'X-Language (ru | uz | en | tr | zh)';
                    var value = (currentVal && currentVal.toString && currentVal.toString().trim()) || getLS(K[name], '');
                    if (value === '' || options.indexOf(value) === -1) value = options[0];

                    return React.createElement(
                      'div',
                      { className: 'auth-container' },
                      React.createElement('h4', null, label),
                      React.createElement(
                        Row,
                        null,
                        React.createElement(
                          Col,
                          null,
                          React.createElement(
                            'select',
                            {
                              value: value,
                              onChange: function(e) { onChangeProxy(e.target.value); },
                              style: { width: '100%', padding: '8px', borderRadius: '4px', maxWidth: '280px' }
                            },
                            options.map(function(o) {
                              return React.createElement('option', { key: o, value: o }, o);
                            })
                          ),
                          React.createElement('div', { style: { marginTop: '6px', fontSize: '12px', color: '#64748b' } },
                            'Применяется ко всем разделам API.'
                          )
                        )
                      )
                    );
                  }

                  // For normal apiKey inputs: keep original but persist to localStorage on change
                  const originalOnChange = props.onChange;
                  const nextProps = Object.assign({}, props, {
                    onChange: function(newState) {
                      if (newState && newState.name) {
                        setLS(K[newState.name] || (LS_PREFIX + newState.name), (newState.value || '').toString());
                      }
                      if (originalOnChange) originalOnChange(newState);
                    }
                  });

                  return React.createElement(Original, nextProps);
                }
              }
            }
          }
        };

        function tagName(t) {
          if (!t) return '';
          if (typeof t === 'string') return t;
          if (t.get && typeof t.get === 'function') return t.get('name') || '';
          if (t.name) return t.name;
          return String(t);
        }

        // Скрыты на вкладке Drivers Mobile (пошагово возвращаем в isTagInGroup + applyGroupFilter).
        const DRIVERS_MOBILE_HIDDEN_TAGS = new Set([
          'Drivers / Driver invitations',
          'Drivers / My dispatchers',
          'Drivers / Invite dispatcher',
          'Drivers / Offers',
          'Drivers / Offer decision flow',
        ]);
        // Do not hide POST /v1/driver/dispatchers/.../rating (cargo manager rating) with a blanket dispatchers prefix.
        function hideDriverMobilePath(pathText) {
          if (!pathText) return false;
          if (pathText.indexOf('/v1/driver/driver-invitations') !== -1) return true;
          if (pathText.indexOf('/v1/driver/dispatcher-invitations') !== -1) return true;
          if (pathText.indexOf('/v1/driver/dispatchers/') !== -1 && pathText.indexOf('/rating') !== -1) return false;
          if (pathText.indexOf('/v1/driver/dispatchers') !== -1) return true;
          return false;
        }

        const TAG_ORDER = [
          'Drivers / Auth',
          'Drivers / Registration',
          'Drivers / KYC',
          'Drivers / Profile',
          'Drivers / Offer minimal',
          'Drivers / Cargo view',
          'Drivers / Cargo likes',
          'Drivers / Dispatcher likes',
          'Drivers / Offers',
          'Drivers / Trips',
          'Notifications and ratings / Driver',
          'SSE / Driver — Trip notifications',
          'SSE / Driver — Trip status',
          'Cargo Manager',
          'Cargo Manager / Auth',
          'Cargo Manager / Registration',
          'Cargo Manager / Profile',
          'Cargo Manager / Cargo CRUD',
          'Cargo Manager / View Cargo',
          'Cargo Manager / Offers',
          'Cargo Manager / Trips',
          'Notifications and ratings / Cargo manager',
          'SSE / Cargo manager — Trip notifications',
          'SSE / Cargo manager — Trip status',
          'Cargo Manager / Cargo likes',
          'Cargo Manager / Driver likes',
          'Driver Manager / Auth',
          'Driver Manager / Registration',
          'Driver Manager / Profile',
          'Driver Manager / Cargo likes',
          'Driver Manager / Driver likes',
          'Driver Manager / Connect offers',
          'Driver Manager / Drivers catalog',
          'Driver Manager / My drivers',
          'Driver Manager / Notifications',
          'Unified notifications SSE',
          'Driver Manager / Offers',
          'Driver Manager / Trips',
          'Driver Manager / Completion & ratings flow',
          'Admin / Auth',
          'Admin / Companies',
          'Company / Dispatcher (приглашённый)',
          'Chat',
          'Calls (Voice)',
          'Calls (Voice) / Test Lab',
          'Company',
          'Reference',
          'Reference / Drivers',
          'Reference / Cargo',
          'Reference / Company',
          'Reference / Admin',
          'Reference / Dispatchers',
        ];
        function tagIndex(t) {
          const n = tagName(t);
          const i = TAG_ORDER.indexOf(n);
          return i === -1 ? 999 : i;
        }

        function normalizeGroup(g) {
          if (g === 'drivers' || g === 'dispatchers' || g === 'admin' || g === 'cargo' || g === 'chat' || g === 'company' || g === 'reference') return g;
          return 'drivers';
        }

        function getInitialGroup() {
          // Allow quick switching by query string (?group=drivers|dispatchers|admin)
          try {
            const qs = new URLSearchParams(window.location.search || '');
            const qg = qs.get('group');
            if (qg) return normalizeGroup(qg);
          } catch(e) {}
          return normalizeGroup(getLS(DOCS_GROUP_KEY, 'drivers'));
        }

        function isTagInGroup(tag, group) {
          if (!tag) return false;
          var t = (typeof tag === 'string' ? tag : '').trim();
          var tLower = t.toLowerCase();
          if (group === 'drivers') {
            if (t.startsWith('Notifications and ratings / Driver')) return true;
            if (t.startsWith('SSE / Driver')) return true;
            if (!t.startsWith('Drivers /')) return false;
            return !DRIVERS_MOBILE_HIDDEN_TAGS.has(t);
          }
          if (group === 'dispatchers') {
            const allowedDispatcherTags = new Set([
              'Driver Manager / Auth',
              'Driver Manager / Registration',
              'Driver Manager / Profile',
              'Driver Manager / Cargo likes',
              'Driver Manager / Driver likes',
              'Driver Manager / Connect offers',
              'Driver Manager / Drivers catalog',
              'Driver Manager / My drivers',
              'Driver Manager / Notifications',
              'Unified notifications SSE',
              'Driver Manager / Offers',
              'Driver Manager / Trips',
              'Driver Manager / Completion & ratings flow',
            ]);
            return allowedDispatcherTags.has(t);
          }
          if (group === 'admin') return t.startsWith('Admin /');
          if (group === 'cargo') {
            if (t.startsWith('Notifications and ratings / Cargo manager')) return true;
            if (t.startsWith('SSE / Cargo manager')) return true;
            if (t === 'Unified notifications SSE') return true;
            return t === 'Cargo Manager' || t.startsWith('Cargo Manager /');
          }
          if (group === 'chat') return t.startsWith('Chat') || t.startsWith('Calls');
          if (group === 'company') return t.startsWith('Company') || tLower === 'company' || tLower.startsWith('company ');
          if (group === 'reference') return t.startsWith('Reference');
          return true;
        }

        function getSectionTagName(sec) {
          const tagBtn = sec.querySelector('.opblock-tag');
          if (!tagBtn) return (sec.querySelector('h3') && sec.querySelector('h3').textContent) || '';
          var name = tagBtn.getAttribute('data-tag');
          if (name) return name.trim();
          return (tagBtn.textContent || '').trim().replace(/\s*\(\d+\)\s*$/, '');
        }

        function normalizeSwaggerPath(s) {
          if (!s) return '';
          return String(s).replace(/\s+/g, '').split('?')[0];
        }

        function applyGroupFilter(group) {
          const g = normalizeGroup(group);
          const sections = document.querySelectorAll('#swagger-ui .opblock-tag-section');
          sections.forEach((sec) => {
            const t = getSectionTagName(sec);
            sec.style.display = isTagInGroup(t, g) ? '' : 'none';
          });
          // Доп. скрытие по пути: эндпоинты рейсов / приглашений / диспетчеров не показываем в Drivers Mobile.
          document.querySelectorAll('#swagger-ui .opblock').forEach((op) => {
            if (g !== 'drivers') {
              op.style.display = '';
              return;
            }
            const pathEl = op.querySelector('.opblock-summary-path');
            const pathText = normalizeSwaggerPath(pathEl && pathEl.textContent);
            const hideByPath = hideDriverMobilePath(pathText);
            op.style.display = hideByPath ? 'none' : '';
          });
        }

        function setActiveMenu(group) {
          document.querySelectorAll('.sarbon-topmenu .btn[data-group]').forEach((b) => {
            b.classList.toggle('active', b.getAttribute('data-group') === group);
          });
        }

        function setGroup(group) {
          const g = normalizeGroup(group);
          setLS(DOCS_GROUP_KEY, g);
          setActiveMenu(g);
          applyGroupFilter(g);
        }

        window.ui = SwaggerUIBundle({
          url: '/openapi.yaml',
          dom_id: '#swagger-ui',
          deepLinking: true,
          persistAuthorization: true,
          docExpansion: 'none',
          defaultModelsExpandDepth: -1,
          tagsSorter: (a, b) => {
            const ai = tagIndex(a);
            const bi = tagIndex(b);
            if (ai !== bi) return ai - bi;
            const an = tagName(a).toLowerCase();
            const bn = tagName(b).toLowerCase();
            return an.localeCompare(bn);
          },
          plugins: [SarbonAuthPlugin],
          requestInterceptor: (req) => {
            // Failsafe: always inject required base headers from localStorage.
            // This guarantees headers are sent even if Swagger UI didn't apply Authorize properly.
            req.headers = req.headers || {};
            const d = getLS(K.DeviceTypeHeader, 'web');
            const l = getLS(K.LanguageHeader, 'ru');
            const ct = getLS(K.ClientTokenHeader, '');
            const ut = getLS(K.UserTokenHeader, '');
            if (d) req.headers['X-Device-Type'] = d;
            if (l) req.headers['X-Language'] = l;
            if (ct) req.headers['X-Client-Token'] = ct;
            if (ut) req.headers['X-User-Token'] = ut;
            var uid = getLS(K.UserIDHeader, '');
            if (uid) req.headers['X-User-ID'] = uid;
            return req;
          }
        });

        // Menu bindings + persist group selection
        const initialGroup = getInitialGroup();
        setActiveMenu(initialGroup);
        document.querySelectorAll('.sarbon-topmenu .btn[data-group]').forEach((btn) => {
          btn.addEventListener('click', () => setGroup(btn.getAttribute('data-group')));
        });
        const themeBtn = document.getElementById('sarbon-theme-toggle');
        if (themeBtn) {
          themeBtn.addEventListener('click', () => {
            const cur = document.documentElement.getAttribute('data-theme') || 'light';
            const next = (cur === 'dark') ? 'light' : 'dark';
            setLS(THEME_KEY, next);
            applyTheme(next);
          });
        }

        // Swagger UI renders async; re-apply filter when DOM changes.
        let filterTimer = null;
        const mo = new MutationObserver(() => {
          if (filterTimer) clearTimeout(filterTimer);
          filterTimer = setTimeout(() => applyGroupFilter(getLS(DOCS_GROUP_KEY, initialGroup)), 30);
        });
        const root = document.getElementById('swagger-ui');
        if (root) mo.observe(root, { childList: true, subtree: true });

        // Auto-apply defaults from localStorage on page load (so refresh keeps headers)
        try { window.ui.preauthorizeApiKey('DeviceTypeHeader', getLS(K.DeviceTypeHeader, 'web')); } catch(e) {}
        try { window.ui.preauthorizeApiKey('LanguageHeader', getLS(K.LanguageHeader, 'ru')); } catch(e) {}
        try { window.ui.preauthorizeApiKey('ClientTokenHeader', getLS(K.ClientTokenHeader, '')); } catch(e) {}
        const ut = getLS(K.UserTokenHeader, '');
        if (ut) {
          try { window.ui.preauthorizeApiKey('UserTokenHeader', ut); } catch(e) {}
        }
        try { window.ui.preauthorizeApiKey('UserIDHeader', getLS(K.UserIDHeader, '')); } catch(e) {}

        // Initial filter apply (after first paint)
        setTimeout(() => setGroup(initialGroup), 0);
      };
    </script>
  </body>
</html>`

const wsTestHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width,initial-scale=1"/>
  <title>Sarbon — WebSocket Test</title>
  <style>
    *{box-sizing:border-box;margin:0;padding:0}
    body{font-family:ui-sans-serif,system-ui,-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;background:#f8fafc;color:#1e293b;padding:24px}
    .container{max-width:860px;margin:0 auto}
    h1{font-size:22px;margin-bottom:6px}
    .subtitle{font-size:13px;color:#64748b;margin-bottom:20px}
    .subtitle a{color:#3b82f6;text-decoration:none}
    .subtitle a:hover{text-decoration:underline}
    .card{background:#fff;border:1px solid #e2e8f0;border-radius:12px;padding:20px;margin-bottom:16px}
    .card h2{font-size:15px;margin-bottom:12px;color:#334155}
    label{display:block;font-size:13px;font-weight:500;color:#475569;margin-bottom:4px}
    input,select{width:100%;padding:8px 10px;border:1px solid #cbd5e1;border-radius:8px;font-size:14px;margin-bottom:10px;outline:none;transition:border .15s}
    input:focus,select:focus{border-color:#3b82f6}
    .row{display:flex;gap:12px;flex-wrap:wrap}
    .row>div{flex:1;min-width:180px}
    .actions{display:flex;gap:10px;margin-top:6px;flex-wrap:wrap}
    button{padding:9px 18px;border:none;border-radius:8px;font-size:14px;font-weight:500;cursor:pointer;transition:background .15s,box-shadow .15s}
    .btn-connect{background:#16a34a;color:#fff}
    .btn-connect:hover{background:#15803d}
    .btn-disconnect{background:#dc2626;color:#fff}
    .btn-disconnect:hover{background:#b91c1c}
    .btn-send{background:#2563eb;color:#fff}
    .btn-send:hover{background:#1d4ed8}
    .btn-clear{background:#e2e8f0;color:#334155}
    .btn-clear:hover{background:#cbd5e1}
    .status{display:inline-block;padding:4px 10px;border-radius:999px;font-size:12px;font-weight:600;margin-left:8px}
    .status-closed{background:#fee2e2;color:#991b1b}
    .status-open{background:#dcfce7;color:#166534}
    .status-connecting{background:#fef3c7;color:#92400e}
    #log{background:#0f172a;color:#e2e8f0;border-radius:10px;padding:14px;font-family:'Fira Code',Consolas,monospace;font-size:13px;line-height:1.6;height:340px;overflow-y:auto;white-space:pre-wrap;word-break:break-all}
    .log-in{color:#4ade80}
    .log-out{color:#60a5fa}
    .log-err{color:#f87171}
    .log-info{color:#a78bfa}
    .send-row{display:flex;gap:8px;margin-top:8px}
    .send-row input{flex:1;margin-bottom:0}
  </style>
</head>
<body>
<div class="container">
  <h1>Sarbon WebSocket Test</h1>
  <p class="subtitle"><a href="/docs">← Swagger Docs</a> &nbsp;|&nbsp; Chat & Calls real-time testing</p>

  <div class="card">
    <h2>Connection</h2>
    <div class="row">
      <div>
        <label>Host (auto-detected)</label>
        <input id="host" placeholder="ws://localhost:8080"/>
      </div>
      <div>
        <label>Auth method</label>
        <select id="authMethod">
          <option value="token">JWT token (query ?token=)</option>
        </select>
      </div>
    </div>
    <div class="row">
      <div>
        <label>Token / User ID</label>
        <input id="authValue" placeholder="paste JWT or UUID"/>
      </div>
    </div>
    <div class="row">
      <div>
        <label>X-Device-Type</label>
        <select id="deviceType"><option>ios</option><option>android</option><option selected>web</option></select>
      </div>
      <div>
        <label>X-Language</label>
        <select id="lang"><option>ru</option><option>uz</option><option selected>en</option><option>tr</option><option>zh</option></select>
      </div>
      <div>
        <label>X-Client-Token</label>
        <input id="clientToken" placeholder="client token"/>
      </div>
    </div>
    <div class="actions">
      <button class="btn-connect" onclick="doConnect()">Connect</button>
      <button class="btn-disconnect" onclick="doDisconnect()">Disconnect</button>
      <span id="statusBadge" class="status status-closed">CLOSED</span>
    </div>
  </div>

  <div class="card">
    <h2>Send message</h2>
    <div class="send-row">
      <input id="msgInput" placeholder='{"type":"typing","data":{"conversation_id":"..."}}' onkeydown="if(event.key==='Enter')doSend()"/>
      <button class="btn-send" onclick="doSend()">Send</button>
    </div>
    <div style="margin-top:10px">
      <label>Quick templates</label>
      <div class="actions">
        <button class="btn-clear" onclick="setMsg('typing')">typing</button>
        <button class="btn-clear" onclick="setMsg('webrtc.offer')">webrtc.offer</button>
        <button class="btn-clear" onclick="setMsg('webrtc.answer')">webrtc.answer</button>
        <button class="btn-clear" onclick="setMsg('webrtc.ice')">webrtc.ice</button>
        <button class="btn-clear" onclick="setMsg('call.end')">call.end</button>
      </div>
    </div>
  </div>

  <div class="card">
    <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px">
      <h2 style="margin-bottom:0">Log</h2>
      <button class="btn-clear" onclick="clearLog()">Clear</button>
    </div>
    <div id="log"></div>
  </div>
</div>

<script>
let ws = null;
const $ = id => document.getElementById(id);

(function initHost(){
  const proto = location.protocol === 'https:' ? 'wss://' : 'ws://';
  $('host').value = proto + location.host;
})();

function badge(state) {
  const b = $('statusBadge');
  b.textContent = state;
  b.className = 'status status-' + state.toLowerCase();
}

function log(cls, prefix, text) {
  const el = $('log');
  const ts = new Date().toLocaleTimeString();
  const line = document.createElement('div');
  line.className = cls;
  line.textContent = '[' + ts + '] ' + prefix + ' ' + text;
  el.appendChild(line);
  el.scrollTop = el.scrollHeight;
}

function doConnect() {
  if (ws && ws.readyState <= 1) { doDisconnect(); }
  const host = $('host').value.replace(/\/+$/, '');
  const method = $('authMethod').value;
  const val = $('authValue').value.trim();
  if (!val) { log('log-err', 'ERR', 'token is required'); return; }

  const dt = $('deviceType').value;
  const ln = $('lang').value;
  const ct = $('clientToken').value.trim();

  let url = host + '/v1/chat/ws?' + method + '=' + encodeURIComponent(val);
  url += '&device_type=' + dt + '&language=' + ln;
  if (ct) url += '&client_token=' + encodeURIComponent(ct);

  log('log-info', 'SYS', 'connecting to ' + url);
  badge('CONNECTING');

  ws = new WebSocket(url);
  ws.onopen = () => { badge('OPEN'); log('log-info', 'SYS', 'connected'); };
  ws.onclose = (e) => { badge('CLOSED'); log('log-info', 'SYS', 'closed code=' + e.code + ' reason=' + (e.reason||'-')); ws = null; };
  ws.onerror = (e) => { log('log-err', 'ERR', 'websocket error'); };
  ws.onmessage = (e) => {
    let text = e.data;
    try { text = JSON.stringify(JSON.parse(e.data), null, 2); } catch(_){}
    log('log-in', '← IN', text);
  };
}

function doDisconnect() {
  if (ws) { ws.close(); ws = null; }
  badge('CLOSED');
}

function doSend() {
  const msg = $('msgInput').value.trim();
  if (!msg) return;
  if (!ws || ws.readyState !== 1) { log('log-err', 'ERR', 'not connected'); return; }
  ws.send(msg);
  log('log-out', '→ OUT', msg);
}

function setMsg(type) {
  const templates = {
    'typing': '{"type":"typing","data":{"conversation_id":"CONV_ID"}}',
    'webrtc.offer': '{"type":"webrtc.offer","data":{"call_id":"CALL_ID","payload":{"sdp":"..."}}}',
    'webrtc.answer': '{"type":"webrtc.answer","data":{"call_id":"CALL_ID","payload":{"sdp":"..."}}}',
    'webrtc.ice': '{"type":"webrtc.ice","data":{"call_id":"CALL_ID","payload":{"candidate":"..."}}}',
    'call.end': '{"type":"call.end","data":{"call_id":"CALL_ID"}}'
  };
  $('msgInput').value = templates[type] || '';
  $('msgInput').focus();
}

function clearLog() { $('log').innerHTML = ''; }
</script>
</body>
</html>`

