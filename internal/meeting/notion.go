package meeting

import (
	"fmt"
	"strings"
)

// NotionBlockURL returns a stable link to a block within its parent page.
func NotionBlockURL(parentPageID, blockID string) (string, error) {
	parent, err := normalizeNotionID(parentPageID)
	if err != nil {
		return "", fmt.Errorf("invalid Notion parent page id: %w", err)
	}
	block, err := normalizeNotionID(blockID)
	if err != nil {
		return "", fmt.Errorf("invalid Notion meeting-note block id: %w", err)
	}
	return "https://app.notion.com/p/" + parent + "#" + block, nil
}

func normalizeNotionID(value string) (string, error) {
	value = strings.ReplaceAll(strings.TrimSpace(value), "-", "")
	if len(value) != 32 {
		return "", fmt.Errorf("expected 32 hexadecimal characters")
	}
	for _, character := range value {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
			return "", fmt.Errorf("expected hexadecimal characters")
		}
	}
	return strings.ToLower(value), nil
}
