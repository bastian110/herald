package telegram

import (
	"html"
	"regexp"
	"strings"
)

var (
	tableRe = regexp.MustCompile(`(?is)<table\b[^>]*>.*?</table>`)
	rowRe   = regexp.MustCompile(`(?is)<tr\b[^>]*>(.*?)</tr>`)
	cellRe  = regexp.MustCompile(`(?is)<t[dh]\b[^>]*>(.*?)</t[dh]>`)
	tagRe   = regexp.MustCompile(`(?is)<[^>]+>`)
	spaceRe = regexp.MustCompile(`\s+`)
)

func prepareTelegramText(text string, parseMode string) string {
	if parseMode != "HTML" {
		return text
	}
	return sanitizeTelegramHTML(convertTablesToPre(text))
}

func prepareTelegramRichHTML(text string) string {
	return convertPlainNewlinesToRichBreaks(text)
}

func convertPlainNewlinesToRichBreaks(text string) string {
	var out strings.Builder
	rawDepth := 0
	for len(text) > 0 {
		idx := strings.IndexByte(text, '<')
		if idx == -1 {
			writeRichTextSegment(&out, text, rawDepth)
			break
		}
		writeRichTextSegment(&out, text[:idx], rawDepth)
		end := strings.IndexByte(text[idx:], '>')
		if end == -1 {
			writeRichTextSegment(&out, text[idx:], rawDepth)
			break
		}
		rawTag := text[idx : idx+end+1]
		out.WriteString(rawTag)
		updateRichRawDepth(rawTag, &rawDepth)
		text = text[idx+end+1:]
	}
	return out.String()
}

func writeRichTextSegment(out *strings.Builder, text string, rawDepth int) {
	if rawDepth > 0 {
		out.WriteString(text)
		return
	}
	out.WriteString(strings.ReplaceAll(text, "\n", "<br>"))
}

func updateRichRawDepth(rawTag string, rawDepth *int) {
	content := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(rawTag, "<"), ">"))
	if content == "" || strings.HasPrefix(content, "!") {
		return
	}
	if strings.HasPrefix(content, "/") {
		name := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(content, "/")))
		if preservesRawRichNewlines(name) && *rawDepth > 0 {
			*rawDepth -= 1
		}
		return
	}
	name, _ := splitTag(content)
	name = strings.ToLower(strings.TrimSuffix(name, "/"))
	if !preservesRawRichNewlines(name) || strings.HasSuffix(content, "/") {
		return
	}
	*rawDepth++
}

func preservesRawRichNewlines(name string) bool {
	switch name {
	case "pre", "code", "table", "ul", "ol":
		return true
	default:
		return false
	}
}

func convertTablesToPre(text string) string {
	return tableRe.ReplaceAllStringFunc(text, func(table string) string {
		rows := parseTableRows(table)
		if len(rows) == 0 {
			return html.EscapeString(table)
		}
		return "<pre>" + html.EscapeString(formatPlainTable(rows)) + "</pre>"
	})
}

func parseTableRows(table string) [][]string {
	matches := rowRe.FindAllStringSubmatch(table, -1)
	rows := make([][]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		cellMatches := cellRe.FindAllStringSubmatch(match[1], -1)
		row := make([]string, 0, len(cellMatches))
		for _, cellMatch := range cellMatches {
			if len(cellMatch) < 2 {
				continue
			}
			row = append(row, htmlCellText(cellMatch[1]))
		}
		if len(row) > 0 {
			rows = append(rows, row)
		}
	}
	return rows
}

func htmlCellText(value string) string {
	withoutTags := tagRe.ReplaceAllString(value, "")
	return strings.TrimSpace(spaceRe.ReplaceAllString(html.UnescapeString(withoutTags), " "))
}

func formatPlainTable(rows [][]string) string {
	widths := tableWidths(rows)
	lines := make([]string, 0, len(rows)+1)
	for i, row := range rows {
		lines = append(lines, formatPlainTableRow(row, widths))
		if i == 0 && len(rows) > 1 {
			lines = append(lines, formatPlainTableSeparator(widths))
		}
	}
	return strings.Join(lines, "\n")
}

func tableWidths(rows [][]string) []int {
	maxCols := 0
	for _, row := range rows {
		if len(row) > maxCols {
			maxCols = len(row)
		}
	}
	widths := make([]int, maxCols)
	for _, row := range rows {
		for i, cell := range row {
			if width := len([]rune(cell)); width > widths[i] {
				widths[i] = width
			}
		}
	}
	return widths
}

func formatPlainTableRow(row []string, widths []int) string {
	cells := make([]string, len(widths))
	for i := range widths {
		cell := ""
		if i < len(row) {
			cell = row[i]
		}
		cells[i] = padRight(cell, widths[i])
	}
	return strings.TrimRight(strings.Join(cells, " | "), " ")
}

func formatPlainTableSeparator(widths []int) string {
	parts := make([]string, len(widths))
	for i, width := range widths {
		if width < 3 {
			width = 3
		}
		parts[i] = strings.Repeat("-", width)
	}
	return strings.Join(parts, "-+-")
}

func padRight(value string, width int) string {
	padding := width - len([]rune(value))
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}

func sanitizeTelegramHTML(text string) string {
	var out strings.Builder
	for len(text) > 0 {
		idx := strings.IndexByte(text, '<')
		if idx == -1 {
			out.WriteString(escapeHTMLText(text))
			break
		}
		out.WriteString(escapeHTMLText(text[:idx]))
		end := strings.IndexByte(text[idx:], '>')
		if end == -1 {
			out.WriteString("&lt;")
			text = text[idx+1:]
			continue
		}
		rawTag := text[idx : idx+end+1]
		if tag, ok := sanitizeTelegramHTMLTag(rawTag); ok {
			out.WriteString(tag)
		} else {
			out.WriteString(html.EscapeString(rawTag))
		}
		text = text[idx+end+1:]
	}
	return out.String()
}

func escapeHTMLText(text string) string {
	var out strings.Builder
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '&':
			if entityEnd := htmlEntityEnd(text[i:]); entityEnd > 0 {
				out.WriteString(text[i : i+entityEnd])
				i += entityEnd - 1
			} else {
				out.WriteString("&amp;")
			}
		case '<':
			out.WriteString("&lt;")
		case '>':
			out.WriteString("&gt;")
		default:
			out.WriteByte(text[i])
		}
	}
	return out.String()
}

func htmlEntityEnd(text string) int {
	semicolon := strings.IndexByte(text, ';')
	if semicolon < 2 || semicolon > 16 {
		return 0
	}
	entity := text[1:semicolon]
	if strings.HasPrefix(entity, "#") {
		return semicolon + 1
	}
	switch entity {
	case "lt", "gt", "amp", "quot":
		return semicolon + 1
	default:
		return 0
	}
}

func sanitizeTelegramHTMLTag(raw string) (string, bool) {
	content := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "<"), ">"))
	if content == "" || strings.HasPrefix(content, "!") {
		return "", false
	}
	if strings.HasPrefix(content, "/") {
		name := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(content, "/")))
		if isSimpleHTMLTag(name) || name == "span" || name == "a" || name == "tg-emoji" || name == "blockquote" {
			return "</" + name + ">", true
		}
		return "", false
	}

	name, attrs := splitTag(content)
	name = strings.ToLower(name)
	switch name {
	case "b", "strong", "i", "em", "u", "ins", "s", "strike", "del", "tg-spoiler", "pre":
		if strings.TrimSpace(attrs) == "" {
			return "<" + name + ">", true
		}
	case "code":
		if strings.TrimSpace(attrs) == "" {
			return "<code>", true
		}
		if classValue, ok := attrValue(attrs, "class"); ok && isTelegramCodeClass(classValue) {
			return `<code class="` + html.EscapeString(classValue) + `">`, true
		}
	case "span":
		if classValue, ok := attrValue(attrs, "class"); ok && classValue == "tg-spoiler" {
			return `<span class="tg-spoiler">`, true
		}
	case "a":
		if href, ok := attrValue(attrs, "href"); ok && isSafeTelegramURL(href) {
			return `<a href="` + html.EscapeString(href) + `">`, true
		}
	case "tg-emoji":
		if emojiID, ok := attrValue(attrs, "emoji-id"); ok && digitsOnly(emojiID) {
			return `<tg-emoji emoji-id="` + emojiID + `">`, true
		}
	case "blockquote":
		attrs = strings.TrimSpace(attrs)
		if attrs == "" {
			return "<blockquote>", true
		}
		if attrs == "expandable" {
			return "<blockquote expandable>", true
		}
	}
	return "", false
}

func splitTag(content string) (string, string) {
	for i, r := range content {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return content[:i], content[i+1:]
		}
	}
	return content, ""
}

func isSimpleHTMLTag(name string) bool {
	switch name {
	case "b", "strong", "i", "em", "u", "ins", "s", "strike", "del", "tg-spoiler", "code", "pre":
		return true
	default:
		return false
	}
}

func attrValue(attrs string, name string) (string, bool) {
	pattern := regexp.MustCompile(`(?i)(?:^|\s)` + regexp.QuoteMeta(name) + `\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)
	match := pattern.FindStringSubmatch(attrs)
	if len(match) < 2 {
		return "", false
	}
	value := match[1]
	if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
		value = value[1 : len(value)-1]
	}
	return html.UnescapeString(value), true
}

func isTelegramCodeClass(value string) bool {
	if !strings.HasPrefix(value, "language-") {
		return false
	}
	for _, r := range strings.TrimPrefix(value, "language-") {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

func isSafeTelegramURL(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "tg://") || strings.HasPrefix(lower, "mailto:")
}

func digitsOnly(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
