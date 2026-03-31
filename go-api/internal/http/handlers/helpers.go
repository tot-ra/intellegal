package handlers

import (
	"fmt"
	"net/url"
	"strings"
)

func normalizeTags(input []string) ([]string, error) {
	if len(input) == 0 {
		return nil, nil
	}

	const maxTags = 20
	const maxTagLength = 50

	tags := make([]string, 0, len(input))
	seen := make(map[string]struct{}, len(input))
	for _, raw := range input {
		tag := strings.TrimSpace(raw)
		if tag == "" {
			continue
		}
		if len(tag) > maxTagLength {
			return nil, fmt.Errorf("tag must be at most %d characters", maxTagLength)
		}
		key := strings.ToLower(tag)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		tags = append(tags, tag)
		if len(tags) > maxTags {
			return nil, fmt.Errorf("at most %d tags are allowed", maxTags)
		}
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
}

func parseTagFilters(q url.Values) ([]string, error) {
	raw := append([]string{}, q["tag"]...)
	if extra := strings.TrimSpace(strings.Join(q["tags"], ",")); extra != "" {
		raw = append(raw, strings.Split(extra, ",")...)
	}
	return normalizeTags(raw)
}

func documentHasAnyTag(doc document, filters []string) bool {
	if len(doc.Tags) == 0 || len(filters) == 0 {
		return false
	}

	docTags := make(map[string]struct{}, len(doc.Tags))
	for _, tag := range doc.Tags {
		docTags[strings.ToLower(strings.TrimSpace(tag))] = struct{}{}
	}
	for _, filter := range filters {
		if _, ok := docTags[strings.ToLower(filter)]; ok {
			return true
		}
	}
	return false
}
