package app

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const sessionCookieName = "session_id"

// minimal user shape for templates
type User struct {
	ID       int64
	Email    string
	Username string
	DisplayName string
	Bio         string
	AvatarPath  string
}

// hash a plaintext password
func hashPassword(pw string) ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
}

// compare a password with a stored hash
func checkPassword(hash []byte, pw string) error {
	return bcrypt.CompareHashAndPassword(hash, []byte(pw))
}

// create a session row + return token and expiry
func createSession(db *sql.DB, userID int64) (token string, expires time.Time, err error) {
	token = uuid.NewString()
	expires = time.Now().Add(7 * 24 * time.Hour)
	_, err = db.Exec(`INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)`,
		token, userID, expires.Unix()) // <-- store as INTEGER
	return
}
// delete a session row
func deleteSession(db *sql.DB, token string) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}

// set the browser cookie
func setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// Secure: true, // enable in production (HTTPS)
	})
}

// clear the cookie
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// currentUser: read expiry as int64 and compare
func (a *App) currentUser(r *http.Request) (*User, error) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return nil, nil
	}
	var u User
	var expiresUnix int64
	err = a.db.QueryRow(`
		SELECT u.id, u.email, u.username, COALESCE(u.display_name,''), COALESCE(u.bio,''), COALESCE(u.avatar_path,''), s.expires_at
                FROM sessions s
                JOIN users u ON u.id = s.user_id
                WHERE s.token = ?`, c.Value).
		Scan(&u.ID, &u.Email, &u.Username, &u.DisplayName, &u.Bio, &u.AvatarPath, &expiresUnix)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if time.Now().Unix() > expiresUnix {
		_ = deleteSession(a.db, c.Value)
		return nil, nil
	}
	return &u, nil
}