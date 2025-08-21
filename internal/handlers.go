package app

import (
	"bytes"
	"database/sql"
	"fmt"
	"html/template"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)
func (a *App) render(w http.ResponseWriter, tmpl string, data any) {
	t, ok := a.tpl[tmpl]
	if !ok {
		a.renderError(w, http.StatusInternalServerError, "template not found")
		return
	}
	if err := t.ExecuteTemplate(w, tmpl, data); err != nil {
		a.renderError(w, http.StatusInternalServerError, "template error")
	}
}

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
SELECT p.id, p.title, u.username, COALESCE(u.avatar_path,''), p.created_at,
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
		AvatarPath string
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
		if err := rows.Scan(&it.ID, &it.Title, &it.Username, &it.AvatarPath, &it.CreatedAt, &it.Cats); err == nil {
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
	// if err := a.tpl.ExecuteTemplate(w, "index.html", data); err != nil {
	// 	http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	// 	return
	// }
	a.render(w, "index.html", data)
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
	
	// if err := a.tpl.ExecuteTemplate(w, "register.html", nil); err != nil {
	// 	http.Error(w, err.Error(), http.StatusInternalServerError)
	// }
	a.render(w, "register.html", nil)
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
	
	// if err := a.tpl.ExecuteTemplate(w, "login.html", nil); err != nil {
	// 	http.Error(w, err.Error(), http.StatusInternalServerError)
	// }
	
	a.render(w, "login.html", nil)
}

// LoginPOST — POST /login
// Verifies credentials and starts a session; shows error on the page if invalid.
func (a *App) LoginPOST(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.render(w, "login.html", map[string]any{"Error": "Bad form"})
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
		a.render(w, "login.html", map[string]any{"Error": "Invalid email or password"})
		return
	}
	if err != nil {
		a.render(w, "login.html", map[string]any{"Error": "Database error"})
		return
	}
	if err := checkPassword(hash, pw); err != nil {
		a.render(w, "login.html", map[string]any{"Error": "Invalid email or password"})
		return
	}

	token, exp, err := createSession(a.db, id)
	if err != nil {
		a.render(w, "login.html", map[string]any{"Error": "Session error"})
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
	a.render(w, "new_post.html", data)
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
		AvatarPath string
		CreatedAt string
	}
	err = a.db.QueryRow(`
		SELECT p.id, p.title, p.content, u.username, u.avatar_path, p.created_at
		FROM posts p
		JOIN users u ON u.id = p.user_id
		WHERE p.id = ?`, id).
		Scan(&post.ID, &post.Title, &post.Content, &post.Username, &post.AvatarPath, &post.CreatedAt)
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
		AvatarPath string
		Content   string
		CreatedAt string
		Likes     int
		Dislikes  int
	}
	var comments []commentItem
	cr, err := a.db.Query(`
		SELECT c.id, u.username, COALESCE(u.avatar_path,''), c.content, c.created_at
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
		if err := cr.Scan(&cmt.ID, &cmt.Username, &cmt.AvatarPath, &cmt.Content, &cmt.CreatedAt); err == nil {
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
	a.render(w, "post.html", data)
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
// ProfileRouter handles /u/{username}, /u/{username}/posts, /u/{username}/comments
func (a *App) ProfileRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/u/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		a.renderError(w, http.StatusNotFound, "User not found")
		return
	}
	username := parts[0]
	tab := "posts"
	if len(parts) > 1 && parts[1] == "comments" {
		tab = "comments"
	}

	viewer, _ := a.currentUser(r)
	prof, err := GetUserByUsername(a.db, username)
	if err != nil {
		if err == sql.ErrNoRows {
			a.renderError(w, http.StatusNotFound, "User not found")
		} else {
			a.renderError(w, http.StatusInternalServerError, "Database error")
		}
		return
	}

	// counts
	postsCount, _ := CountUserPosts(a.db, prof.ID)
	commentsCount, _ := CountUserComments(a.db, prof.ID)
	likesCount, _ := CountUserPostLikes(a.db, prof.ID)

	page := 1
	limit := 10
	if v := r.URL.Query().Get("page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			page = p
		}
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if l, err := strconv.Atoi(v); err == nil && l > 0 && l <= 50 {
			limit = l
		}
	}
	offset := (page - 1) * limit

	type meta struct {
		Page    int
		Limit   int
		Total   int
		HasPrev bool
		HasNext bool
		PrevURL string
		NextURL string
	}
	m := meta{Page: page, Limit: limit}

	data := map[string]any{
		"Title":   prof.Username + " - Profile",
		"User":    viewer,
		"Profile": prof,
		"Counts":  map[string]int{"Posts": postsCount, "Comments": commentsCount, "Likes": likesCount},
		"Tab":     tab,
		"Meta":    &m,
		"IsOwner": viewer != nil && viewer.ID == prof.ID,
	}

	if tab == "comments" {
		comments, total, _ := ListCommentsByAuthor(a.db, prof.ID, offset, limit)
		m.Total = total
		m.HasPrev = page > 1
		m.HasNext = total > page*limit
		if m.HasPrev {
			m.PrevURL = fmt.Sprintf("/u/%s/comments?page=%d", prof.Username, page-1)
		}
		if m.HasNext {
			m.NextURL = fmt.Sprintf("/u/%s/comments?page=%d", prof.Username, page+1)
		}
		data["Comments"] = comments
	} else {
		posts, total, _ := ListPostsByAuthor(a.db, prof.ID, offset, limit)
		m.Total = total
		m.HasPrev = page > 1
		m.HasNext = total > page*limit
		if m.HasPrev {
			m.PrevURL = fmt.Sprintf("/u/%s/posts?page=%d", prof.Username, page-1)
		}
		if m.HasNext {
			m.NextURL = fmt.Sprintf("/u/%s/posts?page=%d", prof.Username, page+1)
		}
		data["Posts"] = posts
	}

	tpl, err := template.ParseFiles(
		"web/templates/base.html",
		"web/templates/profile.html",
		"web/templates/profile_posts.html",
		"web/templates/profile_comments.html",
	)
	if err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	if err := tpl.ExecuteTemplate(w, "profile.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// MeSettingsGET renders the profile settings form.
func (a *App) MeSettingsGET(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	csrf := a.generateCSRF(r)
	data := map[string]any{
		"Title":     "Settings",
		"User":      u,
		"CSRFToken": csrf,
	}
	tpl, err := template.ParseFiles("web/templates/base.html", "web/templates/me_settings.html")
	if err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	if err := tpl.ExecuteTemplate(w, "me_settings.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// MeSettingsPOST updates display name and bio.
func (a *App) MeSettingsPOST(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil || !a.checkCSRF(r) {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	dn := strings.TrimSpace(r.Form.Get("display_name"))
	bio := strings.TrimSpace(r.Form.Get("bio"))
	if len(dn) == 0 || len(dn) > 50 || len(bio) > 280 {
		http.Error(w, "validation error", http.StatusBadRequest)
		return
	}
	if err := UpdateUserProfile(a.db, u.ID, dn, bio); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/u/"+u.Username, http.StatusSeeOther)
}

// MeAvatarPOST uploads a new avatar for the current user.
func (a *App) MeAvatarPOST(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseMultipartForm(2<<20 + 1024); err != nil || !a.checkCSRF(r) {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	f, hdr, err := r.FormFile("avatar")
	if err != nil {
		http.Error(w, "file error", http.StatusBadRequest)
		return
	}
	defer f.Close()

	if hdr.Size > 2<<20 {
		http.Error(w, "file too large", http.StatusBadRequest)
		return
	}

	// read first bytes for mime detection
	var buf bytes.Buffer
	tee := io.TeeReader(f, &buf)
	head := make([]byte, 512)
	if _, err := tee.Read(head); err != nil && err != io.EOF {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	mime := http.DetectContentType(head)
	if mime != "image/jpeg" && mime != "image/png" {
		http.Error(w, "invalid mime", http.StatusBadRequest)
		return
	}
	img, _, err := image.Decode(io.MultiReader(bytes.NewReader(head), &buf, f))
	if err != nil {
		http.Error(w, "decode error", http.StatusBadRequest)
		return
	}
	img = resizeTo256(img)

	dir := "web/uploads/avatars"
	_ = os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, fmt.Sprintf("%d.jpg", u.ID))
	out, err := os.Create(path)
	if err != nil {
		http.Error(w, "save error", http.StatusInternalServerError)
		return
	}
	defer out.Close()
	if err := jpeg.Encode(out, img, &jpeg.Options{Quality: 90}); err != nil {
		http.Error(w, "encode error", http.StatusInternalServerError)
		return
	}
	webPath := "/uploads/avatars/" + fmt.Sprintf("%d.jpg", u.ID)
	if err := UpdateUserAvatar(a.db, u.ID, webPath); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/u/"+u.Username, http.StatusSeeOther)
}

// resizeTo256 crops the image to a square and scales to 256x256 using a simple
// nearest-neighbor algorithm.
func resizeTo256(img image.Image) image.Image {
	b := img.Bounds()
	size := b.Dx()
	if b.Dy() < size {
		size = b.Dy()
	}
	startX := b.Min.X + (b.Dx()-size)/2
	startY := b.Min.Y + (b.Dy()-size)/2
	out := image.NewRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			sx := startX + x*size/256
			sy := startY + y*size/256
			out.Set(x, y, img.At(sx, sy))
		}
	}
	return out
}