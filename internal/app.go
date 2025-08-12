package app

import (
	"database/sql"
	"html/template"
	"net/http"
	"fmt"
)

type App struct {
	tpl *template.Template
	mux *http.ServeMux
	db  *sql.DB
}

func New() (*App, error) {
	// include post-related templates
	tpl, err := template.ParseFiles(
		"web/templates/base.html",
		"web/templates/index.html",
		"web/templates/login.html",
		"web/templates/register.html",
		"web/templates/new_post.html",
		"web/templates/post.html",
		"web/templates/error.html",
	)
	if err != nil { return nil, err }

	db, err := openDB()
	if err != nil { return nil, err }

	mux := http.NewServeMux()
	a := &App{tpl: tpl, mux: mux, db: db}

	// pages
	mux.HandleFunc("/", a.Home)
	mux.HandleFunc("/health", a.Health)
	mux.HandleFunc("/dbcheck", a.DBCheck)

	// auth
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost { a.RegisterPOST(w, r); return }
		a.RegisterGET(w, r)
	})
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost { a.LoginPOST(w, r); return }
		a.LoginGET(w, r)
	})
	mux.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost { http.Error(w, "method not allowed", http.StatusMethodNotAllowed); return }
		a.LogoutPOST(w, r)
	})

	// posts
	mux.HandleFunc("/posts/new", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost { a.NewPostPOST(w, r); return }
		a.NewPostGET(w, r)
	})
	mux.HandleFunc("/post", a.PostViewGET)      // GET /post?id=123
	mux.HandleFunc("/comment", a.CommentPOST)   // POST add comment
	// reactions (POST only)
	mux.HandleFunc("/react", func(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { http.Error(w, "method not allowed", http.StatusMethodNotAllowed); return }
	a.ReactPOST(w, r)
})

	// static
	fs := http.FileServer(http.Dir("web/assets"))
	mux.Handle("/assets/", http.StripPrefix("/assets/", fs))

	return a, nil
}

// renderError shows a friendly error page with the given status code.
func (a *App) renderError(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	_ = a.tpl.ExecuteTemplate(w, "error.html", map[string]any{
		"Title":   http.StatusText(status),
		"Status":  status,
		"Message": msg,
	})
}

// Router returns the mux wrapped with a panic recovery that shows a 500 page.
func (a *App) Router() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				_ = fmt.Sprint(rec) // avoid unused warning
				a.renderError(w, http.StatusInternalServerError, "Something went wrong. Please try again.")
			}
		}()
		a.mux.ServeHTTP(w, r)
	})
}



