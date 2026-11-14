package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"
	"strings"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

const DefaultChunkMaxChars = 1800

// ChunkItem разбивает индексный элемент на смысловые чанки (~1800 символов) с сохранением заголовка раздела и anchor.
func ChunkItem(item platform.IndexItem, maxChars int) []SearchChunk {
	if maxChars <= 0 {
		maxChars = DefaultChunkMaxChars
	}
	content := strings.TrimSpace(item.Content)
	if content == "" {
		content = item.Title
	}
	lines := strings.Split(content, "\n")

	var chunks []SearchChunk
	curHeading := ""
	curAnchor := ""
	var buf strings.Builder

	flush := func() {
		txt := strings.TrimSpace(buf.String())
		if txt == "" {
			return
		}
		if curHeading != "" && !strings.HasPrefix(txt, "#") {
			txt = curHeading + "\n\n" + txt
		}
		metadata, _ := json.Marshal(item.Metadata)
		sum := sha256.Sum256([]byte(item.Title + "\n" + txt + "\n" + item.Path + "\n" + string(metadata)))
		chunks = append(chunks, SearchChunk{
			SourceType:  item.SourceType,
			SourceID:    item.SourceID,
			ChunkIndex:  len(chunks),
			Title:       item.Title,
			Anchor:      curAnchor,
			Content:     txt,
			Path:        item.Path,
			Metadata:    item.Metadata,
			ContentHash: hex.EncodeToString(sum[:]),
			UpdatedAt:   item.UpdatedAt,
		})
		buf.Reset()
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		isHeading := strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "### ")
		if isHeading && buf.Len() > 0 {
			flush()
		}
		if isHeading {
			curHeading = trimmed
			headingTitle := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			curAnchor = platform.SlugifyAnchor(headingTitle)
		}
		if buf.Len()+len(line)+1 > maxChars && buf.Len() > 0 {
			flush()
		}
		if buf.Len() > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(line)
	}
	flush()
	return chunks
}

func CosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

// ReciprocalRankFusion объединяет ранги FTS и векторного поиска по формуле RRF (k=60).
func ReciprocalRankFusion(ftsRanked, vecRanked []SearchResult, k int) []SearchResult {
	if k <= 0 {
		k = 60
	}
	type entry struct {
		res   SearchResult
		score float64
	}
	merged := make(map[string]*entry)

	addList := func(list []SearchResult) {
		for idx, item := range list {
			key := item.SourceType + ":" + item.SourceID + ":" + item.Anchor
			e, ok := merged[key]
			if !ok {
				cp := item
				cp.Score = 0
				e = &entry{res: cp}
				merged[key] = e
			}
			e.score += 1.0 / float64(k+idx+1)
		}
	}

	addList(ftsRanked)
	addList(vecRanked)

	out := make([]SearchResult, 0, len(merged))
	for _, e := range merged {
		e.res.Score = e.score
		out = append(out, e.res)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Score > out[j].Score
	})
	return out
}

func MakeSnippet(text string, limit int) string {
	if limit <= 0 {
		limit = 220
	}
	clean := strings.Join(strings.Fields(text), " ")
	runes := []rune(clean)
	if len(runes) <= limit {
		return clean
	}
	return string(runes[:limit]) + "…"
}
