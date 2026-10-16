package backbone

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/cases"
)

var reservedPath = regexp.MustCompile(`(?i)^(?:con|prn|aux|nul|com[1-9]|lpt[1-9])(?:\..*)?$`)
var anchorPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
var headingPattern = regexp.MustCompile(`^ {0,3}#{1,6} ([A-Za-z0-9 _-]+?)(?: +#+)?\s*$`)

func relative(value string, anchors bool) (string, string, error) {
	if err := validText(value, 512); err != nil {
		return "", "", err
	}
	pieces := strings.Split(value, "#")
	if len(pieces) > 2 || !anchors && len(pieces) != 1 {
		return "", "", fmt.Errorf("unexpected Backbone path anchor")
	}
	anchor := ""
	if len(pieces) == 2 {
		anchor = pieces[1]
		if !anchorPattern.MatchString(anchor) {
			return "", "", fmt.Errorf("invalid Backbone heading anchor")
		}
	}
	for _, part := range strings.Split(pieces[0], "/") {
		if oneOf(part, "", ".", "..") || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || strings.ContainsAny(part, `\:<>"|?*`) || reservedPath.MatchString(part) {
			return "", "", fmt.Errorf("unsafe or nonportable Backbone path")
		}
	}
	return pieces[0], anchor, nil
}

func safeRoot(workspace string) (string, error) {
	if workspace == "" || !filepath.IsAbs(workspace) {
		return "", fmt.Errorf("backbone workspace must be absolute")
	}
	root := filepath.Clean(workspace)
	current := root
	for {
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || linked(info) {
			return "", fmt.Errorf("backbone workspace has missing, linked or non-directory ancestor")
		}
		parent := filepath.Dir(current)
		if current == parent {
			break
		}
		current = parent
	}
	return root, nil
}

func locate(root, name string) (string, error) {
	path, _, err := relative(name, false)
	if err != nil {
		return "", err
	}
	current := root
	parts := strings.Split(path, "/")
	for i, part := range parts {
		entries, err := boundedEntries(current)
		if err != nil {
			return "", fmt.Errorf("cannot inspect Backbone path %s", name)
		}
		matches := 0
		for _, entry := range entries {
			if cases.Fold().String(entry.Name()) == cases.Fold().String(part) {
				matches++
				if entry.Name() != part {
					return "", fmt.Errorf("backbone path has case mismatch or ambiguity: %s", name)
				}
			}
		}
		if matches != 1 {
			return "", fmt.Errorf("backbone path absent or ambiguous: %s", name)
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || linked(info) || i < len(parts)-1 && !info.IsDir() {
			return "", fmt.Errorf("backbone path is linked or has a non-directory ancestor: %s", name)
		}
	}
	return current, nil
}

func readDocument(root, name string) ([]byte, error) {
	path, err := locate(root, name)
	if err != nil {
		return nil, err
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || linked(before) || before.Size() > MaxDocumentBytes {
		return nil, fmt.Errorf("backbone document is nonregular or oversized: %s", name)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read Backbone document: %s", name)
	}
	defer func() { _ = file.Close() }()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) || !opened.Mode().IsRegular() {
		return nil, fmt.Errorf("backbone document changed during open: %s", name)
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxDocumentBytes+1))
	if err != nil || len(data) > MaxDocumentBytes {
		return nil, fmt.Errorf("cannot read bounded Backbone document: %s", name)
	}
	after, err := os.Lstat(path)
	if err != nil || linked(after) || !os.SameFile(before, after) {
		return nil, fmt.Errorf("backbone document changed during read: %s", name)
	}
	if _, err := locate(root, name); err != nil {
		return nil, err
	}
	if !utf8.Valid(data) || strings.TrimSpace(string(data)) == "" || strings.ContainsRune(string(data), 0) {
		return nil, fmt.Errorf("backbone document is empty or invalid UTF-8: %s", name)
	}
	return data, nil
}

func boundedEntries(path string) ([]os.DirEntry, error) {
	directory, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = directory.Close() }()
	entries, err := directory.ReadDir(4097)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > 4096 {
		return nil, fmt.Errorf("backbone path directory exceeds entry limit")
	}
	return entries, nil
}

// hasHeading mirrors the deliberately narrow producer heading IDs. Fence closes
// must use the opener character, at least its length, and no trailing content.
func hasHeading(data []byte, anchor string) bool {
	counts := map[string]int{}
	fence := byte(0)
	fenceLength := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		indent := len(line) - len(strings.TrimLeft(line, " "))
		trimmed := strings.TrimLeft(line, " ")
		if indent <= 3 && len(trimmed) >= 3 && (trimmed[0] == '`' || trimmed[0] == '~') {
			char := trimmed[0]
			n := 0
			for n < len(trimmed) && trimmed[n] == char {
				n++
			}
			if fence != 0 {
				if char == fence && n >= fenceLength && strings.TrimSpace(trimmed[n:]) == "" {
					fence = 0
					fenceLength = 0
				}
				continue
			}
			if n >= 3 && (char != '`' || !strings.ContainsRune(trimmed[n:], '`')) {
				fence = char
				fenceLength = n
				continue
			}
		}
		if fence != 0 {
			continue
		}
		match := headingPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		slug := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(match[1])), " ", "-")
		count := counts[slug]
		counts[slug]++
		if count > 0 {
			slug = fmt.Sprintf("%s-%d", slug, count)
		}
		if slug == anchor {
			return true
		}
	}
	return false
}
