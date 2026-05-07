package oauth

import "html/template"

var consentTmpl = template.Must(template.New("consent").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Authorize Access — msg2agent</title>
  <link rel="stylesheet" href="/style.css">
  <link rel="icon" href="/favicon.svg" type="image/svg+xml">
  <style>
    body { display: flex; flex-direction: column; min-height: 100vh; align-items: center; justify-content: center; padding: 1rem; }
    .consent-card {
      background: var(--surface); border: 1px solid var(--border); border-radius: 10px;
      padding: 2rem; width: 100%; max-width: 440px;
      box-shadow: 0 4px 24px rgba(0,0,0,0.4);
    }
    .consent-brand { display: flex; align-items: center; gap: 0.6rem; margin-bottom: 1.5rem; }
    .consent-brand img { border-radius: 6px; }
    .consent-brand span { font-size: 1.1rem; font-weight: 700; color: var(--accent); letter-spacing: 0.04em; }
    .consent-card h1 { font-size: 1.15rem; margin-bottom: 0.4rem; color: var(--text); }
    .app { font-weight: 600; color: var(--accent); }
    .tenant { color: var(--muted); font-size: 0.88rem; margin-bottom: 1rem; }
    .scope-list { list-style: none; padding: 0; margin: 0.75rem 0 1.5rem; border: 1px solid var(--border); border-radius: 6px; overflow: hidden; }
    .scope-list li { padding: 0.55rem 0.9rem; font-size: 0.9rem; color: var(--text); border-bottom: 1px solid var(--border); }
    .scope-list li:last-child { border: none; }
    .scope-list li::before { content: "✓ "; color: var(--accent); }
    .actions { display: flex; gap: 0.75rem; }
    .btn-primary { flex: 1; padding: 0.6rem 0; border: none; border-radius: 5px; font-size: 0.95rem; cursor: pointer; background: var(--accent); color: #fff; transition: opacity 0.15s; }
    .btn-primary:hover { opacity: 0.85; }
    .btn-ghost { flex: 1; padding: 0.6rem 0; border-radius: 5px; font-size: 0.95rem; cursor: pointer; background: transparent; color: var(--text); border: 1px solid var(--border); transition: border-color 0.15s; }
    .btn-ghost:hover { border-color: var(--accent); color: var(--accent); }
    .consent-footer { margin-top: 1.25rem; text-align: center; font-size: 0.78rem; color: var(--muted); }
    .consent-footer a { color: var(--muted); text-decoration: none; margin: 0 0.4rem; }
    .consent-footer a:hover { color: var(--accent); }
  </style>
</head>
<body>
  <div class="consent-card">
    <div class="consent-brand">
      <img src="/logo-512.png" width="36" height="36" alt="msg2agent logo">
      <span>msg2agent</span>
    </div>
    <h1><span class="app">{{.ClientName}}</span> wants to access msg2agent</h1>
    <p class="tenant">Signed in as <strong>{{.TenantName}}</strong></p>
    <p style="font-size:0.9rem;color:var(--muted);margin-bottom:0.5rem;">The application is requesting:</p>
    <ul class="scope-list">
      {{range .Scopes}}<li>{{.}}</li>{{end}}
    </ul>
    <form method="POST" action="/oauth/authorize/confirm">
      <input type="hidden" name="session" value="{{.SessionToken}}">
      <input type="hidden" name="client_id" value="{{.ClientID}}">
      <input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
      <input type="hidden" name="code_challenge" value="{{.CodeChallenge}}">
      <input type="hidden" name="code_challenge_method" value="{{.CodeChallengeMethod}}">
      <input type="hidden" name="scope" value="{{.Scope}}">
      <input type="hidden" name="state" value="{{.State}}">
      <div class="actions">
        <button type="submit" name="action" value="allow" class="btn-primary">Allow</button>
        <button type="submit" name="action" value="deny"  class="btn-ghost">Deny</button>
      </div>
    </form>
    <div class="consent-footer">
      Powered by msg2agent &middot;
      <a href="/privacy">Privacy</a> &middot;
      <a href="/terms">Terms</a>
    </div>
  </div>
</body>
</html>`))

type consentData struct {
	ClientName          string
	TenantName          string
	Scopes              []string
	SessionToken        string
	ClientID            string
	RedirectURI         string
	CodeChallenge       string
	CodeChallengeMethod string
	Scope               string
	State               string
}
