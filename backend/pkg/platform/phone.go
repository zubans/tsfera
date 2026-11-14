package platform

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

// ParsePhone10 оставляет только цифры и берёт последние 10 символов (как PhoneParser/PhoneValidator в legacy CRM).
func ParsePhone10(raw string) (string, error) {
	var digits strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	s := digits.String()
	if len(s) < 10 {
		return "", NewValidation("Телефон должен содержать не менее 10 цифр", map[string]string{
			"phone": "Введен некорректный телефон",
		})
	}
	return s[len(s)-10:], nil
}

// FormatPhoneRU форматирует 10-значный номер в вид +7 (XXX) XXX-XX-XX.
func FormatPhoneRU(digits string) string {
	clean, err := ParsePhone10(digits)
	if err != nil {
		return digits
	}
	return fmt.Sprintf("+7 (%s) %s-%s-%s", clean[0:3], clean[3:6], clean[6:8], clean[8:10])
}

// ParseDateRange разбирает диапазон дат формата "DD.MM.YYYY - DD.MM.YYYY" или "YYYY-MM-DD - YYYY-MM-DD".
func ParseDateRange(raw string) (time.Time, time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, time.Time{}, NewValidation("Пустой диапазон дат", nil)
	}
	parts := strings.Split(raw, " - ")
	var fromStr, toStr string
	if len(parts) == 2 {
		fromStr = strings.TrimSpace(parts[0])
		toStr = strings.TrimSpace(parts[1])
	} else {
		fromStr = raw
		toStr = raw
	}
	from, err := parseFlexibleDate(fromStr)
	if err != nil {
		return time.Time{}, time.Time{}, NewValidation("Неверный формат начальной даты", map[string]string{"date_from": err.Error()})
	}
	to, err := parseFlexibleDate(toStr)
	if err != nil {
		return time.Time{}, time.Time{}, NewValidation("Неверный формат конечной даты", map[string]string{"date_to": err.Error()})
	}
	if to.Before(from) {
		return time.Time{}, time.Time{}, NewValidation("Конечная дата раньше начальной", nil)
	}
	// Если передана только дата без времени, включаем весь конечный день.
	if len(toStr) == 10 {
		to = to.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
	}
	return from, to, nil
}

func parseFlexibleDate(s string) (time.Time, error) {
	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02",
		"02.01.2006 15:04:05",
		"02.01.2006",
	}
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		loc = time.FixedZone("MSK", 3*3600)
	}
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, s, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("неподдерживаемый формат даты: %s", s)
}

// SlugifyAnchor строит URL-безопасный якорь для Markdown-заголовков (поддерживает кириллицу и латиницу).
func SlugifyAnchor(heading string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(heading)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash && b.Len() > 0 {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// TokenizeSearchQuery выделяет уникальные слова и цифровые группы для взвешенного поиска контрагентов.
func TokenizeSearchQuery(term string) (words []string, numGroups4to9 []string, phones10 []string) {
	term = strings.ToLower(term)
	wordSeen := map[string]bool{}
	numSeen := map[string]bool{}
	p10Seen := map[string]bool{}

	flushWord := func(w string) {
		if len(w) > 0 && !wordSeen[w] {
			wordSeen[w] = true
			words = append(words, w)
		}
	}
	flushNum := func(n string) {
		if len(n) == 10 && !p10Seen[n] {
			p10Seen[n] = true
			phones10 = append(phones10, n)
		} else if len(n) >= 4 && len(n) <= 9 && !numSeen[n] {
			numSeen[n] = true
			numGroups4to9 = append(numGroups4to9, n)
		} else if len(n) > 10 {
			last10 := n[len(n)-10:]
			if !p10Seen[last10] {
				p10Seen[last10] = true
				phones10 = append(phones10, last10)
			}
		}
	}

	var curWord, curNum strings.Builder
	for _, r := range term {
		switch {
		case unicode.IsLetter(r):
			if curNum.Len() > 0 {
				flushNum(curNum.String())
				curNum.Reset()
			}
			curWord.WriteRune(r)
		case r >= '0' && r <= '9':
			if curWord.Len() > 0 {
				flushWord(curWord.String())
				curWord.Reset()
			}
			curNum.WriteRune(r)
		default:
			if curWord.Len() > 0 {
				flushWord(curWord.String())
				curWord.Reset()
			}
			if curNum.Len() > 0 {
				flushNum(curNum.String())
				curNum.Reset()
			}
		}
	}
	flushWord(curWord.String())
	flushNum(curNum.String())
	return words, numGroups4to9, phones10
}
