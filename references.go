package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type referenceRegistry struct {
	mu     sync.Mutex
	opened map[string]string
	mux    *http.ServeMux
}

// Each reader keeps its own document, root and comment store. Child readers
// share the listening port and assets, so opening a reference starts no process.
func (s *server) references() http.HandlerFunc {
	registry := s.referencesRegistry
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", 405)
			return
		}
		var request struct {
			File string `json:"file"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&request) != nil {
			http.Error(w, "invalid reference", 400)
			return
		}
		// A token is not a licence to browse arbitrary files: the current document
		// must explicitly reference this file.
		loaded, err := LoadDoc(s.file)
		allowed := false
		if err == nil && loaded.View().V2 != nil {
			loaded.View().V2.walkBlocks(func(_ string, b *Block) {
				if b.Type == BlockReference && b.Target != nil && b.Target.File == request.File {
					allowed = true
				}
			})
		}
		if !allowed && s.comments != nil {
			for _, thread := range s.commentsView().Threads {
				for _, message := range thread.Messages {
					for _, b := range message.Blocks {
						if b.Type == BlockReference && b.Target != nil && b.Target.File == request.File {
							allowed = true
						}
					}
				}
			}
		}
		if !allowed {
			http.Error(w, "file is not referenced by this walkthrough", 400)
			return
		}
		file, err := referenceFile(s.file, request.File)
		if err != nil {
			http.Error(w, "referenced file is unavailable or outside the walkthrough directory", 400)
			return
		}
		target, err := LoadDoc(file)
		if err != nil || len(target.Errors) != 0 {
			http.Error(w, "referenced walkthrough cannot be read", 400)
			return
		}
		registry.mu.Lock()
		defer registry.mu.Unlock()
		route := registry.opened[file]
		if route == "" {
			route = "/r/" + randomToken() + "/"
			key := commentKey("", file)
			child := &server{file: file, root: s.root, dev: s.dev, token: randomToken(), vendor: s.vendor, web: s.web,
				key: key, title: target.View().Title, comments: openComments(key, target.View().Title), port: s.port, referencesRegistry: registry}
			registry.mux.Handle(route, http.StripPrefix(strings.TrimSuffix(route, "/"), child.routes()))
			registry.opened[file] = route
			if s.port != 0 {
				rememberRun(servedRun{PID: os.Getpid(), Port: s.port, BasePath: strings.TrimSuffix(route, "/"), Token: child.token, Key: key, Title: child.title, File: file, At: time.Now()})
			}
		}
		writeJSON(w, 200, map[string]string{"url": route})
	}
}

func referenceFile(containing, relative string) (string, error) {
	relative = strings.TrimPrefix(relative, "./")
	if !portablePath(relative) {
		return "", &referencePathError{}
	}
	root, err := filepath.EvalSymlinks(filepath.Dir(containing))
	if err != nil {
		return "", err
	}
	file, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, file)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", &referencePathError{}
	}
	return file, nil
}

type referencePathError struct{}

func (*referencePathError) Error() string {
	return "reference must stay inside the walkthrough directory"
}
