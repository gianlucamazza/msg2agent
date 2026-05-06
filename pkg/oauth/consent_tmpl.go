package oauth

import "html/template"

var consentTmpl = template.Must(template.New("consent").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Authorize Access — msg2agent</title>
  <style>
    body { font-family: system-ui, sans-serif; max-width: 480px; margin: 80px auto; padding: 0 24px; color: #1a1a1a; }
    h1 { font-size: 1.4rem; margin-bottom: 8px; }
    .app { font-weight: 600; color: #2563eb; }
    .scope-list { list-style: none; padding: 0; margin: 16px 0; }
    .scope-list li { padding: 6px 0; border-bottom: 1px solid #e5e7eb; font-size: 0.95rem; }
    .scope-list li:last-child { border: none; }
    .actions { display: flex; gap: 12px; margin-top: 24px; }
    button { flex: 1; padding: 10px 0; border: none; border-radius: 6px; font-size: 1rem; cursor: pointer; }
    .allow { background: #2563eb; color: #fff; }
    .deny  { background: #f3f4f6; color: #374151; border: 1px solid #d1d5db; }
    .tenant { color: #6b7280; font-size: 0.9rem; margin-top: 4px; }
  </style>
</head>
<body>
  <h1><span class="app">{{.ClientName}}</span> wants to access msg2agent</h1>
  <p class="tenant">Signed in as <strong>{{.TenantName}}</strong></p>
  <p>The application is requesting access to:</p>
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
      <button type="submit" name="action" value="allow" class="allow">Allow</button>
      <button type="submit" name="action" value="deny"  class="deny">Deny</button>
    </div>
  </form>
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
