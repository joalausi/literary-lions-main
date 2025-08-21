package app

import (
	"database/sql"
	"html/template"
	"net/http"
	"fmt"
	"github.com/google/uuid"
)

type App struct {
	tpl map[string]*template.Template
	mux *http.ServeMux
	db  *sql.DB
	csrf map[string]string
}

func New() (*App, error) {
	// include post-related templates
	tpls := map[string]*template.Template{}

	var err error
	if tpls["index.html"], err = template.ParseFiles(
		"web/templates/base.html",
		"web/templates/index.html",
		); err != nil {
		return nil, err
	}
	if tpls["new_post.html"], err = template.ParseFiles(
		"web/templates/base.html",
		"web/templates/new_post.html",
	); err != nil {
		return nil, err
	}
	if tpls["post.html"], err = template.ParseFiles(
		"web/templates/base.html",
		"web/templates/post.html",
		); err != nil {
		return nil, err
	}
	if tpls["login.html"], err = template.ParseFiles("web/templates/login.html"); err != nil {
		return nil, err
	}
	if tpls["register.html"], err = template.ParseFiles("web/templates/register.html"); err != nil {
		return nil, err
	}
	if tpls["error.html"], err = template.ParseFiles("web/templates/error.html"); err != nil {
		return nil, err
	}

	db, err := openDB()
	if err != nil { return nil, err }

	mux := http.NewServeMux()
	a := &App{tpl: tpls, mux: mux, db: db, csrf: make(map[string]string)}

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

	// profile routes
	mux.HandleFunc("/u/", a.ProfileRouter) // handles /u/{username}/...
	mux.HandleFunc("/me/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			a.MeSettingsPOST(w, r)
			return
		}
		a.MeSettingsGET(w, r)
	})
	mux.HandleFunc("/me/avatar", a.MeAvatarPOST)

	// static
	fs := http.FileServer(http.Dir("web/assets"))
	mux.Handle("/assets/", http.StripPrefix("/assets/", fs))

	// uploaded avatars served with cache headers
	avatars := http.StripPrefix("/uploads/avatars/", http.FileServer(http.Dir("web/uploads/avatars")))
	mux.Handle("/uploads/avatars/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		avatars.ServeHTTP(w, r)
	}))

	return a, nil
}

// generateCSRF creates a token for the current session and stores it in memory.
func (a *App) generateCSRF(r *http.Request) string {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return ""
	}
	tok := uuid.NewString()
	a.csrf[c.Value] = tok
	return tok
}

// checkCSRF validates the token from the POST form.
func (a *App) checkCSRF(r *http.Request) bool {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return false
	}
	tok := r.FormValue("csrf")
	return tok != "" && a.csrf[c.Value] == tok
}

// renderError shows a friendly error page with the given status code.
func (a *App) renderError(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	if t, ok := a.tpl["error.html"]; ok {
		_ = t.ExecuteTemplate(w, "error.html", map[string]any{
			"Title":   http.StatusText(status),
			"Status":  status,
			"Message": msg,
		})
		return
	}
	http.Error(w, msg, status)

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



