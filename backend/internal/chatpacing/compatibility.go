package chatpacing

import (
	"context"
	"database/sql"
	"time"
)

// CheckState validates retained account formats at startup without mutation.
// The application must keep version-two turns, charges, cooldowns and claims.
func (s *Store) CheckState(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT attempts,fair_state FROM chat_pacing_accounts LIMIT 1025`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	count := 0
	for rows.Next() {
		count++
		if count > 1024 {
			return ErrCapacity
		}
		var attempts, fair []byte
		if err := rows.Scan(&attempts, &fair); err != nil {
			return err
		}
		if _, err := decodeAttempts(attempts); err != nil {
			return err
		}
		if _, err := decodeFair(fair); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	return tx.Commit()
}
