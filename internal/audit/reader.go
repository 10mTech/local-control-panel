package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Record struct {
	Entry
	File string
	Line int
}

// Chain is the result of checking every line's prev against the line before.
type Chain struct {
	OK       bool
	Lines    int
	BrokenAt string // "2026-10.jsonl:12" when not OK
}

// ReadAll returns every record, oldest first, and the chain check.
func ReadAll(dir string) ([]Record, Chain, error) {
	files, err := monthFiles(dir)
	if err != nil {
		return nil, Chain{}, err
	}
	var recs []Record
	chain := Chain{OK: true}
	var prevLine []byte
	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			return nil, chain, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
		n := 0
		for sc.Scan() {
			n++
			raw := sc.Bytes()
			if len(raw) == 0 {
				continue
			}
			var e Entry
			where := fmt.Sprintf("%s:%d", filepath.Base(path), n)
			if err := json.Unmarshal(raw, &e); err != nil {
				if chain.OK {
					chain.OK, chain.BrokenAt = false, where
				}
				prevLine = append([]byte(nil), raw...)
				continue
			}
			if chain.OK && e.Prev != hashLine(prevLine) {
				chain.OK, chain.BrokenAt = false, where
			}
			chain.Lines++
			prevLine = append([]byte(nil), raw...)
			recs = append(recs, Record{Entry: e, File: filepath.Base(path), Line: n})
		}
		err = sc.Err()
		f.Close()
		if err != nil {
			return nil, chain, err
		}
	}
	return recs, chain, nil
}
