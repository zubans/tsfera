package docs

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

// ExtractOutline строит оглавление документа по заголовкам Markdown (#..####), игнорируя блоки кода.
func ExtractOutline(md string) []OutlineItem {
	lines := strings.Split(md, "\n")
	var out []OutlineItem
	inFence := false
	for idx, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(trimmed, "#") {
			continue
		}
		level := 0
		for level < len(trimmed) && trimmed[level] == '#' {
			level++
		}
		if level == 0 || level > 4 || level >= len(trimmed) || trimmed[level] != ' ' {
			continue
		}
		title := strings.TrimSpace(trimmed[level+1:])
		if title == "" {
			continue
		}
		out = append(out, OutlineItem{
			Level:  level,
			Title:  title,
			Anchor: platform.SlugifyAnchor(title),
			Line:   idx + 1,
		})
	}
	return out
}

// ConvertDocxToMarkdown преобразует OOXML-документ .docx (word/document.xml) в чистый Markdown без внешних библиотек.
func ConvertDocxToMarkdown(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", platform.NewValidation("Некорректный .docx архив", nil)
	}
	var docXML []byte
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			docXML, err = io.ReadAll(io.LimitReader(rc, 32<<20))
			_ = rc.Close()
			if err != nil {
				return "", err
			}
			break
		}
	}
	if len(docXML) == 0 {
		return "", platform.NewValidation("В .docx отсутствует word/document.xml", nil)
	}
	return parseWordDocumentXML(docXML)
}

func parseWordDocumentXML(raw []byte) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	var out strings.Builder

	var inPara, inTable, inRow, inCell bool
	var style string
	var isList bool
	var boldRun bool
	var paraBuf strings.Builder
	var rowCells []string
	var tableRows [][]string

	flushTable := func() {
		if len(tableRows) == 0 {
			return
		}
		cols := len(tableRows[0])
		for idx, r := range tableRows {
			for len(r) < cols {
				r = append(r, "")
			}
			out.WriteString("| " + strings.Join(r, " | ") + " |\n")
			if idx == 0 {
				sep := make([]string, cols)
				for i := range sep {
					sep[i] = "---"
				}
				out.WriteString("| " + strings.Join(sep, " | ") + " |\n")
			}
		}
		out.WriteByte('\n')
		tableRows = nil
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "tbl":
				inTable = true
				tableRows = nil
			case "tr":
				if inTable {
					inRow = true
					rowCells = nil
				}
			case "tc":
				if inRow {
					inCell = true
					paraBuf.Reset()
				}
			case "p":
				inPara = true
				style = ""
				isList = false
				if !inCell {
					paraBuf.Reset()
				}
			case "pStyle":
				if inPara {
					for _, a := range t.Attr {
						if a.Name.Local == "val" {
							style = a.Value
						}
					}
				}
			case "numPr":
				if inPara {
					isList = true
				}
			case "b":
				boldRun = true
			case "br":
				if inPara {
					paraBuf.WriteByte('\n')
				}
			case "t":
				var txt string
				if err := dec.DecodeElement(&txt, &t); err == nil {
					if boldRun && !strings.HasPrefix(style, "Heading") && strings.TrimSpace(txt) != "" {
						paraBuf.WriteString("**" + txt + "**")
					} else {
						paraBuf.WriteString(txt)
					}
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "r":
				boldRun = false
			case "p":
				if !inPara {
					continue
				}
				inPara = false
				if inCell {
					if paraBuf.Len() > 0 {
						paraBuf.WriteByte(' ')
					}
					continue
				}
				line := strings.TrimSpace(paraBuf.String())
				if line == "" {
					continue
				}
				prefix := docxHeadingPrefix(style)
				if prefix != "" {
					out.WriteString(prefix + line + "\n\n")
				} else if isList {
					out.WriteString("- " + line + "\n")
				} else {
					out.WriteString(line + "\n\n")
				}
			case "tc":
				if inCell {
					inCell = false
					cellText := strings.ReplaceAll(strings.TrimSpace(paraBuf.String()), "|", `\|`)
					rowCells = append(rowCells, cellText)
					paraBuf.Reset()
				}
			case "tr":
				if inRow {
					inRow = false
					if len(rowCells) > 0 {
						tableRows = append(tableRows, rowCells)
					}
				}
			case "tbl":
				if inTable {
					inTable = false
					flushTable()
				}
			}
		}
	}
	return strings.TrimSpace(out.String()), nil
}

func docxHeadingPrefix(style string) string {
	s := strings.ToLower(strings.TrimSpace(style))
	switch {
	case s == "title" || s == "heading1" || s == "1":
		return "# "
	case s == "heading2" || s == "2":
		return "## "
	case s == "heading3" || s == "3":
		return "### "
	case s == "heading4" || s == "4":
		return "#### "
	default:
		return ""
	}
}

// ExtractXlsxText извлекает таблицы из .xlsx (sharedStrings + worksheets) в текстовые строки для поиска и ИИ.
func ExtractXlsxText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", platform.NewValidation("Некорректный .xlsx архив", nil)
	}
	shared := readXlsxSharedStrings(zr)
	var sheetFiles []*zip.File
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			sheetFiles = append(sheetFiles, f)
		}
	}
	sort.Slice(sheetFiles, func(i, j int) bool { return sheetFiles[i].Name < sheetFiles[j].Name })

	var out strings.Builder
	for _, sf := range sheetFiles {
		rc, err := sf.Open()
		if err != nil {
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(rc, 16<<20))
		_ = rc.Close()
		fmt.Fprintf(&out, "### Лист %s\n", strings.TrimSuffix(filepath.Base(sf.Name), ".xml"))
		extractSheetRows(raw, shared, &out)
		out.WriteByte('\n')
	}
	return strings.TrimSpace(out.String()), nil
}

func readXlsxSharedStrings(zr *zip.Reader) []string {
	for _, f := range zr.File {
		if f.Name != "xl/sharedStrings.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil
		}
		defer rc.Close()
		var sst struct {
			Items []struct {
				T    string `xml:"t"`
				Runs []struct {
					T string `xml:"t"`
				} `xml:"r"`
			} `xml:"si"`
		}
		if err := xml.NewDecoder(io.LimitReader(rc, 16<<20)).Decode(&sst); err != nil {
			return nil
		}
		out := make([]string, len(sst.Items))
		for i, item := range sst.Items {
			if item.T != "" {
				out[i] = item.T
				continue
			}
			var sb strings.Builder
			for _, r := range item.Runs {
				sb.WriteString(r.T)
			}
			out[i] = sb.String()
		}
		return out
	}
	return nil
}

func extractSheetRows(raw []byte, shared []string, out *strings.Builder) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	var rowVals []string
	var inCell bool
	var cellType string
	var valBuf strings.Builder

	for {
		tok, err := dec.Token()
		if err != nil {
			return
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				rowVals = nil
			case "c":
				inCell = true
				cellType = ""
				valBuf.Reset()
				for _, a := range t.Attr {
					if a.Name.Local == "t" {
						cellType = a.Value
					}
				}
			case "v", "t":
				if inCell {
					var s string
					if err := dec.DecodeElement(&s, &t); err == nil {
						valBuf.WriteString(s)
					}
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "c":
				inCell = false
				v := strings.TrimSpace(valBuf.String())
				if cellType == "s" {
					if idx, err := strconv.Atoi(v); err == nil && idx >= 0 && idx < len(shared) {
						v = shared[idx]
					}
				}
				if v != "" {
					rowVals = append(rowVals, v)
				}
			case "row":
				if len(rowVals) > 0 {
					out.WriteString("| " + strings.Join(rowVals, " | ") + " |\n")
				}
			}
		}
	}
}

// ExtractAttachmentText извлекает текст вложения для индексации и AI Compose по расширению файла.
func ExtractAttachmentText(filename string, data []byte) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".md", ".txt", ".csv", ".json", ".yaml", ".yml", ".log":
		if utf8.Valid(data) {
			return string(data)
		}
		return strings.ToValidUTF8(string(data), "")
	case ".docx":
		md, err := ConvertDocxToMarkdown(data)
		if err == nil {
			return md
		}
	case ".xlsx":
		txt, err := ExtractXlsxText(data)
		if err == nil {
			return txt
		}
	}
	return ""
}
