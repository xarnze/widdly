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

package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opennota/widdly/store"
)

type testStore struct {
	get func(context.Context, string) (store.Tiddler, error)
	all func(context.Context) ([]store.Tiddler, error)
	put func(context.Context, store.Tiddler) (int, error)
	del func(context.Context, string) error
}

func (ts *testStore) Get(ctx context.Context, key string) (store.Tiddler, error) {
	if ts.get == nil {
		return store.Tiddler{}, nil
	}
	return ts.get(ctx, key)
}

func (ts *testStore) All(ctx context.Context) ([]store.Tiddler, error) {
	if ts.all == nil {
		return nil, nil
	}
	return ts.all(ctx)
}

func (ts *testStore) Put(ctx context.Context, tiddler store.Tiddler) (int, error) {
	if ts.put == nil {
		return 0, nil
	}
	return ts.put(ctx, tiddler)
}

func (ts *testStore) Delete(ctx context.Context, key string) error {
	if ts.del == nil {
		return nil
	}
	return ts.del(ctx, key)
}

func TestIndex(t *testing.T) {
	ServeIndex = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/html")
		w.Write([]byte("index"))
	}
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	index(w, r)
	if w.Code != 200 {
		t.Errorf("want 200 OK, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if want := "text/html"; ct != want {
		t.Errorf("want %s, got %v", want, ct)
	}
	body := w.Body.String()
	if want := "index"; body != want {
		t.Errorf("want %q, got %q", want, body)
	}
}

func TestStatus(t *testing.T) {
	r := httptest.NewRequest("GET", "/status", nil)
	w := httptest.NewRecorder()
	status(w, r)
	if w.Code != 200 {
		t.Errorf("want 200 OK, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if want := "application/json"; ct != want {
		t.Errorf("want %s, got %v", want, ct)
	}
	body := w.Body.String()
	if want := `{"username":"me","space":{"recipe":"all"}}`; body != want {
		t.Errorf("want %q, got %q", want, body)
	}
}

func TestList(t *testing.T) {
	Store = &testStore{
		all: func(context.Context) ([]store.Tiddler, error) {
			return []store.Tiddler{
				{"tiddler1", []byte(`{"author":"robpike"}`), "", false},
				{"tiddler2", []byte(`{"author":"bradfitz"}`), "text", false},
			}, nil
		},
	}
	r := httptest.NewRequest("GET", "/recipes/all/tiddlers.json", nil)
	w := httptest.NewRecorder()
	list(w, r)
	if w.Code != 200 {
		t.Errorf("want 200 OK, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if want := "application/json"; ct != want {
		t.Errorf("want %s, got %v", want, ct)
	}
	body := strings.TrimRight(w.Body.String(), "\n")
	if want := `[{"author":"robpike"},{"author":"bradfitz"}]`; body != want {
		t.Errorf("want %q, got %q", want, body)
	}
}

func TestGetTiddler(t *testing.T) {
	Store = &testStore{
		get: func(_ context.Context, key string) (store.Tiddler, error) {
			if key != "tiddler2" {
				return store.Tiddler{}, nil
			}
			return store.Tiddler{
				"tiddler2", []byte(`{"author":"bradfitz"}`), "text of the second tiddler", true,
			}, nil
		},
	}
	r := httptest.NewRequest("GET", "/recipes/all/tiddlers/tiddler2", nil)
	w := httptest.NewRecorder()
	tiddler(w, r)
	if w.Code != 200 {
		t.Errorf("want 200 OK, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if want := "application/json"; ct != want {
		t.Errorf("want %s, got %v", want, ct)
	}
	body := w.Body.String()
	if want := `{"author":"bradfitz","text":"text of the second tiddler"}`; body != want {
		t.Errorf("want %q, got %q", want, body)
	}
}

func TestPutTiddler(t *testing.T) {
	putCalled := false
	Store = &testStore{
		put: func(_ context.Context, tiddler store.Tiddler) (int, error) {
			putCalled = true
			if tiddler.Key != "tiddler2" {
				return 0, errors.New(`expected key to be "tiddler2"`)
			}
			if string(tiddler.Meta) != `{"author":"bradfitz","bag":"bag"}` {
				return 0, errors.New(`expected meta to be ""`)
			}
			if tiddler.Text != "text of the second tiddler" {
				return 0, errors.New(`expected text to be "text of the second tiddler"`)
			}
			return 1, nil
		},
	}
	r := httptest.NewRequest("PUT", "/recipes/all/tiddlers/tiddler2", strings.NewReader(`
		{
			"author": "bradfitz",
			"text" :"text of the second tiddler"
		}
	`))
	w := httptest.NewRecorder()
	tiddler(w, r)
	if w.Code != 204 {
		t.Errorf("want 204 No Content, got %d", w.Code)
	}
	ct := w.Header().Get("ETag")
	if ct == "" {
		t.Errorf("want ETag header, got none")
	}
	if !putCalled {
		t.Errorf("expected Store.Put to be called")
	}
}

func TestDeleteTiddler(t *testing.T) {
	delCalled := false
	Store = &testStore{
		del: func(_ context.Context, key string) error {
			delCalled = true
			if key != "tiddler2" {
				return errors.New(`expected key to be "tiddler2"`)
			}
			return nil
		},
	}
	r := httptest.NewRequest("DELETE", "/bags/bag/tiddlers/tiddler2", nil)
	w := httptest.NewRecorder()
	remove(w, r)
	if w.Code != 204 {
		t.Errorf("want 204 No Content, got %d", w.Code)
	}
	if !delCalled {
		t.Errorf("expected Store.Delete to be called")
	}
}
