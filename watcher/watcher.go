package watcher

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/fsnotify/fsnotify"
	"github.com/telnet2/pm/pubsub"
)

type WatchEvent struct {
	Name string `yaml:"name"` // Name is the name of the origin watcher set
	Path string // Path is the path to the modified file or directory
	Type string // Type holds the kind of event that happened (create, write, remove)
}

// WatcherSet is a set of directories or files to watch.
type WatcherSet struct {
	Name  string   `yaml:"name" json:"name"`   // Name is the name of this set
	Watch []string `yaml:"watch" json:"watch"` // A list of glob patterns to watch
}

// Watcher is a code monitor for multiple projects
type Watcher struct {
	pub    *pubsub.ElemPublisher
	target map[string]*WatcherSet // A set of watch targets
	run    bool
	mu     sync.Mutex
}

// NewWatcher creates a watcher instance.
func NewWatcher(pub *pubsub.ElemPublisher) *Watcher {
	retVal := &Watcher{pub: pub, target: make(map[string]*WatcherSet), run: false}
	return retVal
}

// Start starts a new go-routine that monitors the watcher set and publishes an event when there's code change.
// Start returns an error channel. Whenever an error occurs within Start, that error will be fed into the error channel.
// Start and its goroutine will then return, leaving the error channel with only the last error that occurred
// If there is no error, checking the channel will return a nil value
// Errors will occur under the following circumstances:
// there is already a Start goroutine running
// fsnotify could not create a fileWatcher
// filepath could not perform a walk of the target project
// a directory cannot be added to the filewatcher
// filewatcher failed in reported/receiving an event
// goroutine failed to get info about the file/directory
// In the case of an error, the returned channel will close
func (w *Watcher) Start() chan error {
	errChannel := make(chan error, 1)
	// Return if w.run is already true
	// Don't want to run multiple processes
	w.mu.Lock()
	if w.run {
		errChannel <- errors.New("we already have a start running")
		w.mu.Unlock()
		close(errChannel)
		return errChannel
	}
	w.run = true // Set to true
	w.mu.Unlock()
	// We need to tell fsnotify what to watch
	// fsnotify is NOT recursive, need to tell it to watch every directory
	fileWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		errChannel <- err
		close(errChannel)
		return errChannel
	}
	w.mu.Lock()
	for project := range w.target {
		err = filepath.Walk(project, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				err = fileWatcher.Add(path)
				if err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			errChannel <- err
			close(errChannel)
			w.mu.Unlock()
			return errChannel
		}
	}
	w.mu.Unlock()

	// Here is where the go routine goes, checking to see if the event matches any glob patterns
	go func() {
		for {
			w.mu.Lock()
			if !w.run {
				// Won't really change the value, leaves w.run as 1
				// If it ever fails, we know w.run has been set to 0
				w.mu.Unlock()
				return
			}
			w.mu.Unlock()
			select {
			case event, ok := <-fileWatcher.Events:
				// We only care about write, create, delete, and rename for directories
				// Don't think we need to worry about renames or chmod
				if event.Op == fsnotify.Chmod {
					// Skip chmods
					continue
				}
				// fsnotify doesn't notify us when directories are deleted
				// can't remove from fileWatcher without an expensive procedure, ignore for now
				// if a directory is deleted you won't get events from them anyways
				if !ok {
					err = errors.New("error detecting event")
					errChannel <- err
					return
				}
				var watcherset *WatcherSet
				// The event needs to match a project directory
				w.mu.Lock()
				for project := range w.target {
					if strings.Index(event.Name, project) == 0 {
						// That's the project, now see if it matches the glob patterns
						watcherset = w.target[project]
						break
					}
				}
				w.mu.Unlock()
				if watcherset == nil {
					err = errors.New("could not find a watcherset that matches the file event")
					errChannel <- err
					close(errChannel)
					return
				}
				// loop through all the glob patterns, see if it matches
				for _, globpattern := range watcherset.Watch {
					// There is room for easy error here. If the globpattern starts with ./ its not going to work
					rslt1, _ := doublestar.Match(globpattern, event.Name)
					rslt2, _ := doublestar.Match(globpattern, "./"+event.Name)
					if rslt1 || rslt2 {
						// We have an event we need to publish
						pubEvent := &WatchEvent{
							Name: watcherset.Name,
							Path: event.Name,
							Type: event.Op.String(),
						}
						w.pub.Publish(pubEvent)
						if event.Op == fsnotify.Create || event.Op == fsnotify.Rename {
							fileInfo, err := os.Stat(event.Name)
							if err != nil {
								errChannel <- err
								close(errChannel)
								return
							}
							// Only want to trigger this when we create in a
							// directory that matches our glob pattern anyways
							if fileInfo.IsDir() {
								err = fileWatcher.Add(event.Name)
								if err != nil {
									errChannel <- err
									close(errChannel)
									return
								}
							}
						}
						errChannel <- nil
						break
					}
				}
			}
		}
	}()
	return errChannel
}

// Dispose stops the go-routine of this watcher and releases all resources used by this watcher
func (w *Watcher) Dispose() {
	w.mu.Lock()
	w.run = false
	w.mu.Unlock()
}

// Add adds a set of files to a watcher.
func (w *Watcher) Add(ws *WatcherSet) error {
	w.mu.Lock()
	if w.run {
		w.mu.Unlock()
		return errors.New("we already have a start running")
	}
	// if there are no paths or any error happens return
	if ws == nil {
		return errors.New("nil WatcherSet passed")
	}
	if len(ws.Watch) == 0 {
		return errors.New("WatcherSet has no paths")
	}
	// Generate all glob pattern possibilities
	w.target[ws.Name] = ws
	w.mu.Unlock()
	return nil
}

// Remove the watch set from this watcher.
func (w *Watcher) Remove(name string) {
	w.mu.Lock()
	if w.run {
		w.Dispose()
		delete(w.target, name)
		w.Start()
	} else {
		delete(w.target, name)
	}
	w.mu.Unlock()
}
