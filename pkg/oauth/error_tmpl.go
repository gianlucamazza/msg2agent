package oauth

import (
	"html/template"
	"net/http"
)

var errorTmpl = template.Must(template.New("error").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{.Title}} — msg2agent</title>
  <link rel="stylesheet" href="/style.css">
  <link rel="icon" href="/favicon.svg" type="image/svg+xml">
</head>
<body class="consent-body">
  <div class="consent-card">
    <div class="consent-brand">
      <img src="/logo-512.png" width="36" height="36" alt="msg2agent logo">
      <span>msg2agent</span>
    </div>
    <h1>{{.Title}}</h1>
    <p style="font-size:0.9rem;color:var(--muted);margin:1rem 0 1.5rem;">{{.Message}}</p>
    <a href="{{.BackURL}}" class="btn-primary" style="display:block;text-align:center;">Back to msg2agent</a>
    <div class="consent-footer">
      <a href="/privacy">Privacy</a> &middot; <a href="/terms">Terms</a>
    </div>
  </div>
</body>
</html>`))

type errorData struct {
	Title   string
	Message string
	BackURL string
}

func renderError(w http.ResponseWriter, status int, title, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = errorTmpl.Execute(w, errorData{
		Title:   title,
		Message: message,
		BackURL: "/",
	})
}
