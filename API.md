# Outline API (for mobile apps)

All routes are mounted under the **`/api/v1`** prefix on the same server (using the same `config.json`
and `network.db`)

## Authorization

Uses a **Bearer token** instead of cookie sessions. Obtain a token via `/auth/login` or
`/auth/register`, then include it in every request:

```
Authorization: Bearer <token>
```

Tokens are stored in the `api_tokens` table (created automatically via `AutoMigrate`) and do
not expire automatically; use `/auth/logout` to revoke them. If you wish to implement
token expiration, that is the first feature to add later (an `ExpiresAt` field + a check in the middleware).

### POST `/api/v1/auth/register`
```json
{ "first_name": "Ivan", "last_name": "Ivanov", "email": "ivan@example.com",
"username": "ivan", "password": "12345678", "password2": "12345678" }
```
→ `201` `{ "token": "...", "user": { ... } }`

### POST `/api/v1/auth/login`
```json
{ "username": "ivan", "password": "12345678" }
```
(`username` accepts either the username or the e-mail) → `200` `{ "token": "...", "user": { ... } }`

### POST `/api/v1/auth/logout` 🔒
Revokes the provided token. → `204`

## Profile

| Method | Path | Description |
|---|---|---|
| GET  | `/me` 🔒 | current user (with e-mail) |
| PATCH| `/me` 🔒 | partial update of profile fields |
| POST | `/me/avatar` 🔒 | multipart field `avatar` → upload avatar |
| GET  | `/users/{id}` 🔒 | public profile + `is_following`, `friends`, `groups` |
| GET  | `/users/{id}/posts` 🔒 | posts on user's wall |
| POST | `/users/{id}/follow` 🔒 | follow |
| DELETE | `/users/{id}/follow` 🔒 | unfollow |

`PATCH /me` accepts any subset of fields (only those provided are updated):
```json
{ "full_name": "...", "about": "...", "phone": "...", "website": "...",
"city": "...", "interests": "...", "music": "...", "books": "..." }
```

## Feed, posts, comments

| Method | Path | Description |
|---|---|---|
| GET  | `/feed` 🔒 | feed (own posts + posts from followed users) |
| POST | `/posts` 🔒 | `{ "content": "...", "wall_owner_id": 1 }` or `{ "content": "...", "group_id": 3 }` |
| POST | `/posts/{id}/comments` 🔒 | `{ "content": "..." }` |

## Groups

| Method | Path | Description |
|---|---|---|
| GET  | `/groups` 🔒 | `{ "all": [...], "my": [...] }` |
| POST | `/groups` 🔒 | `{ "name": "...", "username": "...", "about": "..." }` |
| GET  | `/groups/{username}` 🔒 | group data + `is_member` + members |
| PATCH| `/groups/{username}` 🔒 | creator only: `{ "name": "...", "about": "..." }` |
| GET  | `/groups/{username}/posts` 🔒 | group wall |
| POST | `/groups/{id}/join` 🔒 | join |
| DELETE | `/groups/{id}/join` 🔒 | leave |
| POST | `/groups/{id}/avatar` 🔒 | multipart field `avatar`, creator only |

## Audio

| Method | Path | Description |
|---|---|---|
| GET  | `/audio?filter=mine\|all` 🔒 | list of tracks (default: `mine`) |
| POST | `/audio` 🔒 | multipart: fields `artist`, `title`, file `audio_file` |

## Admin Panel

| Method | Path | Description |
|---|---|---|
| GET  | `/admin/users` 🔒👑 | list of all users |
| POST | `/admin/users/{id}/toggle` 🔒👑 | `{ "type": "admin" }` or `{ "type": "verify" }` |

🔒 — requires `Authorization: Bearer <token>`. 👑 — additionally requires `is_admin: true`.

## Error Format
Always `{ "error": "human-readable message" }` with the appropriate HTTP status
(400/401/403/404/409).
