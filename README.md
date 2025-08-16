# 🦁 Literary Lions Forum

Literary Lions Forum is a web-based discussion platform designed specifically for book enthusiasts. Members can create posts, engage in discussions, share book covers and reading moments through images, and connect with fellow literary lions in organized categories.

## 🚀 Quick Start

### Prerequisites
- Docker and Docker Compose
- Go 1.21+ (for local development)
## Getting Started
To run the project locally or in a Docker container, follow these steps:

### Clone the repo
```
git clone https://gitea.kood.tech/mohammadtavasoli/Literary-lions.git
cd literaryLions
```
- if run locally:

```
go run main.go
go run ./cmd/forumd
```

- or if using Docker:
```
docker build -t literary-lions .
docker run -p 8080:8080 literary-lions
```

Then visit http://localhost:8080

### Accessing the Forum
You can register an account and log in to explore the forum, create posts, comment on discussions, and interact with other book enthusiasts. If you want to test the project without registering, you can use the following credentials:

- **Email:** test@example.com
- **Password:** password
## Project Description

Literary Lions Forum is an online discussion platform where users can:

- **Create** posts and **comment** on posts
- **Search** for posts that spark your interests
- **Categorize** discussions by book genres, themes, and more
- **Like** posts and comments
- **Register and log in** with secure authentication
- **Handling data** using **SQLite**

---

## ✨ Features

- ✅ Register & log in (email, username, password) with **bcrypt**
- ✅ **UUID** cookie sessions with expiry
- ✅ Create **posts** & **comments** (logged-in only)
- ✅ Tag posts with **categories** and filter by category / **my posts** / **liked by me**
- ✅ **Like/Dislike** posts & comments (mutually exclusive) with counts
- ✅ Graceful **404 / 500** error pages
- ✅ **Dockerized** build & run


---

## 📁 File Structure

```bash
├─ cmd/
│  └─ forumd/           # Entry point (main)
├─ internal/
│  ├─ app.go            # Router, template loading, static
│  ├─ handlers.go       # HTTP handlers
│  ├─ auth.go           # Password hashing & sessions
│  └─ db.go             # SQLite + schema bootstrap
├─ web/
│  ├─ assets/
│  │  └─ css/style.css  # Styling
│  └─ templates/        # HTML templates
│     ├─ base.html
│     ├─ index.html
│     ├─ login.html
│     ├─ register.html
│     ├─ new_post.html
│     ├─ post.html
│     └─ error.html
├─ docs/
│  └─ ERD.md     # Mermaid ERD (diagram of tables)
   └─ README.md           
├─ Dockerfile
├─ .dockerignore
├─ .gitignore
├─ go.mod
└─ go.sum

## 🚀 How to Run

🚀 Run Locally

# from project root
go run ./cmd/forumd
# app -> http://localhost:8080

🚀 Environment

    PORT=8080 DB_PATH=forum.db go run ./cmd/forumd

🐳 Run with Docker

docker build -t literary-lions:0.1 .

🐳 quick run (ephemeral DB inside container)

docker run --name ll-forum -d \
  -p 8080:8080 \
  -e DB_PATH=/app/forum.db \
  --label com.kood.project="literary-lions" \
  literary-lions:0.1

🗄️ Database / ERD  

The app bootstraps tables for:
users, sessions, posts, comments, categories, post_categories, post_reactions, comment_reactions.

