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
package flatFile

import (
	"bytes"
	"context"
	"strings"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
    "io/ioutil"
    "regexp"
    "strconv"

	"github.com/opennota/widdly/store"
)

// flatFileStore is a sqliteDB store for tiddlers.
type flatFileStore struct {
	storePath string
	tiddlersPath string
	tiddlerHistoryPath string
}

func init() {
	if store.MustOpen != nil {
		panic("attempt to use two different backends at the same time!")
	}
	store.MustOpen = MustOpen
}

func exists(path string) (bool, error) {
    _, err := os.Stat(path)
    if err == nil { return true, nil }
    if os.IsNotExist(err) { return false, nil }
    return true, err
}

func checkExt(pathS string, ext string) []string {
	var files []string
	filepath.Walk(pathS, func(path string, f os.FileInfo, _ error) error {
		if !f.IsDir() {
			r, err := regexp.MatchString(ext, f.Name())
			if err == nil && r {
				files = append(files, f.Name())
			}
		}
		return nil
	})
	return files
}

// MustOpen opens the BoltDB file specified as dataSource,
// creates the necessary buckets and returns a TiddlerStore.
// MustOpen panics if there is an error.
func MustOpen(dataSource string) store.TiddlerStore {
	storePath := filepath.Join(".", dataSource)
	if _, err := os.Stat(storePath); os.IsNotExist(err) {
	    os.Mkdir(storePath, os.ModePerm)
	}

	tiddlersPath := filepath.Join(storePath, "tiddlers")
	if _, err := os.Stat(tiddlersPath); os.IsNotExist(err) {
	    os.Mkdir(tiddlersPath, os.ModePerm)
	}

	tiddlerHistoryPath := filepath.Join(storePath, "tiddlerHistory")
	if _, err := os.Stat(tiddlerHistoryPath); os.IsNotExist(err) {
	    os.Mkdir(tiddlerHistoryPath, os.ModePerm)
	}
	return &flatFileStore{storePath, tiddlersPath, tiddlerHistoryPath}
}

// Get retrieves a tiddler from the store by key (title).
func (s *flatFileStore) Get(_ context.Context, key string) (store.Tiddler, error) {
	t := store.Tiddler{WithText: true}
	tiddlerPath := filepath.Join(s.tiddlersPath, key + ".tid")
	tiddlerMetaPath := filepath.Join(s.tiddlersPath, key + ".meta")
	if _, err := os.Stat(tiddlerPath); os.IsNotExist(err) {
		return t, store.ErrNotFound
	}else {
		meta, err := ioutil.ReadFile(tiddlerMetaPath)
		if err != nil {
			return store.Tiddler{}, err
		}
		tiddler, err := ioutil.ReadFile(tiddlerPath)
		if err != nil {
			return store.Tiddler{}, err
		}
		t.Meta = make([]byte, len(meta))
		copy(t.Meta, meta)
		t.Text = string(tiddler)
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
func (s *flatFileStore) All(_ context.Context) ([]store.Tiddler, error) {
	tiddlers := []store.Tiddler{}
	files := checkExt(s.tiddlersPath, ".meta")
	for _, file := range files {
		var t store.Tiddler
		meta, _ := ioutil.ReadFile(filepath.Join(s.tiddlersPath, file))
		t.Meta = make([]byte, len(meta))
		copy(t.Meta, meta)
		if bytes.Contains(t.Meta, []byte(`"$:/tags/Macro"`)) {
			var extension = filepath.Ext(file)
			var tiddlerPath = file[0:len(file)-len(extension)]
			tiddler, _ := ioutil.ReadFile(tiddlerPath + ".tid")
			t.Text = string(tiddler)
			t.WithText = true
		}
		tiddlers = append(tiddlers, t)
	}
	return tiddlers, nil
}

func getLastRevision(s *flatFileStore, key string) int {
	var files []string
	filepath.Walk(s.tiddlerHistoryPath, func(path string, f os.FileInfo, _ error) error {
		if !f.IsDir() {
			r, err := regexp.MatchString(key + "#\\d+", f.Name())
			if err == nil && r {
				files = append(files, f.Name())
			}
		}
		return nil
	})

	highestRev := 0

	for _, file := range files {
		filePart := strings.Split(file, "#")
		rev, _ := strconv.Atoi(filePart[1])
		if(rev > highestRev){
			highestRev = rev
		}
	}

	return highestRev + 1
}

// Put saves tiddler to the store, incrementing and returning revision.
// The tiddler is also written to the tiddler_history bucket.
func (s *flatFileStore) Put(ctx context.Context, tiddler store.Tiddler) (int, error) {
	var js map[string]interface{}
	err := json.Unmarshal(tiddler.Meta, &js)
	if err != nil {
		return 0, err
	}
	rev := getLastRevision(s, tiddler.Key)
	data, _ := json.Marshal(js)

	err = ioutil.WriteFile(filepath.Join(s.tiddlersPath, tiddler.Key + ".tid"), []byte(tiddler.Text), 0644)
	err = ioutil.WriteFile(filepath.Join(s.tiddlersPath, tiddler.Key + ".meta"), tiddler.Meta, 0644)
	err = ioutil.WriteFile(filepath.Join(s.tiddlerHistoryPath, fmt.Sprintf("%s#%d", tiddler.Key, rev)), data, 0644)

	return rev, nil
}

// Delete deletes a tiddler with the given key (title) from the store.
func (s *flatFileStore) Delete(ctx context.Context, key string) error {
	err := os.Remove(filepath.Join(s.tiddlersPath, key + ".tid"))
	if err != nil {
		return err
	}
	err = os.Remove(filepath.Join(s.tiddlersPath, key + ".meta"))
	if err != nil {
		return err
	}
	return nil
}
