// This program is free software: you can redistribute it and/or modify it
// under the terms of the GNU General Public License as published by the Free
// Software Foundation, either version 3 of the License, or (at your option)
// any later version.
//
// This program is distributed in the hope that it will be useful, but
// WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU General
// Public License for more details.
//
// You should have received a copy of the GNU General Public License along
// with this program.  If not, see <http://www.gnu.org/licenses/>.

// Package bolt is a BoltDB TiddlerStore backend.
package sqlite

import (
	"bytes"
	"context"
	"encoding/json"

	"database/sql"
	_ "github.com/mattn/go-sqlite3"

	"github.com/opennota/widdly/store"
)

// sqliteStore is a sqliteDB store for tiddlers.
type sqliteStore struct {
	db *sql.DB
}

func init() {
	if store.MustOpen != nil {
		panic("attempt to use two different backends at the same time!")
	}
	store.MustOpen = MustOpen
}

// MustOpen opens the BoltDB file specified as dataSource,
// creates the necessary buckets and returns a TiddlerStore.
// MustOpen panics if there is an error.
func MustOpen(dataSource string) store.TiddlerStore {
	db, err := sql.Open("sqlite3", dataSource)
	if err != nil {
		panic(err)
	}
	initStmt := `
		CREATE TABLE tiddler (id integer not null primary key AUTOINCREMENT, meta text, content text, revision integer);
	`
	_, err = db.Exec(initStmt)
	return &sqliteStore{db}
}

// Get retrieves a tiddler from the store by key (title).
func (s *sqliteStore) Get(_ context.Context, key string) (store.Tiddler, error) {
	t := store.Tiddler{WithText: true}
	getStmt, err := s.db.Prepare(`SELECT meta, content FROM tiddler WHERE meta LIKE ? LIMIT 1`)
	var meta string
	var content string
	err = getStmt.QueryRow("%" + key + "%").Scan(&meta, &content)
	if err != nil {
		return store.Tiddler{}, err
	}
	t.Meta = make([]byte, len(meta))
	copy(t.Meta, meta)
	t.Text = string(content)
	return t, nil
}

func copyOf(p []byte) []byte {
	q := make([]byte, len(p))
	copy(q, p)
	return q
}

// All retrieves all the tiddlers (mostly skinny) from the store.
// Special tiddlers (like global macros) are returned fat.
func (s *sqliteStore) All(_ context.Context) ([]store.Tiddler, error) {
	tiddlers := []store.Tiddler{}
	rows, err := s.db.Query(`SELECT meta, content FROM tiddler`)
	defer rows.Close()
	for rows.Next() {
		var t store.Tiddler
		var meta string
		var content string
		if err := rows.Scan(&meta, &content); err != nil {
                return nil, err
        }
        t.Meta = []byte(meta)
        if bytes.Contains(t.Meta, []byte(`"$:/tags/Macro"`)) {
			t.Text = string(content)
			t.WithText = true
		}
        tiddlers = append(tiddlers, t)
	}
	if err != nil {
		return nil, err
	}
	return tiddlers, nil
}

func getLastRevision(db *sql.DB, mkey string) int {
	var revision int
	getStmt, err := db.Prepare(`SELECT revision FROM tiddler WHERE meta LIKE ? LIMIT 1`)
	err = getStmt.QueryRow(mkey).Scan(&revision)
	if err == nil {
		return 1
	}
	return revision
}

// Put saves tiddler to the store, incrementing and returning revision.
// The tiddler is also written to the tiddler_history bucket.
func (s *sqliteStore) Put(ctx context.Context, tiddler store.Tiddler) (int, error) {
	var js map[string]interface{}
	err := json.Unmarshal(tiddler.Meta, &js)
	if err != nil {
		return 0, err
	}
	rev := getLastRevision(s.db, tiddler.Key)
	insertStmt, err := s.db.Prepare(`INSERT INTO tiddler(meta, content, revision) VALUES (?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	_, err = insertStmt.Exec(tiddler.Meta, tiddler.Text, rev)
	if err != nil {
		return 0, err
	}
	return rev, nil
}

// Delete deletes a tiddler with the given key (title) from the store.
func (s *sqliteStore) Delete(ctx context.Context, key string) error {
	deleteStmt, err := s.db.Prepare(`DELETE FROM tiddler WHERE meta LIKE ?`)
	if err != nil {
		return err
	}
	_, err = deleteStmt.Exec(key)
	if err != nil {
		return err
	}
	return nil
}
