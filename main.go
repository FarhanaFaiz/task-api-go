package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/FarhanaFaiz/task-api-go/models"
	"github.com/joho/godotenv"
)

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func getTasksHandler(repository taskStore, w http.ResponseWriter, r *http.Request) {
	userID, _ := authenticatedUserID(r)
	tasks, err := repository.List(r.Context(), userID)
	if err != nil {
		log.Printf("list tasks: %v", err)
		writeError(w, http.StatusInternalServerError, "Unable to load tasks")
		return
	}

	writeJSON(w, http.StatusOK, tasks)
}

func getTaskHandler(repository taskStore, w http.ResponseWriter, r *http.Request) {
	id, valid := taskID(r)
	if !valid {
		writeError(w, http.StatusBadRequest, "Invalid task ID")
		return
	}

	userID, _ := authenticatedUserID(r)
	task, err := repository.Get(r.Context(), userID, id)
	if errors.Is(err, errTaskNotFound) {
		writeError(w, http.StatusNotFound, "Task not found")
		return
	}
	if err != nil {
		log.Printf("get task %d: %v", id, err)
		writeError(w, http.StatusInternalServerError, "Unable to load task")
		return
	}

	writeJSON(w, http.StatusOK, task)
}

func createTaskHandler(repository taskStore, w http.ResponseWriter, r *http.Request) {
	var task models.Task
	if err := decodeJSON(r, &task); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	if strings.TrimSpace(task.Title) == "" {
		writeError(w, http.StatusBadRequest, "Title is required")
		return
	}

	task.Completed = false
	userID, _ := authenticatedUserID(r)
	created, err := repository.Create(r.Context(), userID, task)
	if err != nil {
		log.Printf("create task: %v", err)
		writeError(w, http.StatusInternalServerError, "Unable to create task")
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

func updateTaskHandler(repository taskStore, w http.ResponseWriter, r *http.Request) {
	id, valid := taskID(r)
	if !valid {
		writeError(w, http.StatusBadRequest, "Invalid task ID")
		return
	}

	var task models.Task
	if err := decodeJSON(r, &task); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	if strings.TrimSpace(task.Title) == "" {
		writeError(w, http.StatusBadRequest, "Title is required")
		return
	}

	task.ID = id
	userID, _ := authenticatedUserID(r)
	updated, err := repository.Update(r.Context(), userID, task)
	if errors.Is(err, errTaskNotFound) {
		writeError(w, http.StatusNotFound, "Task not found")
		return
	}
	if err != nil {
		log.Printf("update task %d: %v", id, err)
		writeError(w, http.StatusInternalServerError, "Unable to update task")
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

func deleteTaskHandler(repository taskStore, w http.ResponseWriter, r *http.Request) {
	id, valid := taskID(r)
	if !valid {
		writeError(w, http.StatusBadRequest, "Invalid task ID")
		return
	}

	userID, _ := authenticatedUserID(r)
	err := repository.Delete(r.Context(), userID, id)
	if errors.Is(err, errTaskNotFound) {
		writeError(w, http.StatusNotFound, "Task not found")
		return
	}
	if err != nil {
		log.Printf("delete task %d: %v", id, err)
		writeError(w, http.StatusInternalServerError, "Unable to delete task")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func taskID(r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	return id, err == nil && id > 0
}

func decodeJSON(r *http.Request, destination any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(destination); err != nil {
		return err
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("request body must contain a single JSON value")
		}
		return err
	}

	return nil
}

func newHandler(repository taskStore, users userStore, jwtSecret string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler)
	mux.HandleFunc("POST /register", registerHandler(users))
	mux.HandleFunc("POST /login", loginHandler(users, jwtSecret))
	protected := authMiddleware(jwtSecret, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if r.PathValue("id") == "" {
				getTasksHandler(repository, w, r)
			} else {
				getTaskHandler(repository, w, r)
			}
		case http.MethodPost:
			createTaskHandler(repository, w, r)
		case http.MethodPut:
			updateTaskHandler(repository, w, r)
		case http.MethodDelete:
			deleteTaskHandler(repository, w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	mux.Handle("GET /tasks", protected)
	mux.Handle("POST /tasks", protected)
	mux.Handle("GET /tasks/{id}", protected)
	mux.Handle("PUT /tasks/{id}", protected)
	mux.Handle("DELETE /tasks/{id}", protected)
	return corsMiddleware(mux)
}

func loadEnvironment(path string) error {
	err := godotenv.Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func main() {
	if err := loadEnvironment(".env"); err != nil {
		log.Fatalf("load environment file: %v", err)
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is not set; set it to the PostgreSQL tasks database connection string")
	}
	jwtSecret := os.Getenv("JWT_SECRET")
	if len(jwtSecret) < 32 {
		log.Fatal("JWT_SECRET is not set or is shorter than 32 characters")
	}

	repository, err := newPostgresStore(databaseURL)
	if err != nil {
		log.Fatalf("initialize PostgreSQL: %v", err)
	}
	defer repository.Close()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Task API running at http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, newHandler(repository, repository, jwtSecret)))
}
