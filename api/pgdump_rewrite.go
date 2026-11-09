package api

import (
	"bufio"
	"bytes"
	"io"
)

// pgIdempotentServers rewrites CREATE SERVER and CREATE USER MAPPING in a
// plain pg_dump stream to their IF NOT EXISTS forms. devctl wires the
// clickhouse foreign server into template1, so every database already has it
// and a dump of one would otherwise fail on import. Rows inside COPY blocks
// pass through unchanged.
type pgIdempotentServers struct {
	br     *bufio.Reader
	out    []byte
	inCopy bool
	err    error
}

func newPgIdempotentServers(r io.Reader) io.Reader {
	return &pgIdempotentServers{br: bufio.NewReaderSize(r, 64<<10)}
}

var (
	pgCreateServer      = []byte("CREATE SERVER ")
	pgCreateServerSafe  = []byte("CREATE SERVER IF NOT EXISTS ")
	pgCreateMapping     = []byte("CREATE USER MAPPING FOR ")
	pgCreateMappingSafe = []byte("CREATE USER MAPPING IF NOT EXISTS FOR ")
	pgCopyStart         = []byte("COPY ")
	pgCopyFromStdin     = []byte("FROM stdin;")
	pgCopyEnd           = []byte(`\.`)
)

func (p *pgIdempotentServers) Read(b []byte) (int, error) {
	for len(p.out) == 0 {
		if p.err != nil {
			return 0, p.err
		}
		line, err := p.br.ReadBytes('\n')
		p.err = err
		p.out = p.rewrite(line)
	}
	n := copy(b, p.out)
	p.out = p.out[n:]
	return n, nil
}

func (p *pgIdempotentServers) rewrite(line []byte) []byte {
	if p.inCopy {
		if bytes.Equal(bytes.TrimRight(line, "\r\n"), pgCopyEnd) {
			p.inCopy = false
		}
		return line
	}
	switch {
	case bytes.HasPrefix(line, pgCopyStart) && bytes.Contains(line, pgCopyFromStdin):
		p.inCopy = true
	case bytes.HasPrefix(line, pgCreateServer) && !bytes.HasPrefix(line, pgCreateServerSafe):
		return append(append([]byte{}, pgCreateServerSafe...), line[len(pgCreateServer):]...)
	case bytes.HasPrefix(line, pgCreateMapping):
		return append(append([]byte{}, pgCreateMappingSafe...), line[len(pgCreateMapping):]...)
	}
	return line
}
