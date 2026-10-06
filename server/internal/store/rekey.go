package store

import "context"

// Rekey re-encrypts every stored secret with a new key in one transaction.
// open decrypts with the old key, seal encrypts with the new one.
func (s *Store) Rekey(ctx context.Context, open func([]byte) ([]byte, error), seal func([]byte) []byte) (int, error) {
	n := 0
	err := s.Tx(ctx, func(tx *Store) error {
		for _, t := range []struct{ table, col string }{
			{"project_keys", "key_enc"},
			{"hosts", "admin_secret_enc"},
			{"resource_credentials", "secret_enc"},
			{"resource_credentials", "extra_enc"},
		} {
			rows, err := tx.q.Query(ctx, `select id::text, `+t.col+` from `+t.table+` where `+t.col+` is not null for update`)
			if err != nil {
				return err
			}
			type item struct {
				id  string
				val []byte
			}
			var items []item
			for rows.Next() {
				var it item
				if err := rows.Scan(&it.id, &it.val); err != nil {
					rows.Close()
					return err
				}
				items = append(items, it)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
			for _, it := range items {
				plain, err := open(it.val)
				if err != nil {
					return err
				}
				if _, err := tx.q.Exec(ctx, `update `+t.table+` set `+t.col+` = $2 where id::text = $1`, it.id, seal(plain)); err != nil {
					return err
				}
				n++
			}
		}
		return nil
	})
	return n, err
}
