package executor

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var ErrReadOnlyViolation = errors.New("read-only policy violation: working tree mutated")
var ErrWriteBoundaryViolation = errors.New("write boundary violation: modified path outside allowed prefix")

// VerifyReadOnlyUnchanged enforces TC-05: Golden Set B must leave the git working tree untouched.
func VerifyReadOnlyUnchanged(beforePorcelain, afterPorcelain string) error {
	if strings.TrimSpace(beforePorcelain) != strings.TrimSpace(afterPorcelain) {
		return fmt.Errorf("%w\nbefore:\n%s\nafter:\n%s", ErrReadOnlyViolation, beforePorcelain, afterPorcelain)
	}
	return nil
}

// VerifyAllowedWritePaths enforces TC-06: Stage 4 skill curator may only touch allowedPrefixes (e.g. "skills/").
func VerifyAllowedWritePaths(beforePaths, afterPaths, allowedPrefixes []string) error {
	beforeSet := make(map[string]struct{}, len(beforePaths))
	for _, p := range beforePaths {
		beforeSet[p] = struct{}{}
	}

	var violations []string
	for _, p := range afterPaths {
		if _, existed := beforeSet[p]; existed {
			continue
		}
		allowed := false
		for _, prefix := range allowedPrefixes {
			if strings.HasPrefix(p, prefix) {
				allowed = true
				break
			}
		}
		if !allowed {
			violations = append(violations, p)
		}
	}
	if len(violations) > 0 {
		return fmt.Errorf("%w: %s (allowed prefixes: %v)", ErrWriteBoundaryViolation, strings.Join(violations, ", "), allowedPrefixes)
	}
	return nil
}

// ExtractJSONObject extracts the first valid top-level JSON object from raw text or fenced markdown.
func ExtractJSONObject(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", errors.New("empty response")
	}

	// Fast path: already valid JSON object
	if strings.HasPrefix(s, "{") && json.Valid([]byte(s)) {
		return s, nil
	}

	// Strip ```json ... ``` fences if present
	if idx := strings.Index(s, "```json"); idx >= 0 {
		rest := s[idx+7:]
		if end := strings.Index(rest, "```"); end >= 0 {
			candidate := strings.TrimSpace(rest[:end])
			if json.Valid([]byte(candidate)) {
				return candidate, nil
			}
		}
	}

	// Scan for balanced JSON object
	start := strings.IndexByte(s, '{')
	for start >= 0 {
		depth := 0
		inString := false
		escaped := false
		for i := start; i < len(s); i++ {
			ch := s[i]
			if inString {
				if escaped {
					escaped = false
					continue
				}
				if ch == '\\' {
					escaped = true
					continue
				}
				if ch == '"' {
					inString = false
				}
				continue
			}
			switch ch {
			case '"':
				inString = true
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					candidate := s[start : i+1]
					if json.Valid([]byte(candidate)) {
						return candidate, nil
					}
					break
				}
			}
		}
		next := strings.IndexByte(s[start+1:], '{')
		if next < 0 {
			break
		}
		start = start + 1 + next
	}

	return "", errors.New("no valid JSON object found in response")
}
