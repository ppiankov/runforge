package sentinel

import (
	"os"
	"path/filepath"
)

// Dirs holds the directory layout for sentinel state.
type Dirs struct {
	Ingested   string // source: approved WOs land here
	Processing string // WOs currently being executed
	Completed  string // successfully executed WOs
	Failed     string // WOs that failed execution
}

// NewDirs creates a Dirs from the ingested and state directories.
func NewDirs(ingested, stateDir string) Dirs {
	return Dirs{
		Ingested:   ingested,
		Processing: filepath.Join(stateDir, "processing"),
		Completed:  filepath.Join(stateDir, "completed"),
		Failed:     filepath.Join(stateDir, "failed"),
	}
}

// EnsureDirs creates all sentinel directories.
func EnsureDirs(d Dirs) error {
	for _, dir := range []string{d.Processing, d.Completed, d.Failed} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}
