package services

import (
	"fmt"
	"sync"

	"aeropuerto/internal/core/domain"
	"aeropuerto/internal/core/ports"
)

// PhoneService registers the phones the model service reads. Nothing about a
// phone is persisted: the repository is in memory and a restart forgets them.
type PhoneService struct {
	mu   sync.Mutex
	repo ports.PhoneRepository
}

func NewPhoneService(repo ports.PhoneRepository) *PhoneService {
	return &PhoneService{repo: repo}
}

func (s *PhoneService) List() []domain.Phone { return s.repo.List() }

// Add registers a phone; the name defaults to "Teléfono N".
func (s *PhoneService) Add(in domain.PhoneInput) (domain.Phone, error) {
	if err := in.Normalize(); err != nil {
		return domain.Phone{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.repo.List()
	if len(current) >= domain.MaxPhones {
		return domain.Phone{}, domain.Invalid(fmt.Sprintf("máximo %d teléfonos a la vez", domain.MaxPhones))
	}
	for _, p := range current {
		if p.URL == in.URL {
			return domain.Phone{}, domain.Invalid(fmt.Sprintf("esa URL ya está agregada como «%s»", p.Name))
		}
	}
	if in.Name == "" {
		in.Name = fmt.Sprintf("Teléfono %d", len(current)+1)
	}
	phone := domain.Phone{ID: "tel-" + newID()[:8], Name: in.Name, URL: in.URL}
	s.repo.Add(phone)
	return phone, nil
}

func (s *PhoneService) Remove(id string) error {
	if !s.repo.Remove(id) {
		return domain.NotFound("Teléfono no encontrado")
	}
	return nil
}
