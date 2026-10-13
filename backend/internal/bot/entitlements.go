package bot

import "context"

func (s *Store) AllowItem(ctx context.Context, feature, id string) error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.AllowItem(ctx, feature, id)
}
func (s *Store) ReserveHTTP(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.ReserveHTTP(ctx)
}
