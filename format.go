package tgbot

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

var (
	escapeChars           = regexp.MustCompile(`([_\*\[\]\(\)~>#\+\-=|{}\.!])`)
	escapeCharsFormatting = regexp.MustCompile(`([\(\)~>#\+\-=|{}\.!])`)
	smallCodeBlocks       = regexp.MustCompile("`[^`]*`")
	urlMention            = regexp.MustCompile(`\[[^\]]*\]\([^\)]*\)`) // Regex to identify URL mentions

	specialCharPairs = []rune{'*', '_', '~', '|', '[', ']', '(', ')', '`'}
)

// EscapeMarkdown escapes markdown characters for Telegram.
func EscapeMarkdown(text string, allowFormatting ...bool) string {
	var buf bytes.Buffer

	inCodeBlock := false
	lines := strings.Split(text, "\n")

	escapeSet := escapeChars
	if len(allowFormatting) > 0 && allowFormatting[0] {
		// return text

		escapeSet = escapeCharsFormatting
	}

	for _, line := range lines {
		if strings.Contains(line, "```") {
			inCodeBlock = !inCodeBlock
		} else if !inCodeBlock {
			matches := smallCodeBlocks.FindAllString(line, -1)

			orig := map[string]string{}
			for _, match := range matches {
				placeholder := md5Hash(uuid.NewString())
				line = strings.Replace(line, match, placeholder, 1)
				orig[placeholder] = match
			}

			// Replace URL mentions temporarily
			urlMatches := urlMention.FindAllString(line, -1)
			urlPlaceholders := make(map[string]string)
			for _, match := range urlMatches {
				placeholder := md5Hash(uuid.NewString())
				line = strings.Replace(line, match, placeholder, 1)
				urlPlaceholders[placeholder] = match
			}

			// Escape all special characters outside of the small code block and URL mentions
			line = escapeSet.ReplaceAllString(line, `\$1`)
			line = escapeSingularSpecialChars(line, specialCharPairs)

			// Restore the URL mentions and code blocks
			for ori, match := range orig {
				line = strings.Replace(line, ori, match, 1)
			}
			for ori, match := range urlPlaceholders {
				line = strings.Replace(line, ori, match, 1)
			}
		}

		buf.WriteString(line)
		buf.WriteString("\n")
	}

	return strings.TrimSpace(buf.String())
}

func md5Hash(str string) string {
	hash := md5.Sum([]byte(str))
	return hex.EncodeToString(hash[:])
}

func escapeSingularSpecialChars(input string, specialChars []rune) string {
	lines := strings.Split(input, "\n")
	escapedLines := make([]string, len(lines))

	for i, line := range lines {
		escapedLine := line
		for _, char := range specialChars {
			// Escape the special character for use in the regular expression
			escChar := regexp.QuoteMeta(string(char))

			// Regular expression to find the special character not preceded by '\'
			re := regexp.MustCompile(`(?m)([^\\]|^)` + escChar)

			// Find all matches for the current character
			matches := re.FindAllStringIndex(escapedLine, -1)

			// Determine action based on count of matches
			count := len(matches)
			if count%2 != 0 {
				// Escape the last unescaped occurrence of the character
				lastMatch := matches[count-1]
				if lastMatch[0] == 0 {
					// If the match is at the start of the line, add a backslash at the start
					escapedLine = "\\" + escapedLine
				} else {
					escapedLine = escapedLine[:lastMatch[0]+1] + "\\" + escapedLine[lastMatch[0]+1:]
				}
			}
			// No change for even counts
		}
		escapedLines[i] = escapedLine
	}

	return strings.Join(escapedLines, "\n")
}
