package app

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
)

// Home — GET /
// Filters: ?cat=<id>, ?mine=1, ?liked=1
func (a *App) Home(w http.ResponseWriter, r *http.Request) {
	// return 404 for anything other than exact "/"
	if r.URL.Path != "/" {
		a.renderError(w, http.StatusNotFound, "Page not found.")
		return
	}

	u, _ := a.currentUser(r)

	// load categories for dropdown
	type catItem struct {
		ID   int64
		Name string
	}
	var cats []catItem
	if rowsC, _ := a.db.Query(`SELECT id, name FROM categories ORDER BY name`); rowsC != nil {
		defer rowsC.Close()
		for rowsC.Next() {
			var it catItem
			if err := rowsC.Scan(&it.ID, &it.Name); err == nil {
				cats = append(cats, it)
			}
		}
	}

	catIDStr := r.URL.Query().Get("cat")
	mine := r.URL.Query().Get("mine") == "1"
	liked := r.URL.Query().Get("liked") == "1"

	// base query — matches what worked in DB Browser
	q := `
SELECT p.id, p.title, u.username, p.created_at,
       COALESCE(GROUP_CONCAT(c.name, ', '), '') AS cats
FROM posts p
JOIN users u ON u.id = p.user_id
LEFT JOIN post_categories pc ON pc.post_id = p.id
LEFT JOIN categories c ON c.id = pc.category_id
`
	args := []any{}
	where := []string{}

	// filter by category id
	if catIDStr != "" {
		q += " JOIN post_categories pc2 ON pc2.post_id = p.id JOIN categories c2 ON c2.id = pc2.category_id "
		where = append(where, "c2.id = ?")
		if id, err := strconv.ParseInt(catIDStr, 10, 64); err == nil {
			args = append(args, id)
		} else {
			args = append(args, int64(-1))
		}
	}

	// filter: my posts
	if mine && u != nil {
		where = append(where, "p.user_id = ?")
		args = append(args, u.ID)
	}

	// filter: liked by me
	if liked && u != nil {
		q += " JOIN post_reactions pr ON pr.post_id = p.id AND pr.user_id = ? AND pr.value = 1 "
		args = append(args, u.ID)
	}

	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}

	q += `
GROUP BY p.id
ORDER BY p.created_at DESC
LIMIT 100
`

	type postItem struct {
		ID        int64
		Title     string
		Username  string
		CreatedAt string
		Cats      string
	}
	rows, err := a.db.Query(q, args...)
	if err != nil {
		a.renderError(w, http.StatusInternalServerError, "Database error.")
		return
	}
	defer rows.Close()

	var posts []postItem
	for rows.Next() {
		var it postItem
		if err := rows.Scan(&it.ID, &it.Title, &it.Username, &it.CreatedAt, &it.Cats); err == nil {
			posts = append(posts, it)
		}
	}

	data := map[string]any{
		"Title":       "Literary Lions Forum",
		"User":        u,
		"Categories":  cats,
		"Posts":       posts,
		"FilterCat":   catIDStr,
		"FilterMine":  mine,
		"FilterLiked": liked,
	}
	if err := a.tpl.ExecuteTemplate(w, "base.html", data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// Health — GET /health
// Simple liveness probe.
func (a *App) Health(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("ok"))
}

// DBCheck — GET /dbcheck
// Verifies we can ping the DB.
func (a *App) DBCheck(w http.ResponseWriter, r *http.Request) {
	if err := a.db.Ping(); err != nil {
		http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write([]byte("db ok"))
}

// RegisterGET — GET /register
// Shows the registration form.
func (a *App) RegisterGET(w http.ResponseWriter, r *http.Request) {
	// Check if user is already logged in
	u, _ := a.currentUser(r)
	if u != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	
	if err := a.tpl.ExecuteTemplate(w, "register.html", nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// RegisterPOST — POST /register
// Creates a user and starts a session.
func (a *App) RegisterPOST(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	// Trim whitespace to avoid accidental spaces.
	email := strings.TrimSpace(r.Form.Get("email"))
	username := strings.TrimSpace(r.Form.Get("username"))
	pw := strings.TrimSpace(r.Form.Get("password"))
	if email == "" || username == "" || pw == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}

	// Hash the password and insert the user.
	hash, err := hashPassword(pw)
	if err != nil {
		http.Error(w, "hash error", http.StatusInternalServerError)
		return
	}
	res, err := a.db.Exec(`INSERT INTO users (email, username, password_hash) VALUES (?, ?, ?)`,
		email, username, hash)
	if err != nil {
		// Likely UNIQUE constraint violation on email/username.
		http.Error(w, "email or username already exists", http.StatusConflict)
		return
	}
	uid, _ := res.LastInsertId()

	// Create session, set cookie, redirect home.
	token, exp, err := createSession(a.db, uid)
	if err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	setSessionCookie(w, token, exp)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// LoginGET — GET /login
// Shows the login form.
func (a *App) LoginGET(w http.ResponseWriter, r *http.Request) {
	// Check if user is already logged in
	u, _ := a.currentUser(r)
	if u != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	
	if err := a.tpl.ExecuteTemplate(w, "login.html", nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// LoginPOST — POST /login
// Verifies credentials and starts a session; shows error on the page if invalid.
func (a *App) LoginPOST(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		_ = a.tpl.ExecuteTemplate(w, "login.html", map[string]any{"Error": "Bad form"})
		return
	}
	email := strings.TrimSpace(r.Form.Get("email"))
	pw := strings.TrimSpace(r.Form.Get("password"))

	var id int64
	var username string
	var hash []byte
	err := a.db.QueryRow(`SELECT id, username, password_hash FROM users WHERE email = ?`, email).
		Scan(&id, &username, &hash)
	if err == sql.ErrNoRows {
		_ = a.tpl.ExecuteTemplate(w, "login.html", map[string]any{"Error": "Invalid email or password"})
		return
	}
	if err != nil {
		_ = a.tpl.ExecuteTemplate(w, "login.html", map[string]any{"Error": "Database error"})
		return
	}
	if err := checkPassword(hash, pw); err != nil {
		_ = a.tpl.ExecuteTemplate(w, "login.html", map[string]any{"Error": "Invalid email or password"})
		return
	}

	token, exp, err := createSession(a.db, id)
	if err != nil {
		_ = a.tpl.ExecuteTemplate(w, "login.html", map[string]any{"Error": "Session error"})
		return
	}
	setSessionCookie(w, token, exp)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// LogoutPOST — POST /logout
// Deletes session and clears cookie.
func (a *App) LogoutPOST(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(sessionCookieName)
	if err == nil && c.Value != "" {
		_ = deleteSession(a.db, c.Value)
	}
	clearSessionCookie(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// NewPostGET — GET /posts/new
// Shows a form to create a post (requires login).
func (a *App) NewPostGET(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	data := map[string]any{"Title": "New Post", "User": u}
	if err := a.tpl.ExecuteTemplate(w, "new_post.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// NewPostPOST — POST /posts/new
// Inserts the post and redirects to its page.
func (a *App) NewPostPOST(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	if u == nil {
		http.Error(w, "login required", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	title := strings.TrimSpace(r.Form.Get("title"))
	content := strings.TrimSpace(r.Form.Get("content"))
	catsRaw := strings.TrimSpace(r.Form.Get("categories"))
	if title == "" || content == "" {
		http.Error(w, "title and content required", http.StatusBadRequest)
		return
	}

	// Save and redirect to /post?id={newID}.
	res, err := a.db.Exec(`INSERT INTO posts (user_id, title, content) VALUES (?, ?, ?)`, u.ID, title, content)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	postID, _ := res.LastInsertId()

	// categories: split by comma, trim, ignore empties
	if catsRaw != "" {
		for _, c := range strings.Split(catsRaw, ",") {
			name := strings.TrimSpace(c)
			if name == "" {
				continue
			}
			// create category if missing
			_, _ = a.db.Exec(`INSERT OR IGNORE INTO categories (name) VALUES (?)`, name)
			var catID int64
			_ = a.db.QueryRow(`SELECT id FROM categories WHERE name = ?`, name).Scan(&catID)
			if catID > 0 {
				_, _ = a.db.Exec(`INSERT OR IGNORE INTO post_categories (post_id, category_id) VALUES (?, ?)`, postID, catID)
			}
		}
	}
	http.Redirect(w, r, "/post?id="+strconv.FormatInt(postID, 10), http.StatusSeeOther)
}

// PostViewGET — GET /post?id=123
func (a *App) PostViewGET(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)

	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// load post
	var post struct {
		ID        int64
		Title     string
		Content   string
		Username  string
		CreatedAt string
	}
	err = a.db.QueryRow(`
		SELECT p.id, p.title, p.content, u.username, p.created_at
		FROM posts p
		JOIN users u ON u.id = p.user_id
		WHERE p.id = ?`, id).
		Scan(&post.ID, &post.Title, &post.Content, &post.Username, &post.CreatedAt)
	if err == sql.ErrNoRows {
		a.renderError(w, http.StatusNotFound, "Post not found.")
		return
	}
	if err != nil {
		a.renderError(w, http.StatusInternalServerError, "Database error.")
		return
	}

	// post categories
	var postCats []string
	if cr2, err := a.db.Query(`
		SELECT c.name
		FROM categories c
		JOIN post_categories pc ON pc.category_id = c.id
		WHERE pc.post_id = ?`, id); err == nil {
		for cr2.Next() {
			var n string
			if err := cr2.Scan(&n); err == nil {
				postCats = append(postCats, n)
			}
		}
		cr2.Close()
	}

	// post reaction counts
	var postLikes, postDislikes int
	_ = a.db.QueryRow(`
		SELECT
		  COALESCE(SUM(CASE WHEN value=1 THEN 1 END),0),
		  COALESCE(SUM(CASE WHEN value=-1 THEN 1 END),0)
		FROM post_reactions WHERE post_id=?`, id).
		Scan(&postLikes, &postDislikes)

	// comments + counts
	type commentItem struct {
		ID        int64
		Username  string
		Content   string
		CreatedAt string
		Likes     int
		Dislikes  int
	}
	var comments []commentItem
	cr, err := a.db.Query(`
		SELECT c.id, u.username, c.content, c.created_at
		FROM comments c
		JOIN users u ON u.id = c.user_id
		WHERE c.post_id = ?
		ORDER BY c.created_at ASC`, id)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer cr.Close()
	for cr.Next() {
		var cmt commentItem
		if err := cr.Scan(&cmt.ID, &cmt.Username, &cmt.Content, &cmt.CreatedAt); err == nil {
			_ = a.db.QueryRow(`
				SELECT
				  COALESCE(SUM(CASE WHEN value=1 THEN 1 END),0),
				  COALESCE(SUM(CASE WHEN value=-1 THEN 1 END),0)
				FROM comment_reactions WHERE comment_id=?`, cmt.ID).
				Scan(&cmt.Likes, &cmt.Dislikes)
			comments = append(comments, cmt)
		}
	}

	data := map[string]any{
		"Title":          post.Title,
		"User":           u,
		"Post":           post,
		"PostCategories": postCats,
		"PostLikes":      postLikes,
		"PostDislikes":   postDislikes,
		"Comments":       comments,
	}
	if err := a.tpl.ExecuteTemplate(w, "post.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// CommentPOST — POST /comment
// Adds a comment to a post (requires login).
func (a *App) CommentPOST(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	if u == nil {
		http.Error(w, "login required", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		a.renderError(w, http.StatusInternalServerError, "Could not save comment.")
		return
	}
	postID, _ := strconv.ParseInt(r.Form.Get("post_id"), 10, 64)
	content := strings.TrimSpace(r.Form.Get("content"))
	if postID <= 0 || content == "" {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}

	// Insert and bounce back to the post page.
	if _, err := a.db.Exec(`INSERT INTO comments (post_id, user_id, content) VALUES (?, ?, ?)`,
		postID, u.ID, content); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/post?id="+strconv.FormatInt(postID, 10), http.StatusSeeOther)
}

// ReactPOST — POST /react
// Form fields: kind=post|comment, id (int), v=1|-1
func (a *App) ReactPOST(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	if u == nil {
		http.Error(w, "login required", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	kind := r.Form.Get("kind")
	idStr := r.Form.Get("id")
	vStr := r.Form.Get("v")
	if (kind != "post" && kind != "comment") || (vStr != "1" && vStr != "-1") || idStr == "" {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	targetID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || targetID <= 0 {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	val, _ := strconv.Atoi(vStr) // 1 or -1

	switch kind {
	case "post":
		// toggle/flip logic
		var existing int
		err = a.db.QueryRow(`SELECT value FROM post_reactions WHERE user_id=? AND post_id=?`, u.ID, targetID).Scan(&existing)
		if err == sql.ErrNoRows {
			_, _ = a.db.Exec(`INSERT INTO post_reactions (user_id, post_id, value) VALUES (?, ?, ?)`, u.ID, targetID, val)
		} else if err == nil {
			if existing == val {
				_, _ = a.db.Exec(`DELETE FROM post_reactions WHERE user_id=? AND post_id=?`, u.ID, targetID)
			} else {
				_, _ = a.db.Exec(`UPDATE post_reactions SET value=? WHERE user_id=? AND post_id=?`, val, u.ID, targetID)
			}
		} else {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
	case "comment":
		var existing int
		err = a.db.QueryRow(`SELECT value FROM comment_reactions WHERE user_id=? AND comment_id=?`, u.ID, targetID).Scan(&existing)
		if err == sql.ErrNoRows {
			_, _ = a.db.Exec(`INSERT INTO comment_reactions (user_id, comment_id, value) VALUES (?, ?, ?)`, u.ID, targetID, val)
		} else if err == nil {
			if existing == val {
				_, _ = a.db.Exec(`DELETE FROM comment_reactions WHERE user_id=? AND comment_id=?`, u.ID, targetID)
			} else {
				_, _ = a.db.Exec(`UPDATE comment_reactions SET value=? WHERE user_id=? AND comment_id=?`, val, u.ID, targetID)
			}
		} else {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
	}

	// bounce back to where the user came from
	ref := r.Header.Get("Referer")
	if ref == "" {
		ref = "/"
	}
	http.Redirect(w, r, ref, http.StatusSeeOther)
}
