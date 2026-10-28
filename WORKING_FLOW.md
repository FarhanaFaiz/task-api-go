# Task API Working Flow

This document explains how the Task API works from startup through
registration, login, and authenticated task operations.

## 1. Project structure

```text
task-api-go/
|-- main.go              HTTP routes and application startup
|-- auth.go              Registration, login, JWT, and auth middleware
|-- postgres.go          PostgreSQL connection and SQL operations
|-- storage.go           Storage interfaces and test-only in-memory store
|-- main_test.go         Automated API and authentication tests
|-- models/
|   |-- task.go          Task response model
|   |-- user.go          Public user response model
|-- go.mod               Go module and dependencies
|-- README.md            Project reference documentation
|-- .gitignore           Local secret and generated-file exclusions
```

## 2. Required environment variables

Set these variables in the same PowerShell terminal used to start the API.
Replace the placeholders locally. Never commit or share real passwords,
tokens, or secrets.

```powershell
$env:DATABASE_URL = "postgres://postgres:YOUR_PASSWORD@localhost:5432/tasks?sslmode=disable"
$env:JWT_SECRET = "local-development-secret-at-least-32-characters"
$env:PORT = "8084"
```

The application requires:

- `DATABASE_URL`: PostgreSQL connection string for the `tasks` database.
- `JWT_SECRET`: secret used to sign and verify JWTs. It must be at least
  32 characters long.
- `PORT`: HTTP port. The default is `8080`.

Check configuration without printing secret values:

```powershell
if ($env:DATABASE_URL) {
    Write-Output "DATABASE_URL is configured"
} else {
    Write-Output "DATABASE_URL is missing"
}

if ($env:JWT_SECRET.Length -ge 32) {
    Write-Output "JWT_SECRET is long enough"
} else {
    Write-Output "JWT_SECRET is missing or too short"
}
```

## 3. Application startup flow

Run the application:

```powershell
cd C:\Users\hp\task-api-go
go run .
```

The startup sequence is:

1. `main.go` reads `DATABASE_URL`, `JWT_SECRET`, and `PORT`.
2. The application stops with a clear error if the required configuration is
   missing.
3. `postgres.go` parses the PostgreSQL URL.
4. A PostgreSQL connection pool is created.
5. PostgreSQL connectivity is checked with `Ping`.
6. The `users` table is created if it does not exist.
7. The `tasks` table is created if it does not exist.
8. The `tasks.user_id` column is added if it is missing.
9. The task ownership foreign key is added if it is missing.
10. The HTTP server starts on the configured port.

The application never drops, truncates, or recreates existing tables.

## 4. Public health endpoint

The health endpoint does not require authentication:

```powershell
curl.exe -i http://localhost:8084/health
```

Expected response:

```json
{"status":"ok"}
```

## 5. User registration flow

`POST /register` is public.

Request:

```powershell
curl.exe -i `
  -X POST `
  -H "Content-Type: application/json" `
  -d '{"email":"test@example.com","password":"password123"}' `
  http://localhost:8084/register
```

The server:

1. Parses and validates the JSON.
2. Normalizes the email to lowercase.
3. Requires a valid-looking email and a password of at least 8 characters.
4. Hashes the password with bcrypt.
5. Stores the email and bcrypt hash in PostgreSQL.
6. Returns only the public user fields.

The plain-text password is never stored or returned.

Successful response:

```json
{
  "id": 1,
  "email": "test@example.com"
}
```

Duplicate email:

```text
409 Conflict
```

## 6. Login and JWT flow

`POST /login` is public.

```powershell
$loginResponse = curl.exe `
  -sS `
  -X POST `
  -H "Content-Type: application/json" `
  -d '{"email":"test@example.com","password":"password123"}' `
  http://localhost:8084/login

$token = ($loginResponse | ConvertFrom-Json).token
```

The server:

1. Finds the user by email.
2. Compares the submitted password with the bcrypt hash.
3. Rejects incorrect credentials with `401 Unauthorized`.
4. Creates a signed JWT containing the user ID and email.
5. Sets the token to expire after 24 hours.
6. Returns the token to the client.

Do not print or commit `$token`.

## 7. Authentication middleware

Every `/tasks` request must include:

```text
Authorization: Bearer <JWT_TOKEN>
```

Example:

```powershell
curl.exe `
  -i `
  -H "Authorization: Bearer $token" `
  http://localhost:8084/tasks
```

The middleware:

1. Reads the `Authorization` header.
2. Checks for the `Bearer` scheme.
3. Verifies the JWT signature using `JWT_SECRET`.
4. Checks that the token is valid and not expired.
5. Places the authenticated user ID into the request context.
6. Allows the task handler to continue.

Missing or invalid tokens return:

```text
401 Unauthorized
```

## 8. User-owned task flow

After authentication, the user ID from the JWT is used in every task query.

### List tasks

```powershell
curl.exe `
  -i `
  -H "Authorization: Bearer $token" `
  http://localhost:8084/tasks
```

Only tasks belonging to the logged-in user are returned.

### Create a task

```powershell
curl.exe `
  -i `
  -X POST `
  -H "Authorization: Bearer $token" `
  -H "Content-Type: application/json" `
  -d '{"title":"Learn Go","description":"Build a REST API"}' `
  http://localhost:8084/tasks
```

The server assigns the authenticated user's ID as `user_id`. The client
cannot choose another owner.

### Get a task

```powershell
curl.exe `
  -i `
  -H "Authorization: Bearer $token" `
  http://localhost:8084/tasks/1
```

The SQL query checks both the task ID and the authenticated user ID.

### Update a task

```powershell
curl.exe `
  -i `
  -X PUT `
  -H "Authorization: Bearer $token" `
  -H "Content-Type: application/json" `
  -d '{"title":"Learn Go deeply","description":"Updated","completed":true}' `
  http://localhost:8084/tasks/1
```

Users can update only their own tasks.

### Delete a task

```powershell
curl.exe `
  -i `
  -X DELETE `
  -H "Authorization: Bearer $token" `
  http://localhost:8084/tasks/1
```

Successful deletion returns:

```text
204 No Content
```

## 9. PostgreSQL ownership model

The tasks table contains:

```sql
id          INTEGER PRIMARY KEY
title       TEXT NOT NULL
description TEXT NOT NULL
completed   BOOLEAN NOT NULL
user_id     INTEGER REFERENCES users(id)
```

The users table contains:

```sql
id            INTEGER PRIMARY KEY
email         TEXT UNIQUE NOT NULL
password_hash BYTEA NOT NULL
created_at    TIMESTAMPTZ NOT NULL
```

Existing tasks from before authentication are preserved. If their `user_id` is
`NULL`, they are not visible to authenticated users because they do not yet
belong to an account.

Read-only inspection:

```powershell
& "C:\Program Files\PostgreSQL\18\bin\psql.exe" `
  $env:DATABASE_URL `
  -c "SELECT id, title, user_id FROM tasks ORDER BY id;"
```

## 10. Testing flow

Run the formatting and quality checks:

```powershell
cd C:\Users\hp\task-api-go
gofmt -w main.go storage.go postgres.go auth.go main_test.go models\task.go models\user.go
go test ./...
go vet ./...
```

The automated tests use the in-memory store and cover:

- Existing health and CRUD behavior
- Registration
- Duplicate email rejection
- Login
- Wrong password rejection
- Protected endpoint without a token
- Protected endpoint with a valid token
- Cross-user task isolation
- Invalid JSON and invalid task IDs
- Concurrent task creation

The automated tests do not modify the PostgreSQL database.

## 11. Common problems

### `DATABASE_URL is not set`

Set the variable in the same terminal used to run `go run .`:

```powershell
$env:DATABASE_URL = "postgres://postgres:YOUR_PASSWORD@localhost:5432/tasks?sslmode=disable"
```

### `JWT_SECRET is not set or is shorter than 32 characters`

Set a local development secret:

```powershell
$env:JWT_SECRET = "local-development-secret-at-least-32-characters"
```

### `401 Unauthorized` on `/tasks`

Login first and send the returned token:

```powershell
$token = ($loginResponse | ConvertFrom-Json).token
```

Then add:

```text
Authorization: Bearer <token>
```

### Port already in use

Use another port:

```powershell
$env:PORT = "8084"
go run .
```

### Existing tasks do not appear

Check their ownership:

```powershell
& "C:\Program Files\PostgreSQL\18\bin\psql.exe" `
  $env:DATABASE_URL `
  -c "SELECT id, title, user_id FROM tasks ORDER BY id;"
```

Tasks with `NULL` `user_id` were created before authentication and are
preserved but not assigned to a user.

