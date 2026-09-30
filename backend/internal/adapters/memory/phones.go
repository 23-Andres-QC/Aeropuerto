// Package memory implements repositories that must not persist anything.
package memory

import (
	"sync"

	"aeropuerto/internal/core/domain"
)

// PhoneRepository keeps the registered phones only in this process: a backend
// restart forgets them, by design.
type PhoneRepository struct {
	mu     sync.Mutex
	phones []domain.Phone
}

func NewPhoneRepository() *PhoneRepository { return &PhoneRepository{} }

func (r *PhoneRepository) List() []domain.Phone {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domain.Phone{}, r.phones...)
}

func (r *PhoneRepository) Add(p domain.Phone) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.phones = append(r.phones, p)
}

func (r *PhoneRepository) Remove(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, p := range r.phones {
		if p.ID == id {
			r.phones = append(r.phones[:i], r.phones[i+1:]...)
			return true
		}
	}
	return false
}
