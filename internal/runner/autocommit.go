package runner

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ppiankov/tokencontrol/internal/task"
)

const autoCommitTimeout = 30 * time.Second

// AutoCommit checks for uncommitted changes in repoDir and commits them
// using a deterministic commit message derived from the task. Returns true
// if a commit was created, false if the repo was already clean.
func AutoCommit(ctx context.Context, repoDir string, t *task.Task) (bool, error) {
	commitCtx, cancel := context.WithTimeout(ctx, autoCommitTimeout)
	defer cancel()

	// check for uncommitted changes
	files, err := changedFiles(commitCtx, repoDir)
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	if len(files) == 0 {
		return false, nil // agent already committed
	}

	// stage only the changed files
	args := append([]string{"add", "--"}, files...)
	if err := runGitCmd(commitCtx, repoDir, args...); err != nil {
		return false, fmt.Errorf("git add: %w", err)
	}

	// commit with derived message
	msg := DeriveCommitMessage(t)
	if err := runGitCmd(commitCtx, repoDir, "commit", "-m", msg); err != nil {
		return false, fmt.Errorf("git commit: %w", err)
	}

	slog.Info("auto-committed changes", "task", t.ID, "files", len(files), "message", msg)
	return true, nil
}

// changedFiles returns file paths from git status --porcelain.
// Untracked files (status ??) are filtered through git check-ignore to avoid
// attempting to add files covered by .gitignore.
func changedFiles(ctx context.Context, repoDir string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var files []string
	var untrackedFiles []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if len(line) < 4 {
			continue
		}
		// porcelain format: XY <space> filename
		// columns 0-1 are status, column 2 is space, column 3+ is filename
		status := line[:2]
		file := strings.TrimSpace(line[2:])
		// handle renamed files: "old -> new"
		if idx := strings.Index(file, " -> "); idx >= 0 {
			file = file[idx+4:]
		}
		if file == "" {
			continue
		}
		if status == "??" {
			untrackedFiles = append(untrackedFiles, file)
		} else {
			files = append(files, file)
		}
	}

	// filter untracked files: exclude those covered by .gitignore
	for _, f := range untrackedFiles {
		if !isGitIgnored(ctx, repoDir, f) {
			files = append(files, f)
		}
	}

	return files, nil
}

// isGitIgnored returns true if the file is covered by .gitignore.
func isGitIgnored(ctx context.Context, repoDir, file string) bool {
	cmd := exec.CommandContext(ctx, "git", "check-ignore", "-q", file)
	cmd.Dir = repoDir
	return cmd.Run() == nil // exit 0 = ignored, exit 1 = not ignored
}

// runGitCmd executes a git command in the given directory.
// Sets tokencontrol identity so commits work in environments without global git config (e.g. CI).
func runGitCmd(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=tokencontrol",
		"GIT_AUTHOR_EMAIL=tokencontrol@localhost",
		"GIT_COMMITTER_NAME=tokencontrol",
		"GIT_COMMITTER_EMAIL=tokencontrol@localhost",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// commitTypePrefixes maps title prefixes to conventional commit types.
var commitTypePrefixes = []struct {
	prefix string
	typ    string
}{
	{"fix", "fix"},
	{"resolve", "fix"},
	{"repair", "fix"},
	{"add", "feat"},
	{"create", "feat"},
	{"implement", "feat"},
	{"doc", "docs"},
	{"readme", "docs"},
	{"refactor", "refactor"},
	{"clean", "refactor"},
	{"simplify", "refactor"},
	{"test", "test"},
}

// DeriveCommitMessage generates a conventional commit message from task metadata.
func DeriveCommitMessage(t *task.Task) string {
	title := strings.TrimSpace(t.Title)
	if title == "" {
		title = t.ID
	}

	lower := strings.ToLower(title)
	typ := "chore"
	for _, p := range commitTypePrefixes {
		if strings.HasPrefix(lower, p.prefix) {
			typ = p.typ
			// strip the full first word (not just the prefix) and separators
			if idx := strings.IndexByte(title, ' '); idx >= 0 {
				rest := strings.TrimLeft(title[idx:], " :,-")
				if rest != "" {
					title = rest
				}
			}
			break
		}
	}

	// lowercase first char unless it looks like an acronym (all caps)
	if len(title) > 1 && title[0] >= 'A' && title[0] <= 'Z' && (title[1] < 'A' || title[1] > 'Z') {
		title = strings.ToLower(title[:1]) + title[1:]
	}

	msg := fmt.Sprintf("%s: %s", typ, title)
	if len(msg) > 72 {
		msg = msg[:72]
	}
	return msg
}
