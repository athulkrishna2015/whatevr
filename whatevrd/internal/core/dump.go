package core

import (
	"context"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// DumpDerived prints every derived table as sorted rows, for comparing two
// stores that folded the same inputs. it says nothing about the log.
func (db *DB) DumpDerived(ctx context.Context) (map[string][]string, error) {
	out := map[string][]string{}
	for _, t := range db.tables {
		rows, err := db.read.QueryContext(ctx, `SELECT * FROM `+quoteIdent(t))
		if err != nil {
			return nil, err
		}
		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			return nil, err
		}
		var lines []string
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				rows.Close()
				return nil, err
			}
			parts := make([]string, len(cols))
			for i, v := range vals {
				switch v := v.(type) {
				case nil:
					parts[i] = cols[i] + "=NULL"
				case []byte:
					parts[i] = cols[i] + "=x" + hex.EncodeToString(v)
				default:
					parts[i] = fmt.Sprintf("%s=%v", cols[i], v)
				}
			}
			lines = append(lines, strings.Join(parts, " "))
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		sort.Strings(lines)
		out[t] = lines
	}
	return out, nil
}
