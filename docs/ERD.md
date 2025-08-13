# Literary Lions – ERD

```mermaid
erDiagram
  USERS ||--o{ SESSIONS : has
  USERS ||--o{ POSTS : writes
  USERS ||--o{ COMMENTS : writes
  POSTS ||--o{ COMMENTS : has
  POSTS ||--o{ POST_CATEGORIES : tagged
  CATEGORIES ||--o{ POST_CATEGORIES : used_in
  USERS ||--o{ POST_REACTIONS : reacts
  POSTS ||--o{ POST_REACTIONS : receives
  USERS ||--o{ COMMENT_REACTIONS : reacts
  COMMENTS ||--o{ COMMENT_REACTIONS : receives

  USERS {
    integer id PK
    text email UK
    text username UK
    blob password_hash
    datetime created_at
  }
  SESSIONS {
    text token PK
    integer user_id FK
    integer expires_at  "unix seconds"
    datetime created_at
  }
  POSTS {
    integer id PK
    integer user_id FK
    text title
    text content
    datetime created_at
  }
  COMMENTS {
    integer id PK
    integer post_id FK
    integer user_id FK
    text content
    datetime created_at
  }
  CATEGORIES {
    integer id PK
    text name UK
  }
  POST_CATEGORIES {
    integer post_id FK
    integer category_id FK
    PK "post_id, category_id"
  }
  POST_REACTIONS {
    integer user_id FK
    integer post_id FK
    integer value "1 or -1"
    PK "user_id, post_id"
  }
  COMMENT_REACTIONS {
    integer user_id FK
    integer comment_id FK
    integer value "1 or -1"
    PK "user_id, comment_id"
  }
