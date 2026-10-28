package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FarhanaFaiz/task-api-go/models"
	"github.com/golang-jwt/jwt/v5"
)

const testJWTSecret = "test-secret-for-authentication-at-least-32"

func newTestServer() http.Handler {
	store := newMemoryStore()
	return newHandler(store, store, testJWTSecret)
}

func request(t *testing.T, handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var payload bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&payload).Encode(body); err != nil {
			t.Fatalf("encode request: %v", err)
		}
	}

	req := httptest.NewRequest(method, path, &payload)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken(t, 1))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func testToken(t *testing.T, userID int) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, authClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	signed, err := token.SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("sign test token: %v", err)
	}
	return signed
}

func decodeResponse(t *testing.T, recorder *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.NewDecoder(recorder.Body).Decode(destination); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func TestHealth(t *testing.T) {
	recorder := request(t, newTestServer(), http.MethodGet, "/health", nil)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}

	var response map[string]string
	decodeResponse(t, recorder, &response)
	if response["status"] != "ok" {
		t.Fatalf("expected healthy response, got %#v", response)
	}
}

func TestCORSPreflight(t *testing.T) {
	handler := newTestServer()
	request := httptest.NewRequest(http.MethodOptions, "/tasks", nil)
	request.Header.Set("Origin", frontendOrigin)
	request.Header.Set("Access-Control-Request-Method", http.MethodGet)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", recorder.Code)
	}
	if recorder.Header().Get("Access-Control-Allow-Origin") != frontendOrigin {
		t.Fatalf("expected CORS origin %q, got %q", frontendOrigin, recorder.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestTaskCRUD(t *testing.T) {
	handler := newTestServer()

	create := request(t, handler, http.MethodPost, "/tasks", map[string]string{
		"title":       "Learn Go",
		"description": "Build a REST API",
	})
	if create.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", create.Code)
	}

	var created models.Task
	decodeResponse(t, create, &created)
	if created.ID != 1 || created.Completed {
		t.Fatalf("unexpected created task: %#v", created)
	}

	get := request(t, handler, http.MethodGet, "/tasks/1", nil)
	if get.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", get.Code)
	}

	update := request(t, handler, http.MethodPut, "/tasks/1", models.Task{
		Title:       "Learn Go deeply",
		Description: "Build and test a REST API",
		Completed:   true,
	})
	if update.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d", update.Code)
	}
	var updated models.Task
	decodeResponse(t, update, &updated)
	if updated.Title != "Learn Go deeply" || !updated.Completed || updated.ID != 1 {
		t.Fatalf("unexpected updated task: %#v", updated)
	}

	list := request(t, handler, http.MethodGet, "/tasks", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", list.Code)
	}
	var listed []models.Task
	decodeResponse(t, list, &listed)
	if len(listed) != 1 || listed[0] != updated {
		t.Fatalf("unexpected list response: %#v", listed)
	}

	deleteResponse := request(t, handler, http.MethodDelete, "/tasks/1", nil)
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", deleteResponse.Code)
	}

	missing := request(t, handler, http.MethodGet, "/tasks/1", nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("get deleted: expected 404, got %d", missing.Code)
	}
}

func TestTaskValidation(t *testing.T) {
	handler := newTestServer()
	tests := []struct {
		name string
		body string
	}{
		{name: "invalid JSON", body: `{"title":`},
		{name: "missing title", body: `{"description":"No title"}`},
		{name: "blank title", body: `{"title":"   "}`},
		{name: "trailing JSON", body: `{"title":"Task"} {"title":"Other"}`},
		{name: "unknown field", body: `{"title":"Task","priority":1}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewBufferString(test.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+testToken(t, 1))
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", recorder.Code)
			}
		})
	}
}

func TestAuthentication(t *testing.T) {
	handler := newTestServer()

	register := request(t, handler, http.MethodPost, "/register", map[string]string{
		"email": "test@example.com", "password": "password123",
	})
	if register.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", register.Code)
	}
	if strings.Contains(register.Body.String(), "password") {
		t.Fatal("registration response exposed password data")
	}

	duplicate := request(t, handler, http.MethodPost, "/register", map[string]string{
		"email": "test@example.com", "password": "password123",
	})
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate register: expected 409, got %d", duplicate.Code)
	}

	login := request(t, handler, http.MethodPost, "/login", map[string]string{
		"email": "test@example.com", "password": "password123",
	})
	if login.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d", login.Code)
	}
	var tokenResponse map[string]string
	decodeResponse(t, login, &tokenResponse)
	if tokenResponse["token"] == "" {
		t.Fatal("login did not return a token")
	}

	wrongPassword := request(t, handler, http.MethodPost, "/login", map[string]string{
		"email": "test@example.com", "password": "wrong-password",
	})
	if wrongPassword.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: expected 401, got %d", wrongPassword.Code)
	}

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/tasks", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated tasks: expected 401, got %d", unauthenticated.Code)
	}
}

func TestTaskOwnership(t *testing.T) {
	handler := newTestServer()
	create := request(t, handler, http.MethodPost, "/tasks", map[string]string{"title": "Private"})
	if create.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", create.Code)
	}
	var task models.Task
	decodeResponse(t, create, &task)

	req := httptest.NewRequest(http.MethodGet, "/tasks/"+strconv.Itoa(task.ID), nil)
	req.Header.Set("Authorization", "Bearer "+testToken(t, 2))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("other user access: expected 404, got %d", recorder.Code)
	}
}

func TestHandlersKeepIndependentRepositories(t *testing.T) {
	firstHandler := newHandler(newMemoryStore(), newMemoryStore(), testJWTSecret)
	secondHandler := newHandler(newMemoryStore(), newMemoryStore(), testJWTSecret)

	create := request(t, firstHandler, http.MethodPost, "/tasks", map[string]string{"title": "First"})
	if create.Code != http.StatusCreated {
		t.Fatalf("first handler create: expected 201, got %d", create.Code)
	}

	list := request(t, secondHandler, http.MethodGet, "/tasks", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("second handler list: expected 200, got %d", list.Code)
	}
	var tasks []models.Task
	decodeResponse(t, list, &tasks)
	if len(tasks) != 0 {
		t.Fatalf("expected independent repository, got %#v", tasks)
	}
}

func TestTaskNotFoundAndInvalidIDs(t *testing.T) {
	handler := newTestServer()
	for _, path := range []string{"/tasks/abc", "/tasks/0"} {
		recorder := request(t, handler, http.MethodGet, path, nil)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", path, recorder.Code)
		}
	}

	recorder := request(t, handler, http.MethodGet, "/tasks/99", nil)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", recorder.Code)
	}
}

func TestConcurrentTaskCreation(t *testing.T) {
	handler := newTestServer()
	const count = 20
	var waitGroup sync.WaitGroup
	waitGroup.Add(count)

	for i := 0; i < count; i++ {
		go func(i int) {
			defer waitGroup.Done()
			recorder := request(t, handler, http.MethodPost, "/tasks", map[string]string{
				"title": "Task " + strconv.Itoa(i),
			})
			if recorder.Code != http.StatusCreated {
				t.Errorf("create: expected 201, got %d", recorder.Code)
			}
		}(i)
	}
	waitGroup.Wait()

	recorder := request(t, handler, http.MethodGet, "/tasks", nil)
	var listed []models.Task
	decodeResponse(t, recorder, &listed)
	if len(listed) != count {
		t.Fatalf("expected %d tasks, got %d", count, len(listed))
	}
}
