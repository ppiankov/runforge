package scan

import (
	"os"
	"path/filepath"
)

// Language represents the detected primary language of a repo.
type Language int

const (
	LangUnknown Language = iota
	LangGo
	LangPython
	LangMulti
)

func (l Language) String() string {
	switch l {
	case LangGo:
		return "go"
	case LangPython:
		return "python"
	case LangMulti:
		return "multi"
	default:
		return "unknown"
	}
}

// RepoInfo holds metadata about a scanned repository.
type RepoInfo struct {
	Name     string
	Path     string
	Language Language
	HasCmd   bool
	HasDocs  bool
}

// DetectRepo examines a directory and returns RepoInfo.
// Returns nil if the directory is not a git repo.
func DetectRepo(path string) *RepoInfo {
	if !isDir(filepath.Join(path, ".git")) {
		return nil
	}

	info := &RepoInfo{
		Name: filepath.Base(path),
		Path: path,
	}

	hasGo := fileExists(filepath.Join(path, "go.mod"))
	hasPy := fileExists(filepath.Join(path, "pyproject.toml")) || fileExists(filepath.Join(path, "setup.py"))

	switch {
	case hasGo && hasPy:
		info.Language = LangMulti
	case hasGo:
		info.Language = LangGo
	case hasPy:
		info.Language = LangPython
	default:
		info.Language = LangUnknown
	}

	info.HasCmd = isDir(filepath.Join(path, "cmd"))
	info.HasDocs = isDir(filepath.Join(path, "docs"))

	return info
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
