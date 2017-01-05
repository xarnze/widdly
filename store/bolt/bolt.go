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
package bolt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/boltdb/bolt"

	"github.com/opennota/widdly/store"
)

// boltStore is a BoltDB store for tiddlers.
type boltStore struct {
	db *bolt.DB
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
	db, err := bolt.Open(dataSource, 0600, nil)
	if err != nil {
		panic(err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("tiddler"))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte("tiddler_history"))
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	return &boltStore{db}
}

// Get retrieves a tiddler from the store by key (title).
func (s *boltStore) Get(_ context.Context, key string) (store.Tiddler, error) {
	t := store.Tiddler{WithText: true}
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("tiddler"))
		meta := b.Get([]byte(key + "|1"))
		if meta == nil {
			return store.ErrNotFound
		}
		t.Meta = make([]byte, len(meta))
		copy(t.Meta, meta)
		t.Text = string(b.Get([]byte(key + "|2")))
		return nil
	})
	if err != nil {
		return store.Tiddler{}, err
	}
	return t, nil
}

func copyOf(p []byte) []byte {
	q := make([]byte, len(p))
	copy(q, p)
	return q
}

// All retrieves all the tiddlers (mostly skinny) from the store.
// Special tiddlers (like global macros) are returned fat.
func (s *boltStore) All(_ context.Context) ([]store.Tiddler, error) {
	tiddlers := []store.Tiddler{}
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("tiddler"))
		c := b.Cursor()
		for k, meta := c.First(); k != nil; k, meta = c.Next() {
			if len(meta) == 0 {
				c.Next()
				continue
			}
			var t store.Tiddler
			t.Meta = copyOf(meta)
			_, text := c.Next()
			if bytes.Contains(t.Meta, []byte(`"$:/tags/Macro"`)) {
				t.Text = string(text)
				t.WithText = true
			}
			tiddlers = append(tiddlers, t)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return tiddlers, nil
}

func getLastRevision(b *bolt.Bucket, mkey []byte) int {
	var meta struct{ Revision int }
	data := b.Get(mkey)
	if data != nil && json.Unmarshal(data, &meta) == nil {
		return meta.Revision + 1
	}
	return 1
}

// Put saves tiddler to the store, incrementing and returning revision.
// The tiddler is also written to the tiddler_history bucket.
func (s *boltStore) Put(ctx context.Context, tiddler store.Tiddler) (int, error) {
	var js map[string]interface{}
	err := json.Unmarshal(tiddler.Meta, &js)
	if err != nil {
		return 0, err
	}
	var rev int
	err = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("tiddler"))
		mkey := []byte(tiddler.Key + "|1")

		rev = getLastRevision(b, mkey)
		js["revision"] = rev
		data, err := json.Marshal(js)
		if err != nil {
			return err
		}

		err = b.Put(mkey, data)
		if err != nil {
			return err
		}
		err = b.Put([]byte(tiddler.Key+"|2"), []byte(tiddler.Text))
		if err != nil {
			return err
		}

		js["text"] = tiddler.Text
		data, err = json.Marshal(js)
		if err != nil {
			return err
		}
		history := tx.Bucket([]byte("tiddler_history"))
		err = history.Put([]byte(fmt.Sprintf("%s#%d", tiddler.Key, rev)), data)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return 0, err
	}
	return rev, nil
}

// Delete deletes a tiddler with the given key (title) from the store.
func (s *boltStore) Delete(ctx context.Context, key string) error {
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("tiddler"))
		mkey := []byte(key + "|1")

		rev := getLastRevision(b, mkey)

		err := b.Put(mkey, nil)
		if err != nil {
			return err
		}
		err = b.Put([]byte(key+"|2"), nil)
		if err != nil {
			return err
		}

		history := tx.Bucket([]byte("tiddler_history"))
		err = history.Put([]byte(fmt.Sprintf("%s#%d", key, rev)), nil)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}
	return nil
}
