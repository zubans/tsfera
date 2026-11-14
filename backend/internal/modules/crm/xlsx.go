package crm

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"fmt"
	"html"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// BuildXLSX формирует валидный .xlsx файл (OOXML) средствами стандартной библиотеки Go без внешних зависимостей.
func BuildXLSX(sheetName string, headers []string, rows [][]any) ([]byte, error) {
	sheetName = sanitizeSheetName(sheetName)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	addFile := func(name, content string) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, content)
		return err
	}

	if err := addFile("[Content_Types].xml", contentTypesXML); err != nil {
		return nil, err
	}
	if err := addFile("_rels/.rels", rootRelsXML); err != nil {
		return nil, err
	}
	if err := addFile("xl/workbook.xml", fmt.Sprintf(workbookXMLTemplate, escapeXMLText(sheetName))); err != nil {
		return nil, err
	}
	if err := addFile("xl/_rels/workbook.xml.rels", workbookRelsXML); err != nil {
		return nil, err
	}
	if err := addFile("xl/styles.xml", stylesXML); err != nil {
		return nil, err
	}

	sheetXML := buildWorksheetXML(headers, rows)
	if err := addFile("xl/worksheets/sheet1.xml", sheetXML); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// BuildCSV формирует UTF-8 BOM CSV с разделителем ';' для корректного открытия в Microsoft Excel и LibreOffice.
func BuildCSV(headers []string, rows [][]string) ([]byte, error) {
	var buf bytes.Buffer
	// UTF-8 BOM чтобы Excel в кириллической локали корректно распознавал кодировку.
	buf.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(&buf)
	w.Comma = ';'
	if len(headers) > 0 {
		if err := w.Write(headers); err != nil {
			return nil, err
		}
	}
	for _, r := range rows {
		if err := w.Write(r); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func buildWorksheetXML(headers []string, rows [][]any) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	sb.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)

	rowNum := 1
	if len(headers) > 0 {
		sb.WriteString(fmt.Sprintf(`<row r="%d">`, rowNum))
		for colIdx, h := range headers {
			ref := cellRef(colIdx, rowNum)
			sb.WriteString(fmt.Sprintf(`<c r="%s" s="1" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, escapeXMLText(h)))
		}
		sb.WriteString(`</row>`)
		rowNum++
	}

	for _, r := range rows {
		sb.WriteString(fmt.Sprintf(`<row r="%d">`, rowNum))
		for colIdx, val := range r {
			ref := cellRef(colIdx, rowNum)
			sb.WriteString(formatXLSXCell(ref, val))
		}
		sb.WriteString(`</row>`)
		rowNum++
	}

	sb.WriteString(`</sheetData></worksheet>`)
	return sb.String()
}

func formatXLSXCell(ref string, val any) string {
	switch v := val.(type) {
	case nil:
		return ""
	case int:
		return fmt.Sprintf(`<c r="%s" t="n"><v>%d</v></c>`, ref, v)
	case int64:
		return fmt.Sprintf(`<c r="%s" t="n"><v>%d</v></c>`, ref, v)
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Sprintf(`<c r="%s" t="inlineStr"><is><t>0</t></is></c>`, ref)
		}
		return fmt.Sprintf(`<c r="%s" t="n"><v>%s</v></c>`, ref, strconv.FormatFloat(v, 'f', -1, 64))
	case bool:
		if v {
			return fmt.Sprintf(`<c r="%s" t="inlineStr"><is><t>Да</t></is></c>`, ref)
		}
		return fmt.Sprintf(`<c r="%s" t="inlineStr"><is><t>Нет</t></is></c>`, ref)
	default:
		s := fmt.Sprint(val)
		return fmt.Sprintf(`<c r="%s" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, escapeXMLText(s))
	}
}

func cellRef(colIdx, rowNum int) string {
	var col strings.Builder
	n := colIdx
	for {
		col.WriteByte(byte('A' + (n % 26)))
		n = n/26 - 1
		if n < 0 {
			break
		}
	}
	b := []byte(col.String())
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return fmt.Sprintf("%s%d", string(b), rowNum)
}

func sanitizeSheetName(name string) string {
	name = strings.TrimSpace(name)
	repl := strings.NewReplacer(":", " ", "\\", " ", "/", " ", "?", " ", "*", " ", "[", "(", "]", ")")
	name = repl.Replace(name)
	if name == "" {
		return "Sheet1"
	}
	runes := []rune(name)
	if len(runes) > 31 {
		name = string(runes[:31])
	}
	return name
}

func escapeXMLText(s string) string {
	var clean strings.Builder
	clean.Grow(len(s))
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		s = s[size:]
		if r == utf8.RuneError && size == 1 {
			continue
		}
		if r == 0x09 || r == 0x0A || r == 0x0D || (r >= 0x20 && r <= 0xD7FF) || (r >= 0xE000 && r <= 0xFFFD) || (r >= 0x10000 && r <= 0x10FFFF) {
			clean.WriteRune(r)
		}
	}
	return html.EscapeString(clean.String())
}

const contentTypesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
  <Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
  <Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>
</Types>`

const rootRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`

const workbookXMLTemplate = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets>
    <sheet name="%s" sheetId="1" r:id="rId1"/>
  </sheets>
</workbook>`

const workbookRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
</Relationships>`

const stylesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <fonts count="2">
    <font><sz val="11"/><name val="Calibri"/></font>
    <font><b/><sz val="11"/><name val="Calibri"/></font>
  </fonts>
  <fills count="2">
    <fill><patternFill patternType="none"/></fill>
    <fill><patternFill patternType="gray125"/></fill>
  </fills>
  <borders count="1">
    <border><left/><right/><top/><bottom/><diagonal/></border>
  </borders>
  <cellStyleXfs count="1">
    <xf numFmtId="0" fontId="0" fillId="0" borderId="0"/>
  </cellStyleXfs>
  <cellXfs count="2">
    <xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>
    <xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/>
  </cellXfs>
</styleSheet>`
