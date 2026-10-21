package conduitownership

import (
	"context"
	"time"
)

// DatabaseIdentity is read-only pool metadata for explicit service composition.
// A match is not authorization; mutation methods retain their own validation.
func (s *Store) DatabaseIdentity(ctx context.Context) ([4]string, error) {
	var identity [4]string
	if ctx == nil {
		return identity, ErrInvalid
	}
	bound, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.Validate(bound); err != nil {
		return identity, err
	}
	err := s.db.QueryRowContext(bound, `SELECT current_database(),current_schema(),coalesce(inet_server_addr()::text,'local'),coalesce(inet_server_port(),0)::text`).Scan(&identity[0], &identity[1], &identity[2], &identity[3])
	if err != nil {
		return [4]string{}, failure(bound, err)
	}
	return identity, nil
}
