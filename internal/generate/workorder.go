package generate

import (
	"fmt"
	"regexp"
	"strings"
)

// WOStatus represents the status of a work order.
type WOStatus int

const (
	StatusPlanned WOStatus = iota
	StatusInProgress
	StatusDone
)

// WorkOrder represents a parsed work order from a markdown file.
type WorkOrder struct {
	RawID      string   // e.g., "01", "C01", "CW01" (without "WO-" prefix)
	Title      string   // from heading after ": "
	Status     WOStatus // planned, in_progress, done
	Priority   int      // 1=high, 2=medium, 3=low
	Summary    string   // Goal or first Summary paragraph
	Acceptance []string // acceptance criteria bullet points
	DependsOn  string   // raw WO suffix of dependency (e.g., "01")
	Runner     string   // "codex" or "claude" if detected in text
}

// Compiled regex patterns.
var (
	// Matches: ## WO-01: Title  or  ### WO-C01: Title  or  ## WO-CW01: Title ✅
	reWOHeading = regexp.MustCompile(`^#{2,3}\s+WO-([A-Za-z0-9.-]+):\s+(.+?)(?:\s*✅)?\s*$`)

	// Matches: **Status:** `[ ]` planned  or  `[x]` done  etc.
	reStatus = regexp.MustCompile("\\*\\*Status:\\*\\*\\s*`\\[(.)]`")

	// Matches: **Priority:** high|medium|low (with optional trailing text)
	rePriority = regexp.MustCompile(`(?i)\*\*Priority:\*\*\s*(high|medium|low)`)

	// Matches: **Goal:** text
	reGoal = regexp.MustCompile(`^\*\*Goal:\*\*\s*(.+)$`)

	// Matches dependency references in prose. Requires at least one digit to avoid placeholders like "WO-X".
	reDepends = regexp.MustCompile(`(?i)(?:depends\s+on|requires|builds\s+on|after)\s+WO-([A-Za-z]*\d[A-Za-z0-9.-]*)`)

	// Matches section headings within a WO.
	reSectionH3 = regexp.MustCompile(`^###\s+(.+)`)

	// Matches any H2 heading (used for WO boundary detection).
	reAnyH2 = regexp.MustCompile(`^##\s+`)

	// Matches acceptance criteria bullets: "- text" or "- [ ] text" or "- [x] text"
	reBullet = regexp.MustCompile(`^-\s+(?:\[.]\s+)?(.+)`)

	// Pure numeric check for ID normalization.
	reNumericSuffix = regexp.MustCompile(`^\d+$`)
)

type parserState int

const (
	stateOutside parserState = iota
	stateInWO
	stateInSummary
	stateInAcceptance
)

// ParseWorkOrders parses a work-orders.md file and returns all work orders found.
func ParseWorkOrders(content string) []WorkOrder {
	lines := strings.Split(content, "\n")
	var results []WorkOrder
	var current *WorkOrder
	state := stateOutside
	var summaryLines []string

	finalize := func() {
		if current == nil {
			return
		}
		if state == stateInSummary && current.Summary == "" {
			current.Summary = strings.TrimSpace(strings.Join(summaryLines, " "))
		}
		results = append(results, *current)
		current = nil
		summaryLines = nil
		state = stateOutside
	}

	inCodeBlock := false
	for _, line := range lines {
		// Skip content inside fenced code blocks.
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock {
			continue
		}

		// Check for WO heading — starts a new WO or ends the current one.
		if m := reWOHeading.FindStringSubmatch(line); m != nil {
			finalize()
			current = &WorkOrder{
				RawID:    m[1],
				Title:    strings.TrimSpace(m[2]),
				Priority: 2, // default medium
			}
			// ✅ in heading means done
			if strings.Contains(line, "✅") {
				current.Status = StatusDone
			}
			state = stateInWO
			continue
		}

		// H2 heading that is not a WO (Phase, Non-Goals, etc.) ends the current WO.
		if reAnyH2.MatchString(line) && current != nil {
			finalize()
			continue
		}

		// Horizontal rule ends the current WO.
		if strings.TrimSpace(line) == "---" && current != nil {
			finalize()
			continue
		}

		// Outside any WO — skip.
		if current == nil {
			continue
		}

		// Inside a WO — scan every line for dependencies and runner overrides.
		if current.DependsOn == "" {
			if m := reDepends.FindStringSubmatch(line); m != nil {
				current.DependsOn = m[1]
			}
		}
		if strings.Contains(strings.ToLower(line), "runner: claude") && current.Runner == "" {
			current.Runner = "claude"
		}

		// Status line.
		if m := reStatus.FindStringSubmatch(line); m != nil {
			switch m[1] {
			case "x":
				current.Status = StatusDone
			case "~":
				current.Status = StatusInProgress
			default:
				current.Status = StatusPlanned
			}
			continue
		}

		// Priority line.
		if m := rePriority.FindStringSubmatch(line); m != nil {
			current.Priority = mapPriority(m[1])
			continue
		}

		// Goal line (inline summary).
		if m := reGoal.FindStringSubmatch(line); m != nil {
			current.Summary = strings.TrimSpace(m[1])
			state = stateInWO
			continue
		}

		// H3 section heading.
		if m := reSectionH3.FindStringSubmatch(line); m != nil {
			// Finalize any in-progress summary collection.
			if state == stateInSummary && current.Summary == "" {
				current.Summary = strings.TrimSpace(strings.Join(summaryLines, " "))
				summaryLines = nil
			}

			section := strings.ToLower(strings.TrimSpace(m[1]))
			switch {
			case section == "summary" && current.Summary == "":
				state = stateInSummary
				summaryLines = nil
			case strings.HasPrefix(section, "acceptance"):
				state = stateInAcceptance
			default:
				state = stateInWO
			}
			continue
		}

		// Collect content based on current state.
		switch state {
		case stateInSummary:
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				// Blank line after collecting summary text → summary is done.
				if len(summaryLines) > 0 {
					current.Summary = strings.TrimSpace(strings.Join(summaryLines, " "))
					summaryLines = nil
					state = stateInWO
				}
			} else {
				summaryLines = append(summaryLines, trimmed)
			}
		case stateInAcceptance:
			if m := reBullet.FindStringSubmatch(line); m != nil {
				current.Acceptance = append(current.Acceptance, strings.TrimSpace(m[1]))
			}
		case stateInWO:
			// dependency and runner scanning handled above for all states
		}
	}

	// Finalize last WO if file doesn't end with --- or ##.
	finalize()

	return results
}

// NormalizeID converts a raw WO suffix to task-file format.
// "01" → "WO01", "C01" → "WO-C01", "CW12" → "WO-CW12"
func NormalizeID(rawSuffix string) string {
	if reNumericSuffix.MatchString(rawSuffix) {
		return "WO" + rawSuffix
	}
	return "WO-" + rawSuffix
}

// TaskID produces the final task ID: "<repoName>-<normalizedWOID>".
func TaskID(repoName, rawSuffix string) string {
	return repoName + "-" + NormalizeID(rawSuffix)
}

// BuildPrompt assembles a prompt from WO fields.
func BuildPrompt(wo WorkOrder) string {
	var b strings.Builder
	b.WriteString(wo.Summary)
	if len(wo.Acceptance) > 0 {
		b.WriteString(" Acceptance: ")
		b.WriteString(strings.Join(wo.Acceptance, "; "))
		b.WriteString(".")
	}
	b.WriteString(fmt.Sprintf(" Read docs/work-orders.md WO-%s for full details.", wo.RawID))
	return b.String()
}

func mapPriority(s string) int {
	switch strings.ToLower(s) {
	case "high":
		return 1
	case "low":
		return 3
	default:
		return 2
	}
}
