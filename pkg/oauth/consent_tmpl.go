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
</head>
<body class="consent-body">
  <div class="consent-card">
    <div class="consent-brand">
      <img src="/logo-512.png" width="36" height="36" alt="msg2agent logo">
      <span>msg2agent</span>
    </div>
    <h1><span class="app-name">{{.ClientName}}</span> wants to access msg2agent</h1>
    <p class="tenant-name">Signed in as <strong>{{.TenantName}}</strong></p>
    <p class="consent-intro">The application is requesting:</p>
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
      <div class="consent-actions">
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
