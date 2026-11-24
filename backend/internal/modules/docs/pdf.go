package docs

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

const (
	pdfExtractTimeout  = 30 * time.Second
	pdfMaxStdoutBytes  = 32 << 20
	pdfMaxStreamBytes  = 16 << 20
	pdfMaxTotalDecoded = 64 << 20
)

// pdfTextLookPath позволяет тестам подменять поиск внешнего бинарника pdftotext.
var pdfTextLookPath = exec.LookPath

// ConvertPdfToMarkdown извлекает текстовый слой из PDF и форматирует его в Markdown.
// При наличии pdftotext (poppler-utils) используется он для точной работы с шрифтами,
// кодировками и вёрсткой; иначе применяется встроенный Go-парсер текстовых потоков.
func ConvertPdfToMarkdown(ctx context.Context, data []byte) (string, error) {
	if !hasPDFHeader(data) {
		return "", platform.NewValidation("Некорректный .pdf файл", nil)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var raw string
	var err error
	if bin, lookErr := pdfTextLookPath("pdftotext"); lookErr == nil && bin != "" {
		raw, err = extractPdfWithPoppler(ctx, bin, data)
	} else {
		raw, err = extractPdfTextNative(data)
	}
	if err != nil {
		return "", err
	}

	md := formatPdfTextAsMarkdown(raw)
	if md == "" {
		return "", platform.NewValidation(
			"Не удалось извлечь текст из PDF: возможно, файл является сканом без текстового слоя (требуется предварительное распознавание OCR)",
			nil,
		)
	}
	return md, nil
}

func hasPDFHeader(data []byte) bool {
	if len(data) < 5 {
		return false
	}
	limit := len(data)
	if limit > 1024 {
		limit = 1024
	}
	return bytes.Contains(data[:limit], []byte("%PDF-"))
}

func extractPdfWithPoppler(ctx context.Context, bin string, data []byte) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, pdfExtractTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, bin, "-layout", "-enc", "UTF-8", "-", "-")
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitedBuffer{buf: &stdout, limit: pdfMaxStdoutBytes}
	cmd.Stderr = &limitedBuffer{buf: &stderr, limit: 64 << 10}

	if err := cmd.Run(); err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return "", platform.NewValidation("Превышено время извлечения текста из PDF", nil)
		}
		errMsg := strings.ToLower(stderr.String())
		if strings.Contains(errMsg, "password") || strings.Contains(errMsg, "encrypted") {
			return "", platform.NewValidation("PDF-файл защищён паролем и не может быть импортирован", nil)
		}
		return "", platform.NewValidation("Не удалось прочитать .pdf файл: файл повреждён или имеет неподдерживаемый формат", nil)
	}
	return stdout.String(), nil
}

type limitedBuffer struct {
	buf   *bytes.Buffer
	limit int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	remain := l.limit - l.buf.Len()
	if remain <= 0 {
		return len(p), nil
	}
	if len(p) > remain {
		p = p[:remain]
	}
	_, _ = l.buf.Write(p)
	return len(p), nil
}

func cleanPdfPage(p string) string {
	lines := strings.Split(p, "\n")
	var clean []string
	blankRun := 0
	for _, line := range lines {
		trimmedRight := strings.TrimRight(line, " \t")
		if strings.TrimSpace(trimmedRight) == "" {
			blankRun++
			if blankRun <= 1 && len(clean) > 0 {
				clean = append(clean, "")
			}
			continue
		}
		blankRun = 0
		clean = append(clean, trimmedRight)
	}
	for len(clean) > 0 && clean[len(clean)-1] == "" {
		clean = clean[:len(clean)-1]
	}
	return strings.Join(clean, "\n")
}

var rePdfDotLeader = regexp.MustCompile(`\.{6,}`)

// sanitizePdfPageForMarkdown убирает ведущие пробелы (чтобы отступы pdftotext -layout
// не превращались в Markdown-блоки кода <pre> с чёрным фоном) и сжимает длинные цепочки точек оглавления.
func sanitizePdfPageForMarkdown(p string) string {
	lines := strings.Split(p, "\n")
	out := make([]string, 0, len(lines))
	blankRun := 0
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" {
			blankRun++
			if blankRun <= 1 && len(out) > 0 {
				out = append(out, "")
			}
			continue
		}
		blankRun = 0
		t = rePdfDotLeader.ReplaceAllString(t, " … ")
		out = append(out, t)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

func formatPdfTextAsMarkdown(raw string) string {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	rawPages := strings.Split(normalized, "\f")
	pages := make([]string, 0, len(rawPages))
	for _, p := range rawPages {
		if cleaned := sanitizePdfPageForMarkdown(cleanPdfPage(p)); cleaned != "" {
			pages = append(pages, cleaned)
		}
	}
	if len(pages) == 0 {
		return ""
	}
	return strings.TrimSpace(strings.Join(pages, "\n\n---\n\n"))
}

// --- Встроенный Go-парсер текстовых потоков PDF (резервный вариант без poppler-utils) ---

var (
	rePdfStream = regexp.MustCompile(`(?s)<<(.*?)>>\s*stream\r?\n(.*?)\r?\n?endstream`)
	reBfChar    = regexp.MustCompile(`(?s)beginbfchar(.*?)endbfchar`)
	reBfRange   = regexp.MustCompile(`(?s)beginbfrange(.*?)endbfrange`)
	reHexToken  = regexp.MustCompile(`<([0-9A-Fa-f]+)>`)
)

func extractPdfTextNative(data []byte) (string, error) {
	if bytes.Contains(data, []byte("/Encrypt")) {
		return "", platform.NewValidation("PDF-файл защищён паролем или шифрованием и не может быть импортирован без pdftotext", nil)
	}

	matches := rePdfStream.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return "", nil
	}

	type decodedStream struct {
		dict string
		body []byte
	}
	decoded := make([]decodedStream, 0, len(matches))
	totalDecoded := 0

	for _, m := range matches {
		if totalDecoded >= pdfMaxTotalDecoded {
			break
		}
		dict := string(m[1])
		if isNonTextPdfStream(dict) {
			continue
		}
		body := m[2]
		if strings.Contains(dict, "/FlateDecode") {
			plain, err := inflateZlib(body, pdfMaxStreamBytes)
			if err != nil {
				continue
			}
			body = plain
		}
		totalDecoded += len(body)
		decoded = append(decoded, decodedStream{dict: dict, body: body})
	}

	// Собираем таблицы ToUnicode CMap для корректного декодирования кириллицы в CID-шрифтах.
	cmap := map[string]string{}
	for _, s := range decoded {
		if bytes.Contains(s.body, []byte("beginbfchar")) || bytes.Contains(s.body, []byte("beginbfrange")) {
			parseToUnicodeCMap(string(s.body), cmap)
		}
	}

	var pages []string
	for _, s := range decoded {
		if bytes.Contains(s.body, []byte("begincmap")) || bytes.Contains(s.body, []byte("beginbfchar")) {
			continue
		}
		if !bytes.Contains(s.body, []byte("BT")) {
			continue
		}
		txt := extractTextFromContentStream(s.body, cmap)
		if strings.TrimSpace(txt) != "" {
			pages = append(pages, txt)
		}
	}
	return strings.Join(pages, "\f"), nil
}

func isNonTextPdfStream(dict string) bool {
	return strings.Contains(dict, "/Subtype /Image") ||
		strings.Contains(dict, "/Subtype/Image") ||
		strings.Contains(dict, "/Type /Metadata") ||
		strings.Contains(dict, "/Type/Metadata") ||
		strings.Contains(dict, "/FontFile")
}

func inflateZlib(raw []byte, limit int64) ([]byte, error) {
	zr, err := zlib.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(io.LimitReader(zr, limit))
}

func parseToUnicodeCMap(src string, dst map[string]string) {
	for _, block := range reBfChar.FindAllStringSubmatch(src, -1) {
		tokens := reHexToken.FindAllStringSubmatch(block[1], -1)
		for i := 0; i+1 < len(tokens); i += 2 {
			code := strings.ToUpper(tokens[i][1])
			if s := decodeUTF16BEHex(tokens[i+1][1]); s != "" {
				dst[code] = s
			}
		}
	}
	for _, block := range reBfRange.FindAllStringSubmatch(src, -1) {
		lines := strings.Split(block[1], "\n")
		for _, line := range lines {
			tokens := reHexToken.FindAllStringSubmatch(line, -1)
			if len(tokens) < 3 {
				continue
			}
			startHex := tokens[0][1]
			endHex := tokens[1][1]
			startVal, err1 := strconv.ParseUint(startHex, 16, 32)
			endVal, err2 := strconv.ParseUint(endHex, 16, 32)
			if err1 != nil || err2 != nil || endVal < startVal || endVal-startVal > 4096 {
				continue
			}
			width := len(startHex)
			if strings.Contains(line, "[") {
				for idx := uint64(0); idx <= endVal-startVal && int(2+idx) < len(tokens); idx++ {
					key := formatHexKey(startVal+idx, width)
					if s := decodeUTF16BEHex(tokens[2+idx][1]); s != "" {
						dst[key] = s
					}
				}
				continue
			}
			baseUnicode, err := strconv.ParseUint(tokens[2][1], 16, 32)
			if err != nil {
				continue
			}
			for idx := uint64(0); idx <= endVal-startVal; idx++ {
				key := formatHexKey(startVal+idx, width)
				r := rune(baseUnicode + idx)
				if utf8.ValidRune(r) {
					dst[key] = string(r)
				}
			}
		}
	}
}

func formatHexKey(v uint64, width int) string {
	s := strings.ToUpper(strconv.FormatUint(v, 16))
	if len(s) < width {
		return strings.Repeat("0", width-len(s)) + s
	}
	return s
}

func decodeUTF16BEHex(hexStr string) string {
	if len(hexStr)%2 == 1 {
		hexStr += "0"
	}
	raw, err := hex.DecodeString(hexStr)
	if err != nil || len(raw) == 0 {
		return ""
	}
	if len(raw) == 1 {
		return string(rune(raw[0]))
	}
	if len(raw)%2 == 0 {
		u16 := make([]uint16, len(raw)/2)
		for i := range u16 {
			u16[i] = uint16(raw[2*i])<<8 | uint16(raw[2*i+1])
		}
		return string(utf16.Decode(u16))
	}
	return string(raw)
}

func extractTextFromContentStream(stream []byte, cmap map[string]string) string {
	var out strings.Builder
	inText := false
	n := len(stream)
	i := 0
	var lastOperands []string

	for i < n {
		// Пропускаем пробелы и комментарии PDF (% ... \n).
		c := stream[i]
		if isPdfSpace(c) {
			i++
			continue
		}
		if c == '%' {
			for i < n && stream[i] != '\n' && stream[i] != '\r' {
				i++
			}
			continue
		}

		// Литеральная строка (...)
		if c == '(' {
			str, next := readPdfLiteralString(stream, i)
			i = next
			if inText {
				lastOperands = append(lastOperands, "S:"+str)
			}
			continue
		}

		// Шестнадцатеричная строка <...> (не словарь <<...>>)
		if c == '<' && i+1 < n && stream[i+1] != '<' {
			hexStr, next := readPdfHexString(stream, i)
			i = next
			if inText {
				lastOperands = append(lastOperands, "H:"+hexStr)
			}
			continue
		}

		// Массив [...] (используется оператором TJ)
		if c == '[' {
			arrText, next := readPdfTextArray(stream, i, cmap)
			i = next
			if inText {
				lastOperands = append(lastOperands, "A:"+arrText)
			}
			continue
		}

		// Словарь <<...>> пропускаем как обычный токен.
		if c == '<' && i+1 < n && stream[i+1] == '<' {
			i += 2
			continue
		}
		if c == '>' && i+1 < n && stream[i+1] == '>' {
			i += 2
			continue
		}

		// Читаем идентификатор / число / оператор.
		start := i
		for i < n && !isPdfSpace(stream[i]) && !isPdfDelimiter(stream[i]) {
			i++
		}
		if i == start {
			i++
			continue
		}
		tok := string(stream[start:i])

		switch tok {
		case "BT":
			inText = true
			lastOperands = nil
		case "ET":
			if inText && out.Len() > 0 && !strings.HasSuffix(out.String(), "\n") {
				out.WriteByte('\n')
			}
			inText = false
			lastOperands = nil
		case "Tj":
			if inText && len(lastOperands) > 0 {
				out.WriteString(decodePdfOperand(lastOperands[len(lastOperands)-1], cmap))
			}
			lastOperands = nil
		case "TJ":
			if inText && len(lastOperands) > 0 {
				op := lastOperands[len(lastOperands)-1]
				if strings.HasPrefix(op, "A:") {
					out.WriteString(op[2:])
				} else {
					out.WriteString(decodePdfOperand(op, cmap))
				}
			}
			lastOperands = nil
		case "'", `"`:
			if inText && len(lastOperands) > 0 {
				if out.Len() > 0 && !strings.HasSuffix(out.String(), "\n") {
					out.WriteByte('\n')
				}
				out.WriteString(decodePdfOperand(lastOperands[len(lastOperands)-1], cmap))
			}
			lastOperands = nil
		case "T*":
			if inText && out.Len() > 0 && !strings.HasSuffix(out.String(), "\n") {
				out.WriteByte('\n')
			}
			lastOperands = nil
		case "Td", "TD":
			if inText && len(lastOperands) >= 2 {
				dy, _ := strconv.ParseFloat(strings.TrimPrefix(lastOperands[len(lastOperands)-1], "N:"), 64)
				if dy != 0 && out.Len() > 0 && !strings.HasSuffix(out.String(), "\n") {
					out.WriteByte('\n')
				} else if dy == 0 && out.Len() > 0 && !strings.HasSuffix(out.String(), " ") && !strings.HasSuffix(out.String(), "\n") {
					out.WriteByte(' ')
				}
			}
			lastOperands = nil
		case "Tm":
			if inText && out.Len() > 0 && !strings.HasSuffix(out.String(), "\n") {
				out.WriteByte('\n')
			}
			lastOperands = nil
		default:
			if inText {
				lastOperands = append(lastOperands, "N:"+tok)
				if len(lastOperands) > 8 {
					lastOperands = lastOperands[len(lastOperands)-8:]
				}
			}
		}
	}
	return out.String()
}

func isPdfSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n' || b == '\f' || b == 0
}

func isPdfDelimiter(b byte) bool {
	return b == '(' || b == ')' || b == '<' || b == '>' || b == '[' || b == ']' || b == '{' || b == '}' || b == '/' || b == '%'
}

func readPdfLiteralString(src []byte, pos int) (string, int) {
	pos++ // пропускаем '('
	depth := 1
	var buf []byte
	n := len(src)
	for pos < n && depth > 0 {
		b := src[pos]
		if b == '\\' && pos+1 < n {
			pos++
			esc := src[pos]
			switch esc {
			case 'n':
				buf = append(buf, '\n')
			case 'r':
				buf = append(buf, '\r')
			case 't':
				buf = append(buf, '\t')
			case 'b':
				buf = append(buf, '\b')
			case 'f':
				buf = append(buf, '\f')
			case '(', ')', '\\':
				buf = append(buf, esc)
			case '\r':
				if pos+1 < n && src[pos+1] == '\n' {
					pos++
				}
			case '\n':
				// перенос строки внутри строкового литерала игнорируется
			default:
				if esc >= '0' && esc <= '7' {
					oct := []byte{esc}
					for k := 0; k < 2 && pos+1 < n && src[pos+1] >= '0' && src[pos+1] <= '7'; k++ {
						pos++
						oct = append(oct, src[pos])
					}
					if v, err := strconv.ParseUint(string(oct), 8, 8); err == nil {
						buf = append(buf, byte(v))
					}
				} else {
					buf = append(buf, esc)
				}
			}
			pos++
			continue
		}
		if b == '(' {
			depth++
			buf = append(buf, b)
		} else if b == ')' {
			depth--
			if depth > 0 {
				buf = append(buf, b)
			}
		} else {
			buf = append(buf, b)
		}
		pos++
	}
	return decodePdfBytes(buf), pos
}

func readPdfHexString(src []byte, pos int) (string, int) {
	pos++ // пропускаем '<'
	var sb strings.Builder
	for pos < len(src) && src[pos] != '>' {
		if !isPdfSpace(src[pos]) {
			sb.WriteByte(src[pos])
		}
		pos++
	}
	if pos < len(src) && src[pos] == '>' {
		pos++
	}
	return strings.ToUpper(sb.String()), pos
}

func readPdfTextArray(src []byte, pos int, cmap map[string]string) (string, int) {
	pos++ // пропускаем '['
	var out strings.Builder
	n := len(src)
	for pos < n && src[pos] != ']' {
		if isPdfSpace(src[pos]) {
			pos++
			continue
		}
		if src[pos] == '(' {
			s, next := readPdfLiteralString(src, pos)
			out.WriteString(s)
			pos = next
			continue
		}
		if src[pos] == '<' {
			h, next := readPdfHexString(src, pos)
			out.WriteString(decodePdfHexWithCMap(h, cmap))
			pos = next
			continue
		}
		start := pos
		for pos < n && !isPdfSpace(src[pos]) && !isPdfDelimiter(src[pos]) {
			pos++
		}
		if pos > start {
			if adj, err := strconv.ParseFloat(string(src[start:pos]), 64); err == nil && adj < -150 {
				out.WriteByte(' ')
			}
		} else {
			pos++
		}
	}
	if pos < n && src[pos] == ']' {
		pos++
	}
	return out.String(), pos
}

func decodePdfOperand(op string, cmap map[string]string) string {
	switch {
	case strings.HasPrefix(op, "S:"):
		return op[2:]
	case strings.HasPrefix(op, "H:"):
		return decodePdfHexWithCMap(op[2:], cmap)
	default:
		return ""
	}
}

func decodePdfHexWithCMap(hexStr string, cmap map[string]string) string {
	if hexStr == "" {
		return ""
	}
	if len(hexStr)%2 == 1 {
		hexStr += "0"
	}
	if len(cmap) > 0 {
		// Пробуем сначала 2-байтовые (4 hex-символа), затем 1-байтовые (2 hex-символа) коды.
		var sb strings.Builder
		matched := false
		for i := 0; i < len(hexStr); {
			if i+4 <= len(hexStr) {
				if v, ok := cmap[hexStr[i:i+4]]; ok {
					sb.WriteString(v)
					matched = true
					i += 4
					continue
				}
			}
			if i+2 <= len(hexStr) {
				if v, ok := cmap[hexStr[i:i+2]]; ok {
					sb.WriteString(v)
					matched = true
					i += 2
					continue
				}
				if b, err := strconv.ParseUint(hexStr[i:i+2], 16, 8); err == nil && b >= 0x20 && b < 0x7f {
					sb.WriteByte(byte(b))
				}
				i += 2
			} else {
				break
			}
		}
		if matched {
			return sb.String()
		}
	}
	raw, err := hex.DecodeString(hexStr)
	if err != nil {
		return ""
	}
	return decodePdfBytes(raw)
}

func decodePdfBytes(raw []byte) string {
	if len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0xFF {
		u16 := make([]uint16, (len(raw)-2)/2)
		for i := range u16 {
			u16[i] = uint16(raw[2+2*i])<<8 | uint16(raw[2+2*i+1])
		}
		return string(utf16.Decode(u16))
	}
	if utf8.Valid(raw) {
		return string(raw)
	}
	return strings.ToValidUTF8(string(raw), "")
}
