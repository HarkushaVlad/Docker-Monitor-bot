package utils

import (
	"hash/fnv"
	"regexp"
	"strconv"
	"strings"
)

func HashString(s string) string {
	h := fnv.New64a()
	h.Write([]byte(s))
	return strconv.FormatUint(h.Sum64(), 16)
}

func RemoveControlCharactersRegex(s string) string {
	re := regexp.MustCompile(`[\x00-\x08\x0B-\x0C\x0E-\x1F]`)
	return re.ReplaceAllString(s, "")
}

func Min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func EscapeHTML(text string) string {
	replacer := strings.NewReplacer(
		"<", "&lt;",
		">", "&gt;",
		"&", "&amp;",
	)
	return replacer.Replace(text)
}
