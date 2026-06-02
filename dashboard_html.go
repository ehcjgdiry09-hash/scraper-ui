package main

var dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Key Pool</title>
<style>
*,*::before,*::after{margin:0;padding:0;box-sizing:border-box}
:root{--bg:#0a0a0f;--card:#12121a;--border:#1e1e2e;--accent:#6c5ce7;--accent-hover:#7d6ff0;--text:#e0e0e8;--text-dim:#6b6b80;--green:#2ed573;--red:#ff4757;--yellow:#ffa502;--sidebar-w:240px;--radius:8px}
html,body{height:100%;font-family:'Inter',system-ui,-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;background:var(--bg);color:var(--text);overflow:hidden}
a{color:var(--accent);text-decoration:none}

/* ── Login ── */
#loginView{display:none;position:fixed;inset:0;z-index:1000;background:var(--bg);align-items:center;justify-content:center}
#loginView.active{display:flex}
.login-box{background:var(--card);border:1px solid var(--border);border-radius:16px;padding:48px 40px;width:380px;max-width:90vw;text-align:center}
.login-box .logo{margin-bottom:28px}
.login-box .logo svg{width:48px;height:48px;color:var(--accent)}
.login-box h1{font-size:22px;font-weight:700;margin-bottom:6px;letter-spacing:-.3px}
.login-box .sub{color:var(--text-dim);font-size:13px;margin-bottom:28px}
.login-box input{width:100%;padding:12px 16px;border:1px solid var(--border);border-radius:var(--radius);background:var(--bg);color:var(--text);font-size:14px;outline:none;transition:border-color .2s;margin-bottom:12px}
.login-box input:focus{border-color:var(--accent)}
.login-box button{width:100%;padding:12px;background:var(--accent);color:#fff;border:none;border-radius:var(--radius);font-size:14px;font-weight:600;cursor:pointer;transition:background .2s}
.login-box button:hover{background:var(--accent-hover)}
.login-error{color:var(--red);font-size:13px;margin-top:12px;display:none}

/* ── Layout ── */
#appView{display:none;height:100vh}
#appView.active{display:flex}
.sidebar{width:var(--sidebar-w);background:var(--card);border-right:1px solid var(--border);display:flex;flex-direction:column;flex-shrink:0;overflow-y:auto}
.sidebar-brand{padding:24px 20px 20px;display:flex;align-items:center;gap:10px;font-weight:700;font-size:16px;border-bottom:1px solid var(--border)}
.sidebar-brand svg{width:28px;height:28px;color:var(--accent);flex-shrink:0}
.sidebar-nav{padding:12px 10px;flex:1}
.nav-item{display:flex;align-items:center;gap:10px;padding:10px 14px;border-radius:var(--radius);color:var(--text-dim);cursor:pointer;font-size:14px;font-weight:500;transition:all .15s;margin-bottom:2px;user-select:none}
.nav-item:hover{background:rgba(108,92,231,.08);color:var(--text)}
.nav-item.active{background:rgba(108,92,231,.14);color:var(--accent)}
.nav-item svg{width:18px;height:18px;flex-shrink:0}
.sidebar-footer{padding:14px 20px;border-top:1px solid var(--border);font-size:12px;color:var(--text-dim)}
.sidebar-footer a{cursor:pointer}

/* ── Main ── */
.main{flex:1;overflow-y:auto;padding:28px 32px}
.page{display:none}
.page.active{display:block}
.page-title{font-size:22px;font-weight:700;margin-bottom:24px;letter-spacing:-.3px}

/* ── Stats Cards ── */
.stats-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(200px,1fr));gap:16px;margin-bottom:28px}
.stat-card{background:var(--card);border:1px solid var(--border);border-radius:var(--radius);padding:20px}
.stat-card .stat-label{font-size:12px;color:var(--text-dim);text-transform:uppercase;letter-spacing:.5px;margin-bottom:8px;display:flex;align-items:center;gap:6px}
.stat-card .stat-label svg{width:14px;height:14px}
.stat-card .stat-value{font-size:28px;font-weight:700;letter-spacing:-.5px}
.stat-card .stat-value.green{color:var(--green)}
.stat-card .stat-value.red{color:var(--red)}
.stat-card .stat-value.yellow{color:var(--yellow)}
.stat-card .stat-value.accent{color:var(--accent)}

/* ── Activity Feed ── */
.feed-card{background:var(--card);border:1px solid var(--border);border-radius:var(--radius);overflow:hidden}
.feed-header{padding:16px 20px;border-bottom:1px solid var(--border);font-weight:600;font-size:14px;display:flex;align-items:center;gap:8px}
.feed-header svg{width:16px;height:16px;color:var(--accent)}
.feed-list{max-height:360px;overflow-y:auto}
.feed-item{padding:12px 20px;border-bottom:1px solid var(--border);font-size:13px;display:flex;align-items:center;gap:10px;transition:background .1s}
.feed-item:last-child{border-bottom:none}
.feed-item:hover{background:rgba(108,92,231,.04)}
.feed-item .feed-dot{width:8px;height:8px;border-radius:50%;flex-shrink:0}
.feed-item .feed-dot.valid{background:var(--green)}
.feed-item .feed-dot.invalid{background:var(--red)}
.feed-item .feed-dot.scan{background:var(--accent)}
.feed-item .feed-time{margin-left:auto;color:var(--text-dim);font-size:11px;flex-shrink:0;white-space:nowrap}
.feed-empty{padding:32px;text-align:center;color:var(--text-dim);font-size:13px}

/* ── Table ── */
.table-wrap{background:var(--card);border:1px solid var(--border);border-radius:var(--radius);overflow:hidden}
.table-toolbar{padding:16px 20px;border-bottom:1px solid var(--border);display:flex;align-items:center;gap:12px;flex-wrap:wrap}
.search-box{display:flex;align-items:center;gap:8px;background:var(--bg);border:1px solid var(--border);border-radius:var(--radius);padding:8px 12px;flex:1;max-width:340px}
.search-box svg{width:16px;height:16px;color:var(--text-dim);flex-shrink:0}
.search-box input{border:none;background:transparent;color:var(--text);font-size:13px;outline:none;width:100%}
.provider-tabs{display:flex;gap:6px;flex-wrap:wrap}
.provider-tab{padding:6px 14px;border-radius:20px;font-size:12px;font-weight:600;cursor:pointer;background:var(--bg);border:1px solid var(--border);color:var(--text-dim);transition:all .15s;user-select:none}
.provider-tab:hover{border-color:var(--accent);color:var(--text)}
.provider-tab.active{background:var(--accent);border-color:var(--accent);color:#fff}
.btn{padding:8px 16px;border-radius:var(--radius);font-size:13px;font-weight:600;cursor:pointer;border:1px solid var(--border);background:var(--bg);color:var(--text);transition:all .15s;display:inline-flex;align-items:center;gap:6px;user-select:none}
.btn:hover{border-color:var(--accent);color:var(--accent)}
.btn svg{width:14px;height:14px}
.btn-sm{padding:5px 10px;font-size:12px}
.btn-danger{color:var(--red)}
.btn-danger:hover{border-color:var(--red);color:var(--red);background:rgba(255,71,87,.08)}
.btn-accent{background:var(--accent);border-color:var(--accent);color:#fff}
.btn-accent:hover{background:var(--accent-hover);border-color:var(--accent-hover);color:#fff}
table{width:100%;border-collapse:collapse}
thead th{padding:10px 16px;text-align:left;font-size:11px;font-weight:600;color:var(--text-dim);text-transform:uppercase;letter-spacing:.5px;background:rgba(108,92,231,.04);border-bottom:1px solid var(--border)}
tbody tr{border-bottom:1px solid var(--border);transition:background .1s}
tbody tr:last-child{border-bottom:none}
tbody tr:hover{background:rgba(108,92,231,.04)}
tbody td{padding:10px 16px;font-size:13px}
.badge{display:inline-block;padding:3px 10px;border-radius:12px;font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:.3px}
.badge-valid{background:rgba(46,213,115,.12);color:var(--green)}
.badge-invalid{background:rgba(255,71,87,.12);color:var(--red)}
.badge-unchecked{background:rgba(255,165,2,.12);color:var(--yellow)}
.key-val{font-family:'SF Mono','Fira Code',monospace;font-size:12px;color:var(--text-dim);max-width:220px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.key-actions{display:flex;gap:4px}
.key-actions button{background:none;border:none;cursor:pointer;padding:4px;border-radius:4px;color:var(--text-dim);transition:all .15s;display:flex;align-items:center}
.key-actions button:hover{background:rgba(108,92,231,.12);color:var(--accent)}
.key-actions button.danger:hover{background:rgba(255,71,87,.12);color:var(--red)}
.key-actions button svg{width:14px;height:14px}
.key-detail-row{background:rgba(108,92,231,.03)}
.key-detail-row td{padding:0 16px 12px 52px}
.key-detail{display:grid;grid-template-columns:repeat(auto-fill,minmax(180px,1fr));gap:8px 24px;font-size:12px}
.key-detail dt{color:var(--text-dim)}
.key-detail dd{color:var(--text);margin-top:2px;font-family:'SF Mono','Fira Code',monospace;word-break:break-all}
.table-scroll{overflow-x:auto}
.table-empty{padding:48px;text-align:center;color:var(--text-dim);font-size:14px}

/* ── API Proxy ── */
.proxy-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(380px,1fr));gap:16px}
.proxy-card{background:var(--card);border:1px solid var(--border);border-radius:var(--radius);overflow:hidden}
.proxy-card-header{padding:16px 20px;border-bottom:1px solid var(--border);display:flex;align-items:center;gap:10px;font-weight:600;font-size:14px}
.proxy-card-header svg{width:18px;height:18px;color:var(--accent)}
.proxy-card-header .count{margin-left:auto;font-size:11px;color:var(--text-dim);font-weight:400}
.proxy-endpoint{padding:12px 20px;border-bottom:1px solid var(--border);display:flex;align-items:center;gap:10px;font-size:13px}
.proxy-endpoint:last-child{border-bottom:none}
.proxy-method{font-size:10px;font-weight:700;padding:3px 8px;border-radius:4px;background:rgba(46,213,115,.12);color:var(--green);letter-spacing:.5px;flex-shrink:0}
.proxy-path{font-family:'SF Mono','Fira Code',monospace;font-size:12px;color:var(--text-dim);flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.proxy-endpoint .btn-sm{flex-shrink:0}

/* ── Settings ── */
.settings-section{background:var(--card);border:1px solid var(--border);border-radius:var(--radius);margin-bottom:16px;overflow:hidden}
.settings-header{padding:16px 20px;border-bottom:1px solid var(--border);font-weight:600;font-size:14px}
.settings-row{padding:16px 20px;border-bottom:1px solid var(--border);display:flex;align-items:center;justify-content:space-between;gap:16px}
.settings-row:last-child{border-bottom:none}
.settings-row .sr-label{font-size:14px}
.settings-row .sr-desc{font-size:12px;color:var(--text-dim);margin-top:2px}
.toggle{position:relative;width:44px;height:24px;flex-shrink:0}
.toggle input{opacity:0;width:0;height:0;position:absolute}
.toggle-slider{position:absolute;inset:0;background:var(--border);border-radius:12px;cursor:pointer;transition:background .2s}
.toggle-slider::before{content:'';position:absolute;left:3px;top:3px;width:18px;height:18px;background:#fff;border-radius:50%;transition:transform .2s}
.toggle input:checked+.toggle-slider{background:var(--accent)}
.toggle input:checked+.toggle-slider::before{transform:translateX(20px)}
select{background:var(--bg);border:1px solid var(--border);border-radius:var(--radius);color:var(--text);padding:8px 12px;font-size:13px;outline:none;cursor:pointer}
select:focus{border-color:var(--accent)}
.settings-input{background:var(--bg);border:1px solid var(--border);border-radius:var(--radius);color:var(--text);padding:8px 12px;font-size:13px;outline:none;width:200px}
.settings-input:focus{border-color:var(--accent)}

/* ── Scanner ── */
.scanner-stats{display:grid;grid-template-columns:repeat(auto-fill,minmax(180px,1fr));gap:16px;margin-bottom:24px}
.scanner-stat{background:var(--card);border:1px solid var(--border);border-radius:var(--radius);padding:16px;text-align:center}
.scanner-stat .ss-val{font-size:24px;font-weight:700}
.scanner-stat .ss-label{font-size:11px;color:var(--text-dim);text-transform:uppercase;letter-spacing:.5px;margin-top:4px}
.rl-bar{height:6px;background:var(--border);border-radius:3px;overflow:hidden;margin-top:12px}
.rl-bar-fill{height:100%;border-radius:3px;transition:width .5s}

/* ── Toast ── */
.toast-container{position:fixed;top:20px;right:20px;z-index:9999;display:flex;flex-direction:column;gap:8px}
.toast{background:var(--card);border:1px solid var(--border);border-radius:var(--radius);padding:14px 20px;font-size:13px;display:flex;align-items:center;gap:10px;box-shadow:0 8px 32px rgba(0,0,0,.4);animation:slideIn .3s ease;min-width:280px}
.toast svg{width:18px;height:18px;flex-shrink:0}
.toast.success svg{color:var(--green)}
.toast.error svg{color:var(--red)}
.toast.info svg{color:var(--accent)}
@keyframes slideIn{from{transform:translateX(100%);opacity:0}to{transform:translateX(0);opacity:1}}

/* ── Confirm Modal ── */
.modal-overlay{position:fixed;inset:0;background:rgba(0,0,0,.6);z-index:9000;display:flex;align-items:center;justify-content:center;animation:fadeIn .15s}
.modal-box{background:var(--card);border:1px solid var(--border);border-radius:12px;padding:28px;width:360px;max-width:90vw;text-align:center}
.modal-box h3{font-size:16px;margin-bottom:8px}
.modal-box p{font-size:13px;color:var(--text-dim);margin-bottom:20px}
.modal-actions{display:flex;gap:10px;justify-content:center}
@keyframes fadeIn{from{opacity:0}to{opacity:1}}

/* ── Responsive ── */
@media(max-width:768px){
.sidebar{width:60px}
.sidebar-brand span,.nav-item span,.sidebar-footer span{display:none}
.sidebar-brand{padding:20px 16px;justify-content:center}
.nav-item{justify-content:center;padding:10px}
.sidebar-footer{padding:14px 16px;text-align:center}
.main{padding:20px 16px}
.stats-grid{grid-template-columns:repeat(2,1fr)}
.proxy-grid{grid-template-columns:1fr}
}
@media(max-width:480px){
.stats-grid{grid-template-columns:1fr}
}

/* ── Scrollbar ── */
::-webkit-scrollbar{width:6px;height:6px}
::-webkit-scrollbar-track{background:transparent}
::-webkit-scrollbar-thumb{background:var(--border);border-radius:3px}
::-webkit-scrollbar-thumb:hover{background:#2e2e40}
</style>
</head>
<body>

<!-- Login View -->
<div id="loginView" class="active">
<div class="login-box">
<div class="logo"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg></div>
<h1>Key Pool</h1>
<p class="sub">Sign in to access the dashboard</p>
<input type="password" id="loginPass" placeholder="Password" autocomplete="current-password">
<button onclick="doLogin()">Sign In</button>
<div class="login-error" id="loginError">Invalid password</div>
</div>
</div>

<!-- App View -->
<div id="appView">
<!-- Sidebar -->
<aside class="sidebar">
<div class="sidebar-brand">
<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/></svg>
<span>Key Pool</span>
</div>
<nav class="sidebar-nav">
<div class="nav-item active" data-page="overview" onclick="switchPage('overview')">
<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 20V10"/><path d="M12 20V4"/><path d="M6 20v-6"/></svg>
<span>Overview</span>
</div>
<div class="nav-item" data-page="keys" onclick="switchPage('keys')">
<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/></svg>
<span>Keys</span>
</div>
<div class="nav-item" data-page="proxy" onclick="switchPage('proxy')">
<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="2" width="20" height="8" rx="2" ry="2"/><rect x="2" y="14" width="20" height="8" rx="2" ry="2"/><line x1="6" y1="6" x2="6.01" y2="6"/><line x1="6" y1="18" x2="6.01" y2="18"/></svg>
<span>API Proxy</span>
</div>
<div class="nav-item" data-page="scanner" onclick="switchPage('scanner')">
<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg>
<span>Scanner</span>
</div>
<div class="nav-item" data-page="settings" onclick="switchPage('settings')">
<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>
<span>Settings</span>
</div>
</nav>
<div class="sidebar-footer">
<a onclick="doLogout()" title="Sign out"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="width:16px;height:16px;display:inline;vertical-align:middle;margin-right:4px"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" y1="12" x2="9" y2="12"/></svg> <span>Sign out</span></a>
</div>
</aside>

<!-- Main Content -->
<div class="main">

<!-- Overview Page -->
<div class="page active" id="page-overview">
<div class="page-title">Overview</div>
<div class="stats-grid">
<div class="stat-card"><div class="stat-label"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/></svg> Total Keys</div><div class="stat-value accent" id="stat-total">0</div></div>
<div class="stat-card"><div class="stat-label"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/></svg> Valid</div><div class="stat-value green" id="stat-valid">0</div></div>
<div class="stat-card"><div class="stat-label"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg> Invalid</div><div class="stat-value red" id="stat-invalid">0</div></div>
<div class="stat-card"><div class="stat-label"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg> Unchecked</div><div class="stat-value yellow" id="stat-unchecked">0</div></div>
<div class="stat-card"><div class="stat-label"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg> Rate Limit</div><div class="stat-value" id="stat-ratelimit">--</div></div>
<div class="stat-card"><div class="stat-label"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg> Uptime</div><div class="stat-value" id="stat-uptime">--</div></div>
</div>
<div class="feed-card">
<div class="feed-header"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg> Live Activity</div>
<div class="feed-list" id="activityFeed"><div class="feed-empty">Waiting for activity...</div></div>
</div>
</div>

<!-- Keys Page -->
<div class="page" id="page-keys">
<div class="page-title">Keys</div>
<div class="table-wrap">
<div class="table-toolbar">
<div class="search-box"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg><input type="text" id="keySearch" placeholder="Search keys..." oninput="filterKeys()"></div>
<div class="provider-tabs" id="providerTabs"></div>
<button class="btn" onclick="showAddKeyModal()"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg> Add Key</button>
<button class="btn btn-accent" onclick="validateAll()"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg> Validate All</button>
</div>
<div class="table-scroll">
<table>
<thead><tr><th>Status</th><th>Provider</th><th>Key</th><th>Balance</th><th>Type</th><th>Last Checked</th><th>Actions</th></tr></thead>
<tbody id="keysBody"><tr><td colspan="7" class="table-empty">Loading keys...</td></tr></tbody>
</table>
</div>
</div>
</div>

<!-- API Proxy Page -->
<div class="page" id="page-proxy">
<div class="page-title">API Proxy</div>
<div class="proxy-grid" id="proxyGrid"></div>
</div>

<!-- Scanner Page -->
<div class="page" id="page-scanner">
<div class="page-title">Scanner</div>
<div class="scanner-stats">
<div class="scanner-stat"><div class="ss-val accent" id="scan-totalScanned">0</div><div class="ss-label">Commits Scanned</div></div>
<div class="scanner-stat"><div class="ss-val" id="scan-totalFound">0</div><div class="ss-label">Commits Found</div></div>
<div class="scanner-stat"><div class="ss-val" id="scan-totalRawHits">0</div><div class="ss-label">Raw Hits</div></div>
<div class="scanner-stat"><div class="ss-val green" id="scan-totalValid">0</div><div class="ss-label">Valid Keys</div></div>
<div class="scanner-stat"><div class="ss-val red" id="scan-totalInvalid">0</div><div class="ss-label">Invalid Keys</div></div>
<div class="scanner-stat"><div class="ss-val" id="scan-workers">0</div><div class="ss-label">Active Workers</div></div>
</div>
<div style="margin-bottom:24px">
<div class="stat-card" style="max-width:400px">
<div class="stat-label">GitHub Rate Limit</div>
<div class="stat-value" id="scan-ratelimit">--</div>
<div class="rl-bar"><div class="rl-bar-fill" id="rl-bar-fill" style="width:100%;background:var(--green)"></div></div>
</div>
</div>
<div class="feed-card">
<div class="feed-header"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg> Recent Scans</div>
<div class="feed-list" id="scanFeed"><div class="feed-empty">No scan activity yet</div></div>
</div>
</div>

<!-- Settings Page -->
<div class="page" id="page-settings">
<div class="page-title">Settings</div>
<div class="settings-section">
<div class="settings-header">General</div>
<div class="settings-row"><div><div class="sr-label">Auto Validate</div><div class="sr-desc">Automatically re-validate keys on schedule</div></div><label class="toggle"><input type="checkbox" id="set-autoValidate" onchange="saveSettings()"><span class="toggle-slider"></span></label></div>
<div class="settings-row"><div><div class="sr-label">Validate Interval</div><div class="sr-desc">How often to re-validate all keys</div></div><select id="set-validateInterval" onchange="saveSettings()"><option value="1h">1 hour</option><option value="6h">6 hours</option><option value="12h">12 hours</option><option value="24h">24 hours</option><option value="72h">72 hours</option></select></div>
<div class="settings-row"><div><div class="sr-label">Proxy Enabled</div><div class="sr-desc">Enable the API proxy endpoint for external requests</div></div><label class="toggle"><input type="checkbox" id="set-proxyEnabled" onchange="saveSettings()"><span class="toggle-slider"></span></label></div>
<div class="settings-row"><div><div class="sr-label">Discord Notifications</div><div class="sr-desc">Send valid key alerts to Discord webhook</div></div><label class="toggle"><input type="checkbox" id="set-discordEnabled" onchange="saveSettings()"><span class="toggle-slider"></span></label></div>
</div>
<div class="settings-section">
<div class="settings-header">Security</div>
<div class="settings-row"><div><div class="sr-label">Dashboard Password</div><div class="sr-desc">Change the password for dashboard access</div></div><input type="password" class="settings-input" id="set-newPassword" placeholder="New password"><button class="btn btn-sm btn-accent" onclick="changePassword()">Update</button></div>
</div>
</div>

</div>
</div>

<!-- Toast Container -->
<div class="toast-container" id="toastContainer"></div>

<!-- Confirm Modal -->
<div id="confirmModal" style="display:none"></div>

<script>
/* ── State ── */
var S = {
  authenticated: false,
  currentPage: 'overview',
  keys: [],
  providers: [],
  activeProvider: '',
  expandedKey: null,
  ws: null,
  stats: {},
  settings: {}
};

/* ── SVG Icon Helpers ── */
var IC = {
  check: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/></svg>',
  x: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>',
  question: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>',
  copy: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>',
  refresh: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>',
  trash: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>',
  zap: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg>'
};

/* ── Auth ── */
function doLogin() {
  var pw = document.getElementById('loginPass').value;
  if (!pw) return;
  fetch('/auth', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({password:pw})})
    .then(function(r) {
      if (r.ok) {
        S.authenticated = true;
        showApp();
      } else {
        document.getElementById('loginError').style.display = 'block';
        setTimeout(function(){ document.getElementById('loginError').style.display = 'none'; }, 3000);
      }
    })
    .catch(function() {
      document.getElementById('loginError').style.display = 'block';
    });
}

function doLogout() {
  document.cookie = 'session=; Path=/; Max-Age=0';
  S.authenticated = false;
  if (S.ws) { S.ws.close(); S.ws = null; }
  document.getElementById('appView').classList.remove('active');
  document.getElementById('loginView').classList.add('active');
}

/* ── View Switching ── */
function showApp() {
  document.getElementById('loginView').classList.remove('active');
  document.getElementById('appView').classList.add('active');
  loadActivityFromStorage();
  loadAll();
  connectWS();
}

function switchPage(page) {
  S.currentPage = page;
  var pages = document.querySelectorAll('.page');
  for (var i = 0; i < pages.length; i++) pages[i].classList.remove('active');
  document.getElementById('page-' + page).classList.add('active');
  var navs = document.querySelectorAll('.nav-item');
  for (var i = 0; i < navs.length; i++) {
    navs[i].classList.toggle('active', navs[i].getAttribute('data-page') === page);
  }
  if (page === 'keys') loadKeys();
  if (page === 'proxy') loadProxy();
  if (page === 'settings') loadSettings();
}

/* ── Data Loading ── */
function loadAll() {
  loadStats();
  loadKeys();
  loadProviders();
  loadSettings();
}

function loadStats() {
  fetch('/api/stats').then(function(r) {
    if (r.status === 401) { doLogout(); return null; }
    return r.json();
  }).then(function(d) {
    if (!d) return;
    updateStats(d);
  }).catch(function(){});
}

function updateStats(d) {
  S.stats = d;
  var ks = d.keyStore || {};
  document.getElementById('stat-total').textContent = ks.total || 0;
  document.getElementById('stat-valid').textContent = ks.valid || 0;
  document.getElementById('stat-invalid').textContent = d.totalInvalid || ks.invalid || 0;
  document.getElementById('stat-unchecked').textContent = ks.unchecked || 0;

  var rlRemain = d.rateLimitRemain || 0;
  var rlLimit = d.rateLimitLimit || 0;
  document.getElementById('stat-ratelimit').textContent = rlLimit > 0 ? (rlRemain + '/' + rlLimit) : '--';

  if (d.uptime) {
    var up = new Date(d.uptime);
    var diff = Date.now() - up.getTime();
    document.getElementById('stat-uptime').textContent = formatDuration(diff);
  }

  /* Scanner page */
  document.getElementById('scan-totalScanned').textContent = d.totalScanned || 0;
  document.getElementById('scan-totalFound').textContent = d.totalFound || 0;
  document.getElementById('scan-totalRawHits').textContent = d.totalRawHits || 0;
  document.getElementById('scan-totalValid').textContent = d.totalValid || 0;
  document.getElementById('scan-totalInvalid').textContent = d.totalInvalid || 0;
  document.getElementById('scan-workers').textContent = (d.scanWorkers || 0) + ' / ' + (d.verifyWorkers || 0);
  document.getElementById('scan-ratelimit').textContent = rlLimit > 0 ? (rlRemain + ' / ' + rlLimit) : '--';

  var pct = rlLimit > 0 ? (rlRemain / rlLimit * 100) : 100;
  var rlBar = document.getElementById('rl-bar-fill');
  rlBar.style.width = pct + '%';
  rlBar.style.background = pct > 50 ? 'var(--green)' : pct > 20 ? 'var(--yellow)' : 'var(--red)';
}

function formatDuration(ms) {
  var s = Math.floor(ms / 1000);
  var d = Math.floor(s / 86400);
  var h = Math.floor((s % 86400) / 3600);
  var m = Math.floor((s % 3600) / 60);
  if (d > 0) return d + 'd ' + h + 'h';
  if (h > 0) return h + 'h ' + m + 'm';
  return m + 'm';
}

function timeAgo(ts) {
  if (!ts) return '--';
  var d = new Date(ts);
  var diff = Date.now() - d.getTime();
  if (diff < 60000) return 'just now';
  if (diff < 3600000) return Math.floor(diff/60000) + 'm ago';
  if (diff < 86400000) return Math.floor(diff/3600000) + 'h ago';
  return Math.floor(diff/86400000) + 'd ago';
}

/* ── Providers & Keys ── */
function loadProviders() {
  fetch('/api/providers').then(function(r) {
    if (r.status === 401) return null;
    return r.json();
  }).then(function(d) {
    if (!d) return;
    S.providers = d;
    renderProviderTabs();
    renderProxyGrid();
  }).catch(function(){});
}

function renderProviderTabs() {
  var el = document.getElementById('providerTabs');
  var html = '<div class="provider-tab' + (S.activeProvider === '' ? ' active' : '') + '" onclick="selectProvider(\'\')">All</div>';
  for (var i = 0; i < S.providers.length; i++) {
    var p = S.providers[i];
    var cls = S.activeProvider === p.provider ? ' active' : '';
    html += '<div class="provider-tab' + cls + '" onclick="selectProvider(\'' + p.provider + '\')">' + p.provider + ' <span style="opacity:.6">(' + p.valid + '/' + p.total + ')</span></div>';
  }
  el.innerHTML = html;
}

function selectProvider(p) {
  S.activeProvider = p;
  renderProviderTabs();
  loadKeys();
}

function loadKeys() {
  var url = '/api/keys';
  if (S.activeProvider) url += '?provider=' + encodeURIComponent(S.activeProvider);
  fetch(url).then(function(r) {
    if (r.status === 401) return null;
    return r.json();
  }).then(function(d) {
    if (!d) return;
    S.keys = d || [];
    renderKeys();
  }).catch(function(){});
}

function renderKeys() {
  var body = document.getElementById('keysBody');
  var search = (document.getElementById('keySearch').value || '').toLowerCase();
  var keys = S.keys;

  if (search) {
    keys = keys.filter(function(k) {
      return (k.key || '').toLowerCase().indexOf(search) !== -1 ||
             (k.provider || '').toLowerCase().indexOf(search) !== -1 ||
             (k.status || '').toLowerCase().indexOf(search) !== -1 ||
             (k.balance || '').toLowerCase().indexOf(search) !== -1 ||
             (k.tier || '').toLowerCase().indexOf(search) !== -1 ||
             (k.keyType || '').toLowerCase().indexOf(search) !== -1;
    });
  }

  if (keys.length === 0) {
    body.innerHTML = '<tr><td colspan="7" class="table-empty">No keys found</td></tr>';
    return;
  }

  var html = '';
  for (var i = 0; i < keys.length; i++) {
    var k = keys[i];
    var badgeCls = k.status === 'valid' ? 'badge-valid' : (k.status === 'invalid' ? 'badge-invalid' : 'badge-unchecked');
    var badgeIcon = k.status === 'valid' ? IC.check : (k.status === 'invalid' ? IC.x : IC.question);
    var expanded = S.expandedKey === k.id;
    var shortKey = (k.key || '').substring(0, 12) + '...';

    html += '<tr onclick="toggleKeyDetail(' + k.id + ')" style="cursor:pointer">';
    html += '<td><span class="badge ' + badgeCls + '">' + badgeIcon + ' ' + k.status + '</span></td>';
    html += '<td>' + (k.provider || '--') + '</td>';
    html += '<td><span class="key-val" title="' + escHtml(k.key || '') + '">' + escHtml(shortKey) + '</span></td>';
    html += '<td>' + (k.balance || '--') + '</td>';
    html += '<td>' + (k.keyType || '--') + '</td>';
    html += '<td>' + timeAgo(k.lastChecked) + '</td>';
    html += '<td class="key-actions" onclick="event.stopPropagation()">';
    html += '<button title="Copy" onclick="copyKey(' + k.id + ')">' + IC.copy + '</button>';
    html += '<button title="Re-check" onclick="recheckKey(' + k.id + ')">' + IC.refresh + '</button>';
    html += '<button title="Delete" class="danger" onclick="confirmDelete(' + k.id + ')">' + IC.trash + '</button>';
    html += '</td></tr>';

    if (expanded) {
      html += '<tr class="key-detail-row"><td colspan="7"><div class="key-detail">';
      html += '<dl><dt>Full Key</dt><dd>' + escHtml(k.key || '--') + '</dd></dl>';
      html += '<dl><dt>Balance</dt><dd>' + escHtml(k.balance || '--') + '</dd></dl>';
      html += '<dl><dt>Quota</dt><dd>' + escHtml(k.quota || '--') + '</dd></dl>';
      html += '<dl><dt>Tier</dt><dd>' + escHtml(k.tier || '--') + '</dd></dl>';
      html += '<dl><dt>Type</dt><dd>' + escHtml(k.keyType || '--') + '</dd></dl>';
      html += '<dl><dt>Org</dt><dd>' + escHtml(k.org || '--') + '</dd></dl>';
      html += '<dl><dt>Models</dt><dd>' + escHtml(k.models || '--') + '</dd></dl>';
      html += '<dl><dt>Details</dt><dd>' + escHtml(k.details || '--') + '</dd></dl>';
      html += '<dl><dt>Repo</dt><dd>' + escHtml(k.repo || '--') + '</dd></dl>';
      html += '<dl><dt>Use Count</dt><dd>' + (k.useCount || 0) + '</dd></dl>';
      html += '<dl><dt>Found At</dt><dd>' + (k.foundAt ? new Date(k.foundAt).toLocaleString() : '--') + '</dd></dl>';
      html += '<dl><dt>Last Used</dt><dd>' + (k.lastUsed ? new Date(k.lastUsed).toLocaleString() : '--') + '</dd></dl>';
      html += '</div></td></tr>';
    }
  }
  body.innerHTML = html;
}

function escHtml(s) {
  return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}

function filterKeys() { renderKeys(); }

function toggleKeyDetail(id) {
  S.expandedKey = S.expandedKey === id ? null : id;
  renderKeys();
}

function copyKey(id) {
  var k = S.keys.find(function(x){ return x.id === id; });
  if (!k) return;
  navigator.clipboard.writeText(k.key || '').then(function() {
    showToast('Key copied to clipboard', 'success');
  }).catch(function() {
    /* fallback */
    var ta = document.createElement('textarea');
    ta.value = k.key || '';
    document.body.appendChild(ta);
    ta.select();
    document.execCommand('copy');
    document.body.removeChild(ta);
    showToast('Key copied to clipboard', 'success');
  });
}

function recheckKey(id) {
  fetch('/api/keys/' + id + '/recheck', {method:'POST'}).then(function(r) {
    if (r.ok) showToast('Re-checking key #' + id, 'info');
    else showToast('Failed to re-check key', 'error');
  }).catch(function(){ showToast('Request failed', 'error'); });
}

function confirmDelete(id) {
  var modal = document.getElementById('confirmModal');
  modal.style.display = 'block';
  modal.innerHTML = '<div class="modal-overlay" onclick="closeConfirm()"><div class="modal-box" onclick="event.stopPropagation()"><h3>Delete Key</h3><p>Are you sure you want to delete key #' + id + '? This cannot be undone.</p><div class="modal-actions"><button class="btn" onclick="closeConfirm()">Cancel</button><button class="btn btn-danger" onclick="deleteKey(' + id + ')">Delete</button></div></div></div>';
}

function closeConfirm() {
  document.getElementById('confirmModal').style.display = 'none';
}

function deleteKey(id) {
  closeConfirm();
  fetch('/api/keys/' + id, {method:'DELETE'}).then(function(r) {
    if (r.status === 204) {
      showToast('Key deleted', 'success');
      loadKeys();
      loadStats();
    } else {
      showToast('Failed to delete key', 'error');
    }
  }).catch(function(){ showToast('Request failed', 'error'); });
}

function validateAll() {
  fetch('/api/validate-all', {method:'POST'}).then(function(r) {
    if (r.ok) showToast('Validation started', 'info');
    else showToast('Failed to start validation', 'error');
  }).catch(function(){ showToast('Request failed', 'error'); });
}

/* ── Add Key ── */
var ADD_KEY_PROVIDERS = ['openai','anthropic','deepseek','mistral','groq','openrouter','xai','together','fireworks','perplexity','huggingface','replicate','cohere','elevenlabs','ai21'];

function showAddKeyModal() {
  var modal = document.getElementById('confirmModal');
  var opts = '';
  for (var i = 0; i < ADD_KEY_PROVIDERS.length; i++) {
    opts += '<option value="' + ADD_KEY_PROVIDERS[i] + '">' + ADD_KEY_PROVIDERS[i] + '</option>';
  }
  modal.style.display = 'block';
  modal.innerHTML = '<div class="modal-overlay" onclick="closeConfirm()"><div class="modal-box" onclick="event.stopPropagation()" style="text-align:left;width:420px"><h3 style="text-align:center">Add Key</h3><div style="margin-bottom:14px"><label style="font-size:12px;color:var(--text-dim);display:block;margin-bottom:4px">Provider</label><select id="addKeyProvider" style="width:100%">' + opts + '</select></div><div style="margin-bottom:14px"><label style="font-size:12px;color:var(--text-dim);display:block;margin-bottom:4px">API Key</label><input type="text" id="addKeyValue" placeholder="sk-..." style="width:100%;padding:10px 14px;border:1px solid var(--border);border-radius:var(--radius);background:var(--bg);color:var(--text);font-size:13px;font-family:monospace;outline:none"></div><div style="font-size:12px;color:var(--text-dim);margin-bottom:16px">Key will be auto-verified after adding. If invalid, it will be removed.</div><div class="modal-actions"><button class="btn" onclick="closeConfirm()">Cancel</button><button class="btn btn-accent" onclick="submitAddKey()">Add Key</button></div></div></div>';
}

function submitAddKey() {
  var provider = document.getElementById('addKeyProvider').value;
  var key = document.getElementById('addKeyValue').value.trim();
  if (!key) { showToast('Please enter a key', 'error'); return; }
  closeConfirm();
  fetch('/api/keys', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({provider:provider, key:key})})
    .then(function(r) {
      if (r.status === 409) { showToast('Key already exists', 'error'); return null; }
      if (r.status === 400) { return r.json(); }
      if (r.ok) { showToast('Key added, verifying...', 'success'); loadKeys(); loadStats(); return null; }
      showToast('Failed to add key', 'error'); return null;
    })
    .catch(function(){ showToast('Request failed', 'error'); });
}

/* ── Proxy ── */
var PROXY_ENDPOINTS = {
  openai: ['POST /api/openai/v1/chat/completions', 'GET /api/openai/v1/models', 'POST /api/openai/v1/embeddings', 'POST /api/openai/v1/images/generations'],
  anthropic: ['POST /api/anthropic/v1/messages', 'GET /api/anthropic/v1/models'],
  deepseek: ['POST /api/deepseek/v1/chat/completions', 'GET /api/deepseek/v1/models'],
  mistral: ['POST /api/mistral/v1/chat/completions', 'GET /api/mistral/v1/models'],
  groq: ['POST /api/groq/v1/chat/completions', 'GET /api/groq/v1/models'],
  openrouter: ['POST /api/openrouter/v1/chat/completions', 'GET /api/openrouter/v1/models'],
  xai: ['POST /api/xai/v1/chat/completions', 'GET /api/xai/v1/models'],
  together: ['POST /api/together/v1/chat/completions', 'GET /api/together/v1/models'],
  fireworks: ['POST /api/fireworks/v1/chat/completions', 'GET /api/fireworks/v1/models'],
  perplexity: ['POST /api/perplexity/v1/chat/completions'],
  huggingface: ['POST /api/huggingface/v1/chat/completions'],
  replicate: ['POST /api/replicate/v1/predictions'],
  cohere: ['POST /api/cohere/v1/chat', 'POST /api/cohere/v1/embed'],
  elevenlabs: ['POST /api/elevenlabs/v1/text-to-speech/{voice_id}'],
  ai21: ['POST /api/ai21/v1/chat/completions']
};

function renderProxyGrid() {
  var grid = document.getElementById('proxyGrid');
  var html = '';
  var provs = S.providers;

  /* Also include providers with endpoints but no keys yet */
  var allProvs = {};
  for (var name in PROXY_ENDPOINTS) allProvs[name] = {total:0, valid:0};
  for (var i = 0; i < provs.length; i++) {
    allProvs[provs[i].provider] = provs[i];
  }

  for (var name in allProvs) {
    var p = allProvs[name];
    var endpoints = PROXY_ENDPOINTS[name] || [];
    html += '<div class="proxy-card">';
    html += '<div class="proxy-card-header"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="2" width="20" height="8" rx="2" ry="2"/><rect x="2" y="14" width="20" height="8" rx="2" ry="2"/><line x1="6" y1="6" x2="6.01" y2="6"/><line x1="6" y1="18" x2="6.01" y2="18"/></svg> ' + name + ' <span class="count">' + p.valid + '/' + p.total + ' keys</span></div>';
    for (var j = 0; j < endpoints.length; j++) {
      var ep = endpoints[j];
      var parts = ep.split(' /');
      var method = parts[0];
      var path = '/' + (parts[1] || '');
      html += '<div class="proxy-endpoint"><span class="proxy-method">' + method + '</span><span class="proxy-path">' + escHtml(path) + '</span><button class="btn btn-sm" onclick="copyText(\'' + escHtml(path) + '\')">' + IC.copy + '</button></div>';
    }
    html += '</div>';
  }
  grid.innerHTML = html;
}

function loadProxy() {
  loadProviders();
}

function copyText(text) {
  navigator.clipboard.writeText(text).then(function() {
    showToast('Copied: ' + text, 'success');
  }).catch(function() {
    showToast('Copy failed', 'error');
  });
}

/* ── Settings ── */
function loadSettings() {
  fetch('/api/settings').then(function(r) {
    if (r.status === 401) return null;
    return r.json();
  }).then(function(d) {
    if (!d) return;
    S.settings = d;
    document.getElementById('set-autoValidate').checked = !!d.autoValidate;
    document.getElementById('set-proxyEnabled').checked = !!d.proxyEnabled;
    document.getElementById('set-discordEnabled').checked = !!d.discordEnabled;
    var vi = document.getElementById('set-validateInterval');
    vi.value = d.validateInterval || '24h';
  }).catch(function(){});
}

function saveSettings() {
  var data = {
    autoValidate: document.getElementById('set-autoValidate').checked,
    proxyEnabled: document.getElementById('set-proxyEnabled').checked,
    discordEnabled: document.getElementById('set-discordEnabled').checked,
    validateInterval: document.getElementById('set-validateInterval').value,
    dashboardPassword: '***'
  };
  fetch('/api/settings', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(data)})
    .then(function(r) {
      if (r.ok) showToast('Settings saved', 'success');
      else showToast('Failed to save settings', 'error');
    })
    .catch(function(){ showToast('Request failed', 'error'); });
}

function changePassword() {
  var pw = document.getElementById('set-newPassword').value;
  if (!pw) { showToast('Enter a new password', 'error'); return; }
  var data = {
    autoValidate: document.getElementById('set-autoValidate').checked,
    proxyEnabled: document.getElementById('set-proxyEnabled').checked,
    discordEnabled: document.getElementById('set-discordEnabled').checked,
    validateInterval: document.getElementById('set-validateInterval').value,
    dashboardPassword: pw
  };
  fetch('/api/settings', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(data)})
    .then(function(r) {
      if (r.ok) {
        showToast('Password updated', 'success');
        document.getElementById('set-newPassword').value = '';
      } else {
        showToast('Failed to update password', 'error');
      }
    })
    .catch(function(){ showToast('Request failed', 'error'); });
}

/* ── WebSocket ── */
function connectWS() {
  if (S.ws) { try { S.ws.close(); } catch(e){} }
  var proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  var url = proto + '//' + location.host + '/ws';
  try {
    S.ws = new WebSocket(url);
  } catch(e) { return; }

  S.ws.onmessage = function(evt) {
    try {
      var msg = JSON.parse(evt.data);
      handleWSMessage(msg);
    } catch(e) {}
  };

  S.ws.onclose = function() {
    setTimeout(function() {
      if (S.authenticated) connectWS();
    }, 3000);
  };

  S.ws.onerror = function() {
    try { S.ws.close(); } catch(e) {}
  };
}

function handleWSMessage(msg) {
  var type = msg.type;
  var data = msg.data;

  if (type === 'stats') {
    var dashboard = data.dashboard || data;
    var ks = data.keyStore || {};
    var combined = {
      totalFound: dashboard.totalFound,
      totalScanned: dashboard.totalScanned,
      totalRawHits: dashboard.totalRawHits,
      totalValid: dashboard.totalValid,
      totalInvalid: dashboard.totalInvalid,
      totalWebhookOK: dashboard.totalWebhookOK,
      totalWebhookFail: dashboard.totalWebhookFail,
      rateLimitRemain: dashboard.rateLimitRemain,
      rateLimitLimit: dashboard.rateLimitLimit,
      status: dashboard.status,
      uptime: dashboard.uptime,
      scanWorkers: dashboard.scanWorkers,
      verifyWorkers: dashboard.verifyWorkers,
      keyStore: ks
    };
    updateStats(combined);
  }

  if (type === 'newKey') {
    showToast('New ' + (data.Provider || 'key') + ' found!', 'success');
    addActivityItem(data.Provider || 'unknown', data.Valid ? 'valid' : 'invalid', data);
    if (S.currentPage === 'keys') loadKeys();
  }

  if (type === 'keyUpdate') {
    if (data && data.status === 'deleted') {
      showToast('Key #' + data.id + ' removed (no longer valid)', 'info');
    }
    if (S.currentPage === 'keys') loadKeys();
    loadStats();
  }

  if (type === 'scanActivity') {
    addScanFeedItem(data);
  }
}

/* ── Activity Feed ── */
var activityItems = [];
var MAX_ACTIVITY = 50;

function saveActivityToStorage() {
  try {
    localStorage.setItem('kp_activity', JSON.stringify(activityItems));
  } catch(e) {}
}

function loadActivityFromStorage() {
  try {
    var stored = localStorage.getItem('kp_activity');
    if (stored) {
      activityItems = JSON.parse(stored);
      if (activityItems.length > MAX_ACTIVITY) activityItems = activityItems.slice(0, MAX_ACTIVITY);
      renderActivityFeed();
    }
  } catch(e) {}
}

function addActivityItem(provider, status, data) {
  activityItems.unshift({provider: provider, status: status, data: data, time: Date.now()});
  if (activityItems.length > MAX_ACTIVITY) activityItems = activityItems.slice(0, MAX_ACTIVITY);
  saveActivityToStorage();
  renderActivityFeed();
}

function redactKey(key) {
  if (!key) return '';
  if (key.length <= 12) return key.substring(0, 4) + '...' + key.substring(key.length - 4);
  return key.substring(0, 6) + '...' + key.substring(key.length - 4);
}

function renderActivityFeed() {
  var el = document.getElementById('activityFeed');
  if (activityItems.length === 0) {
    el.innerHTML = '<div class="feed-empty">Waiting for activity...</div>';
    return;
  }
  var html = '';
  var shown = activityItems.slice(0, 30);
  for (var i = 0; i < shown.length; i++) {
    var item = shown[i];
    var dotCls = item.status === 'valid' ? 'valid' : (item.status === 'invalid' ? 'invalid' : 'scan');
    var label = item.status === 'valid' ? 'Valid' : (item.status === 'invalid' ? 'Invalid' : 'Found');
    var detail = '';
    if (item.data) {
      if (item.status === 'invalid') {
        detail = ' ' + escHtml(redactKey(item.data.Key || item.data.key || ''));
        if (item.data.Details) detail += ' | ' + item.data.Details;
      } else {
        if (item.data.Balance) detail = ' | Balance: ' + item.data.Balance;
        else if (item.data.Details) detail = ' | ' + item.data.Details;
      }
    }
    html += '<div class="feed-item"><span class="feed-dot ' + dotCls + '"></span><span>' + label + ' <strong>' + item.provider + '</strong> key' + detail + '</span><span class="feed-time">' + timeAgo(item.time) + '</span></div>';
  }
  el.innerHTML = html;
}

/* ── Scan Feed ── */
function addScanFeedItem(data) {
  var el = document.getElementById('scanFeed');
  var empty = el.querySelector('.feed-empty');
  if (empty) el.innerHTML = '';
  var html = '<div class="feed-item"><span class="feed-dot scan"></span><span>' + escHtml(data.repoName || 'unknown') + '</span><span class="feed-time">' + timeAgo(data.time || Date.now()) + '</span></div>';
  el.insertAdjacentHTML('afterbegin', html);
}

/* ── Toast ── */
function showToast(msg, type) {
  var container = document.getElementById('toastContainer');
  var icon = type === 'success' ? IC.check : (type === 'error' ? IC.x : IC.zap);
  var html = '<div class="toast ' + type + '">' + icon + '<span>' + escHtml(msg) + '</span></div>';
  container.insertAdjacentHTML('beforeend', html);
  setTimeout(function() {
    var toasts = container.querySelectorAll('.toast');
    if (toasts.length > 0) toasts[toasts.length - 1].remove();
  }, 4000);
}

/* ── Init ── */
document.getElementById('loginPass').addEventListener('keydown', function(e) {
  if (e.key === 'Enter') doLogin();
});

/* Check auth */
fetch('/api/stats').then(function(r) {
  if (r.status === 401) {
    S.authenticated = false;
    document.getElementById('loginView').classList.add('active');
    document.getElementById('appView').classList.remove('active');
  } else {
    return r.json();
  }
}).then(function(d) {
  if (!d) return;
  S.authenticated = true;
  showApp();
  updateStats(d);
}).catch(function(){
  /* If fetch fails entirely, show login */
  document.getElementById('loginView').classList.add('active');
});

/* Uptime ticker */
setInterval(function() {
  if (S.stats && S.stats.uptime) {
    var up = new Date(S.stats.uptime);
    var diff = Date.now() - up.getTime();
    var el = document.getElementById('stat-uptime');
    if (el) el.textContent = formatDuration(diff);
  }
}, 60000);
</script>
</body>
</html>`
