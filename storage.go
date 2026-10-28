package main

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/FarhanaFaiz/task-api-go/models"
)

var (
	errTaskNotFound = errors.New("task not found")
	errUserNotFound = errors.New("user not found")
	errEmailExists  = errors.New("email already exists")
)

type taskStore interface {
	List(context.Context, int) ([]models.Task, error)
	Get(context.Context, int, int) (models.Task, error)
	Create(context.Context, int, models.Task) (models.Task, error)
	Update(context.Context, int, models.Task) (models.Task, error)
	Delete(context.Context, int, int) error
}

type userStore interface {
	CreateUser(context.Context, string, []byte) (models.User, error)
	GetUserByEmail(context.Context, string) (models.User, []byte, error)
}

type memoryStore struct {
	mu     sync.RWMutex
	tasks  map[int]memoryTask
	users  map[int]memoryUser
	emails map[string]int
	nextID int
}

type memoryTask struct {
	task   models.Task
	userID int
}

type memoryUser struct {
	user     models.User
	password []byte
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		tasks:  make(map[int]memoryTask),
		users:  make(map[int]memoryUser),
		emails: make(map[string]int),
		nextID: 1,
	}
}

func (s *memoryStore) List(_ context.Context, userID int) ([]models.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]models.Task, 0)
	for _, item := range s.tasks {
		if item.userID == userID {
			result = append(result, item.task)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func (s *memoryStore) Get(_ context.Context, userID, id int) (models.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	item, exists := s.tasks[id]
	if !exists || item.userID != userID {
		return models.Task{}, errTaskNotFound
	}
	return item.task, nil
}

func (s *memoryStore) Create(_ context.Context, userID int, task models.Task) (models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task.ID = s.nextID
	s.nextID++
	s.tasks[task.ID] = memoryTask{task: task, userID: userID}
	return task, nil
}

func (s *memoryStore) Update(_ context.Context, userID int, task models.Task) (models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, exists := s.tasks[task.ID]
	if !exists || item.userID != userID {
		return models.Task{}, errTaskNotFound
	}
	s.tasks[task.ID] = memoryTask{task: task, userID: userID}
	return task, nil
}

func (s *memoryStore) Delete(_ context.Context, userID, id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, exists := s.tasks[id]
	if !exists || item.userID != userID {
		return errTaskNotFound
	}
	delete(s.tasks, id)
	return nil
}

func (s *memoryStore) CreateUser(_ context.Context, email string, password []byte) (models.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	email = strings.ToLower(strings.TrimSpace(email))
	if _, exists := s.emails[email]; exists {
		return models.User{}, errEmailExists
	}
	id := s.nextID
	s.nextID++
	user := models.User{ID: id, Email: email}
	s.users[id] = memoryUser{user: user, password: password}
	s.emails[email] = id
	return user, nil
}

func (s *memoryStore) GetUserByEmail(_ context.Context, email string) (models.User, []byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	id, exists := s.emails[strings.ToLower(strings.TrimSpace(email))]
	if !exists {
		return models.User{}, nil, errUserNotFound
	}
	user := s.users[id]
	return user.user, user.password, nil
}
