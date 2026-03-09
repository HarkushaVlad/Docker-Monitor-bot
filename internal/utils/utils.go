package utils

import (
	"hash/fnv"
	"regexp"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types"
)

var controlCharRegex = regexp.MustCompile(`[\x00-\x08\x0B-\x0C\x0E-\x1F]`)

func HashString(s string) string {
	h := fnv.New64a()
	h.Write([]byte(s))
	return strconv.FormatUint(h.Sum64(), 16)
}

func RemoveControlChars(s string) string {
	return controlCharRegex.ReplaceAllString(s, "")
}

func EscapeHTML(text string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(text)
}

func IsUserContainer(c types.Container) bool {
	matched, _ := regexp.MatchString(`^[a-f0-9]{12}$`, c.Image)
	if matched || strings.HasPrefix(c.Image, "sha256:") {
		name := strings.Trim(c.Names[0], "/")
		parts := strings.Split(name, "_")
		if len(parts) == 2 {
			return false
		}
	}
	return true
}

func ContainerName(c types.Container) string {
	return strings.TrimPrefix(c.Names[0], "/")
}
