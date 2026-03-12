package dumps

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	dbq "github.com/danielgormly/devctl/db/queries"
)

const defaultMaxEntries = 500

// Store persists dumps to SQLite.
type Store struct {
	db         *dbq.Queries
	maxEntries int64
}

// NewStore creates a Store.
func NewStore(db *sql.DB, maxEntries int64) *Store {
	if maxEntries <= 0 {
		maxEntries = defaultMaxEntries
	}
	return &Store{db: dbq.New(db), maxEntries: maxEntries}
}

// Insert saves a dump and prunes old entries.
func (s *Store) Insert(ctx context.Context, d Dump) (dbq.Dump, error) {
	file := ptrString(d.Source.File)
	line := ptrInt64(int64(d.Source.Line))
	domain := ptrString(d.Host)

	nodesJSON, err := marshalNodes(d.Nodes)
	if err != nil {
		return dbq.Dump{}, fmt.Errorf("marshal nodes: %w", err)
	}

	row, err := s.db.InsertDump(ctx, dbq.InsertDumpParams{
		File:       file,
		Line:       line,
		Nodes:      nodesJSON,
		Timestamp:  d.Timestamp,
		SiteDomain: domain,
	})
	if err != nil {
		return dbq.Dump{}, err
	}

	// Prune asynchronously — don't block the TCP receiver.
	go func() {
		if err := s.db.PruneOldDumps(context.Background(), s.maxEntries); err != nil {
			log.Printf("dumps: prune: %v", err)
		}
	}()

	return row, nil
}

// List returns a paginated list of dumps, optionally filtered by site domain.
func (s *Store) List(ctx context.Context, site string, limit, offset int64) ([]dbq.Dump, error) {
	if site != "" {
		d := site
		return s.db.GetDumpsBySite(ctx, dbq.GetDumpsBySiteParams{
			SiteDomain: &d,
			Limit:      limit,
			Offset:     offset,
		})
	}
	return s.db.GetDumps(ctx, dbq.GetDumpsParams{Limit: limit, Offset: offset})
}

// Clear deletes all dumps.
func (s *Store) Clear(ctx context.Context) error {
	return s.db.DeleteAllDumps(ctx)
}

func ptrString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func ptrInt64(n int64) *int64 {
	if n == 0 {
		return nil
	}
	return &n
}
