package turnlog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func readLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var row map[string]any
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			t.Fatalf("decode %q: %v", sc.Text(), err)
		}
		out = append(out, row)
	}
	return out
}

func TestAppend_QuerySourceOmittedWhenEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "turn-timings.jsonl")
	if err := Append(path, Record{Outcome: "done"}); err != nil {
		t.Fatal(err)
	}
	if err := Append(path, Record{Outcome: "done", QuerySource: "extract_memories"}); err != nil {
		t.Fatal(err)
	}
	rows := readLines(t, path)
	if _, ok := rows[0]["query_source"]; ok {
		t.Errorf("empty query_source should be omitted: %v", rows[0])
	}
	if rows[1]["query_source"] != "extract_memories" {
		t.Errorf("row 2 = %v", rows[1])
	}
}

func TestAppend_ConcurrentWritersKeepLinesWhole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "turn-timings.jsonl")
	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := Append(path, Record{Outcome: "done", APIRequests: i, Model: "m"}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if rows := readLines(t, path); len(rows) != n {
		t.Fatalf("got %d lines, want %d", len(rows), n)
	}
}
