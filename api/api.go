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

// Package api registers needed HTTP handlers.
package api

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/opennota/widdly/store"
)

var (
	// Store should point to an implementation of TiddlerStore.
	Store store.TiddlerStore

	// Authenticate is a hook that lets the client of the package to
	// provide some authentication.
	// Authenticate should write to the ResponseWriter iff the user
	// may not access the endpoint.
	Authenticate func(http.ResponseWriter, *http.Request)

	// ServeIndex is a callback that should serve the index page.
	ServeIndex = func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	}
)

func init() {
	http.HandleFunc("/", withLoggingAndAuth(index))
	http.HandleFunc("/status", withLoggingAndAuth(status))
	http.HandleFunc("/recipes/all/tiddlers.json", withLoggingAndAuth(list))
	http.HandleFunc("/recipes/all/tiddlers/", withLoggingAndAuth(tiddler))
	http.HandleFunc("/bags/bag/tiddlers/", withLoggingAndAuth(remove))
}

// internalError logs err to the standard error and returns HTTP 500 Internal Server Error.
func internalError(w http.ResponseWriter, err error) {
	log.Println("ERR", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

// logRequest logs the incoming request.
func logRequest(r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	log.Println(host, r.Method, r.URL, r.Referer(), r.UserAgent())
}

// withLogging is a logging middleware.
func withLogging(f http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logRequest(r)
		f(w, r)
	}
}

type responseWriter struct {
	http.ResponseWriter
	written bool
}

func (w *responseWriter) Write(p []byte) (int, error) {
	w.written = true
	return w.ResponseWriter.Write(p)
}

func (w *responseWriter) WriteHeader(status int) {
	w.written = true
	w.ResponseWriter.WriteHeader(status)
}

// withAuth is an authentication middleware.
func withAuth(f http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if Authenticate == nil {
			f(w, r)
		} else {
			rw := responseWriter{
				ResponseWriter: w,
			}
			Authenticate(&rw, r)
			if !rw.written {
				f(w, r)
			}
		}
	}
}

func withLoggingAndAuth(f http.HandlerFunc) http.HandlerFunc {
	return withAuth(withLogging(f))
}

// index serves the index page.
func index(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	ServeIndex(w, r)
}

// status serves the status JSON.
func status(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"username":"me","space":{"recipe":"all"}}`))
}

// list serves a JSON list of (mostly) skinny tiddlers.
func list(w http.ResponseWriter, r *http.Request) {
	tiddlers, err := Store.All(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}

	var buf bytes.Buffer
	buf.WriteByte('[')
	sep := ""
	for _, t := range tiddlers {
		if len(t.Meta) == 0 {
			continue
		}

		json, err := t.MarshalJSON()
		if err != nil {
			continue
		}

		buf.WriteString(sep)
		sep = ","
		buf.Write(json)
	}
	buf.WriteByte(']')

	w.Header().Set("Content-Type", "application/json")
	w.Write(buf.Bytes())
}

// getTiddler serves a fat tiddler.
func getTiddler(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/recipes/all/tiddlers/")

	t, err := Store.Get(r.Context(), key)
	if err != nil {
		internalError(w, err)
		return
	}

	data, err := t.MarshalJSON()
	if err != nil {
		internalError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// putTiddler saves a tiddler.
func putTiddler(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/recipes/all/tiddlers/")

	var js map[string]interface{}
	err := json.NewDecoder(r.Body).Decode(&js)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	io.Copy(ioutil.Discard, r.Body)

	js["bag"] = "bag"

	text, _ := js["text"].(string)
	delete(js, "text")

	meta, err := json.Marshal(js)
	if err != nil {
		internalError(w, err)
		return
	}

	rev, err := Store.Put(r.Context(), store.Tiddler{
		Key:  key,
		Meta: meta,
		Text: text,
	})
	if err != nil {
		internalError(w, err)
		return
	}

	etag := fmt.Sprintf(`"bag/%s/%d:%032x"`, url.QueryEscape(key), rev, md5.Sum(meta))
	w.Header().Set("ETag", etag)
	w.WriteHeader(http.StatusNoContent)
}

func tiddler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		getTiddler(w, r)
	case "PUT":
		putTiddler(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// remove removes a tiddler.
func remove(w http.ResponseWriter, r *http.Request) {
	if r.Method != "DELETE" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	key := strings.TrimPrefix(r.URL.Path, "/bags/bag/tiddlers/")
	err := Store.Delete(r.Context(), key)
	if err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
