// Copyright (c) 2013 ActiveState Software Inc. All rights reserved.

package watch

import (
	"os"
	"time"

	"github.com/bcandrea/tail/util"
	"gopkg.in/tomb.v1"
)

// PollingFileWatcher polls the file for changes.
type PollingFileWatcher struct {
	Filename string
	Size     int64
}

func NewPollingFileWatcher(filename string) *PollingFileWatcher {
	fw := &PollingFileWatcher{filename, 0}
	return fw
}

var POLL_DURATION time.Duration

func (fw *PollingFileWatcher) BlockUntilExists(t *tomb.Tomb) error {
	for {
		if _, err := os.Stat(fw.Filename); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		select {
		case <-time.After(POLL_DURATION):
			continue
		case <-t.Dying():
			return tomb.ErrDying
		}
	}
	panic("unreachable")
}

func (fw *PollingFileWatcher) ChangeEvents(t *tomb.Tomb, origFi os.FileInfo) *FileChanges {
	changes := NewFileChanges()
	var prevModTime time.Time

	// XXX: use tomb.Tomb to cleanly manage these goroutines. replace
	// the fatal (below) with tomb's Kill.

	fw.Size = origFi.Size()

	go func() {
		defer changes.Close()

		var retry int = 0

		prevSize := fw.Size
		for {
			select {
			case <-t.Dying():
				return
			default:
			}

			time.Sleep(POLL_DURATION)
			fi, err := os.Stat(fw.Filename)
			if err != nil {
				if os.IsNotExist(err) {
					// File does not exist (has been deleted).
					changes.NotifyDeleted()
					return
				}

				if permissionErrorRetry(err, &retry) {
					continue
				}

				// XXX: report this error back to the user
				util.Fatal("Failed to stat file %v: %v", fw.Filename, err)
			}

			// File got moved/renamed?
			if !os.SameFile(origFi, fi) {
				changes.NotifyDeleted()
				return
			}

			// File got truncated?
			fw.Size = fi.Size()
			if prevSize > 0 && prevSize > fw.Size {
				changes.NotifyTruncated()
				prevSize = fw.Size
				continue
			}

			// File was appended to (changed)?
			// The check on size is needed on Windows server 2008+
			// due to a new "feature" of the OS (see http://bit.ly/1NCTeLc)
			modTime := fi.ModTime()
			if modTime != prevModTime || fw.Size > prevSize {
				prevModTime = modTime
				changes.NotifyModified()
			}

			prevSize = fw.Size
		}
	}()

	return changes
}

func init() {
	POLL_DURATION = 250 * time.Millisecond
}
