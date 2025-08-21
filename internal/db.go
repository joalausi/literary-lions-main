package app

import (
	"database/sql"
	"os"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no CGO)
)

// openDB opens forum.db and ensures the schema exists.
func openDB() (*sql.DB, error) {
	path := os.Getenv("DB_PATH")
	if path == "" {
		path = "forum.db" // default for local dev
	}
	// DSN for modernc: use driver name "sqlite"
	db, err := sql.Open("sqlite", "file:"+ path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return nil, err
	}
  	if err := migrateUserProfile(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

const schemaSQL = `
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  email TEXT NOT NULL UNIQUE,
  username TEXT NOT NULL UNIQUE,
  password_hash BLOB NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  bio TEXT NOT NULL DEFAULT '',
  avatar_path TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);


CREATE TABLE IF NOT EXISTS sessions (
  token TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at INTEGER NOT NULL, -- store unix seconds
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);


CREATE TABLE IF NOT EXISTS categories (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS posts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  content TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS post_categories (
  post_id INTEGER NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  category_id INTEGER NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
  PRIMARY KEY (post_id, category_id)
);

CREATE TABLE IF NOT EXISTS comments (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  post_id INTEGER NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  content TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS post_reactions (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  post_id INTEGER NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  value INTEGER NOT NULL CHECK (value IN (-1, 1)),
  PRIMARY KEY (user_id, post_id)
);

CREATE TABLE IF NOT EXISTS comment_reactions (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  comment_id INTEGER NOT NULL REFERENCES comments(id) ON DELETE CASCADE,
  value INTEGER NOT NULL CHECK (value IN (-1, 1)),
  PRIMARY KEY (user_id, comment_id)
);
`
// migrateUserProfile ensures the users table has profile columns.
func migrateUserProfile(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(users)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		_ = rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk)
		cols[name] = true
	}
	if !cols["display_name"] {
		if _, err := db.Exec(`ALTER TABLE users ADD COLUMN display_name TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if !cols["bio"] {
		if _, err := db.Exec(`ALTER TABLE users ADD COLUMN bio TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if !cols["avatar_path"] {
		if _, err := db.Exec(`ALTER TABLE users ADD COLUMN avatar_path TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	return nil
}

// GetUserByUsername returns full user information by username.
func GetUserByUsername(db *sql.DB, username string) (*User, error) {
	var u User
	err := db.QueryRow(`SELECT id, email, username, display_name, bio, avatar_path FROM users WHERE username = ?`, username).
		Scan(&u.ID, &u.Email, &u.Username, &u.DisplayName, &u.Bio, &u.AvatarPath)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// UpdateUserProfile updates display name and bio.
func UpdateUserProfile(db *sql.DB, userID int64, displayName, bio string) error {
	_, err := db.Exec(`UPDATE users SET display_name=?, bio=? WHERE id=?`, displayName, bio, userID)
	return err
}

// UpdateUserAvatar sets avatar path for user.
func UpdateUserAvatar(db *sql.DB, userID int64, path string) error {
	_, err := db.Exec(`UPDATE users SET avatar_path=? WHERE id=?`, path, userID)
	return err
}

// CountUserPosts returns number of posts by user.
func CountUserPosts(db *sql.DB, userID int64) (int, error) {
	var c int
	err := db.QueryRow(`SELECT COUNT(*) FROM posts WHERE user_id=?`, userID).Scan(&c)
	return c, err
}

// CountUserComments returns number of comments by user.
func CountUserComments(db *sql.DB, userID int64) (int, error) {
	var c int
	err := db.QueryRow(`SELECT COUNT(*) FROM comments WHERE user_id=?`, userID).Scan(&c)
	return c, err
}

// CountUserPostLikes returns number of likes received on posts by the user.
func CountUserPostLikes(db *sql.DB, userID int64) (int, error) {
	var c int
	err := db.QueryRow(`
                SELECT COALESCE(SUM(CASE WHEN pr.value=1 THEN 1 END),0)
                FROM post_reactions pr
                JOIN posts p ON p.id = pr.post_id
                WHERE p.user_id = ?`, userID).Scan(&c)
	return c, err
}

// Post represents a simple post for listings.
type Post struct {
	ID        int64
	Title     string
	CreatedAt string
}

// CommentWithPost represents a comment with link to post.
type CommentWithPost struct {
	ID        int64
	PostID    int64
	PostTitle string
	Content   string
	CreatedAt string
}

// ListPostsByAuthor returns posts for a user with total count.
func ListPostsByAuthor(db *sql.DB, userID int64, offset, limit int) ([]Post, int, error) {
	rows, err := db.Query(`SELECT id, title, created_at FROM posts WHERE user_id=? ORDER BY created_at DESC LIMIT ? OFFSET ?`, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var list []Post
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.ID, &p.Title, &p.CreatedAt); err == nil {
			list = append(list, p)
		}
	}
	var total int
	_ = db.QueryRow(`SELECT COUNT(*) FROM posts WHERE user_id=?`, userID).Scan(&total)
	return list, total, nil
}

// ListCommentsByAuthor returns comments with post title for a user.
func ListCommentsByAuthor(db *sql.DB, userID int64, offset, limit int) ([]CommentWithPost, int, error) {
	rows, err := db.Query(`
                SELECT c.id, c.post_id, p.title, c.content, c.created_at
                FROM comments c
                JOIN posts p ON p.id = c.post_id
                WHERE c.user_id=?
                ORDER BY c.created_at DESC
                LIMIT ? OFFSET ?`, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var list []CommentWithPost
	for rows.Next() {
		var cmt CommentWithPost
		if err := rows.Scan(&cmt.ID, &cmt.PostID, &cmt.PostTitle, &cmt.Content, &cmt.CreatedAt); err == nil {
			list = append(list, cmt)
		}
	}
	var total int
	_ = db.QueryRow(`SELECT COUNT(*) FROM comments WHERE user_id=?`, userID).Scan(&total)
	return list, total, nil
}