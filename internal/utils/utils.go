package utils

import (
	"regexp"
	"strings"

	"github.com/docker/docker/api/types"
)

var controlCharRegex = regexp.MustCompile(`[\x00-\x08\x0B-\x0C\x0E-\x1F]`)

func RemoveControlChars(s string) string {
	return controlCharRegex.ReplaceAllString(s, "")
}

func StripDockerLogHeader(line []byte) []byte {
	if len(line) >= 8 && (line[0] == 0 || line[0] == 1 || line[0] == 2) &&
		line[1] == 0 && line[2] == 0 && line[3] == 0 {
		return line[8:]
	}
	return line
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
