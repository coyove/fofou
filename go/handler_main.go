package main

import (
	"net/http"
)

type ModelMain struct {
	Forums      *[]*Forum
	User        string
	UserIsAdmin bool
	ErrorMsg    string
	RedirectUrl string
}

// handler for url: /
func handleMain(w http.ResponseWriter, r *http.Request) {
	if !isTopLevelUrl(r.URL.Path) {
		serve404(w, r)
		return
	}

	model := &ModelMain{
		Forums:      &appState.Forums,
		User:        decodeUserFromCookie(r),
		UserIsAdmin: false,
		RedirectUrl: r.URL.String()}

	if err := GetTemplates().ExecuteTemplate(w, tmplMain, model); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}