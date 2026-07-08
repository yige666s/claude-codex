package agentruntime

import (
	"encoding/csv"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const defaultRAGEvalSetID = "black-wukong-rag"

type RAGEvaluationRunRequest struct {
	ID            string               `json:"id"`
	Name          string               `json:"name"`
	Trigger       string               `json:"trigger"`
	SetID         string               `json:"set_id"`
	SetVersion    string               `json:"set_version"`
	SetName       string               `json:"set_name"`
	Description   string               `json:"description"`
	KnowledgeText string               `json:"knowledge_text"`
	CSVContent    string               `json:"csv_content"`
	Judge         string               `json:"judge"`
	ChunkSize     int                  `json:"chunk_size"`
	ChunkOverlap  int                  `json:"chunk_overlap"`
	TopK          int                  `json:"top_k"`
	PersistSet    *bool                `json:"persist_set,omitempty"`
	Thresholds    EvaluationThresholds `json:"thresholds"`
}

type ragEvaluationInput struct {
	Set        GoldenSet
	Candidates []GoldenCandidate
	Chunks     []GoldenEvidence
}

func buildRAGEvaluationInput(req RAGEvaluationRunRequest) (ragEvaluationInput, error) {
	setID := strings.TrimSpace(req.SetID)
	if setID == "" {
		setID = defaultRAGEvalSetID
	}
	version := strings.TrimSpace(req.SetVersion)
	if version == "" {
		version = "v1"
	}
	knowledge := strings.TrimSpace(req.KnowledgeText)
	if knowledge == "" {
		return ragEvaluationInput{}, fmt.Errorf("knowledge text is required")
	}
	csvContent := strings.TrimSpace(strings.TrimPrefix(req.CSVContent, "\ufeff"))
	if csvContent == "" {
		return ragEvaluationInput{}, fmt.Errorf("golden CSV content is required")
	}
	chunkSize := req.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 200
	}
	if chunkSize > 4096 {
		chunkSize = 4096
	}
	chunkOverlap := req.ChunkOverlap
	if chunkOverlap < 0 {
		chunkOverlap = 0
	}
	if chunkOverlap >= chunkSize {
		chunkOverlap = chunkSize / 5
	}
	topK := req.TopK
	if topK <= 0 {
		topK = 4
	}
	if topK > 20 {
		topK = 20
	}

	cases, err := parseRAGGoldenCSV(setID, version, csvContent)
	if err != nil {
		return ragEvaluationInput{}, err
	}
	chunks := chunkRAGKnowledge(knowledge, chunkSize, chunkOverlap)
	if len(chunks) == 0 {
		return ragEvaluationInput{}, fmt.Errorf("knowledge text produced no chunks")
	}
	candidates := make([]GoldenCandidate, 0, len(cases))
	for _, item := range cases {
		retrieved := retrieveRAGEvidence(item.Query, chunks, topK)
		candidates = append(candidates, GoldenCandidate{
			CaseID:            item.ID,
			Output:            buildRAGCandidateAnswer(retrieved),
			RetrievedEvidence: retrieved,
			Metadata: map[string]any{
				"source":      "admin_rag_eval",
				"answer_mode": "extractive",
				"top_k":       topK,
			},
		})
	}
	set := GoldenSet{
		ID:          setID,
		Name:        firstNonEmptyString(strings.TrimSpace(req.SetName), strings.TrimSpace(req.Name), "Black Wukong RAG"),
		Description: firstNonEmptyString(strings.TrimSpace(req.Description), "RAG evaluation set imported from knowledge text and question/answer CSV."),
		Version:     version,
		Metadata: map[string]any{
			"source":                "admin_rag_eval",
			"csv_case_count":        len(cases),
			"knowledge_chunk_count": len(chunks),
			"chunk_size":            chunkSize,
			"chunk_overlap":         chunkOverlap,
			"top_k":                 topK,
		},
		Cases: cases,
	}
	return ragEvaluationInput{Set: normalizeGoldenSet(set), Candidates: candidates, Chunks: chunks}, nil
}

func parseRAGGoldenCSV(setID, version, content string) ([]GoldenCase, error) {
	reader := csv.NewReader(strings.NewReader(content))
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse golden CSV: %w", err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("golden CSV requires a header and at least one case")
	}
	header := map[string]int{}
	for index, name := range records[0] {
		header[normalizeRAGCSVHeader(name)] = index
	}
	questionIndex, ok := firstRAGCSVColumn(header, "question", "query", "prompt", "input")
	if !ok {
		return nil, fmt.Errorf("golden CSV requires a question column")
	}
	answerIndex, ok := firstRAGCSVColumn(header, "answer", "expected_answer", "reference", "ground_truth", "groundtruth")
	if !ok {
		return nil, fmt.Errorf("golden CSV requires an answer column")
	}

	cases := make([]GoldenCase, 0, len(records)-1)
	for rowIndex, record := range records[1:] {
		question := csvCell(record, questionIndex)
		answer := csvCell(record, answerIndex)
		if question == "" && answer == "" {
			continue
		}
		if question == "" || answer == "" {
			return nil, fmt.Errorf("golden CSV row %d requires both question and answer", rowIndex+2)
		}
		id := stableEvaluationSubjectID(strings.Join([]string{setID, version, strconv.Itoa(rowIndex + 1), question}, "\n"))
		facts := splitEvaluationSentences(answer)
		if len(facts) == 0 {
			facts = []string{answer}
		}
		cases = append(cases, GoldenCase{
			ID:             id,
			Query:          question,
			ExpectedAnswer: answer,
			ExpectedFacts:  facts,
			GoldEvidence: []GoldenEvidence{{
				ID:      stableEvaluationSubjectID("gold_answer\n" + id + "\n" + answer),
				Content: answer,
				Source:  "gold_answer",
				Metadata: map[string]any{
					"row": rowIndex + 2,
				},
			}},
			Tags: []string{"rag", "goldset"},
			Metadata: map[string]any{
				"csv_row": rowIndex + 2,
			},
		})
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("golden CSV contains no usable cases")
	}
	return cases, nil
}

func normalizeRAGCSVHeader(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "\ufeff")))
	value = strings.ReplaceAll(value, " ", "_")
	value = strings.ReplaceAll(value, "-", "_")
	return value
}

func firstRAGCSVColumn(header map[string]int, names ...string) (int, bool) {
	for _, name := range names {
		if index, ok := header[name]; ok {
			return index, true
		}
	}
	return 0, false
}

func csvCell(record []string, index int) string {
	if index < 0 || index >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[index])
}

func chunkRAGKnowledge(text string, chunkSize, overlap int) []GoldenEvidence {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return nil
	}
	step := chunkSize - overlap
	if step <= 0 {
		step = chunkSize
	}
	out := make([]GoldenEvidence, 0, len(runes)/step+1)
	for start := 0; start < len(runes); start += step {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		content := strings.TrimSpace(string(runes[start:end]))
		if content != "" {
			out = append(out, GoldenEvidence{
				ID:      fmt.Sprintf("chunk-%04d", len(out)+1),
				Content: content,
				Source:  "knowledge_text",
				Metadata: map[string]any{
					"start": start,
					"end":   end,
				},
			})
		}
		if end == len(runes) {
			break
		}
	}
	return out
}

func retrieveRAGEvidence(query string, chunks []GoldenEvidence, topK int) []GoldenEvidence {
	type scoredChunk struct {
		chunk GoldenEvidence
		score float64
		index int
	}
	scored := make([]scoredChunk, 0, len(chunks))
	for index, chunk := range chunks {
		scored = append(scored, scoredChunk{chunk: chunk, score: tokenOverlapScore(query, chunk.Content), index: index})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].index < scored[j].index
		}
		return scored[i].score > scored[j].score
	})
	if topK > len(scored) {
		topK = len(scored)
	}
	out := make([]GoldenEvidence, 0, topK)
	for _, item := range scored[:topK] {
		chunk := item.chunk
		if chunk.Metadata == nil {
			chunk.Metadata = map[string]any{}
		}
		chunk.Metadata["retrieval_score"] = item.score
		out = append(out, chunk)
	}
	return out
}

func buildRAGCandidateAnswer(retrieved []GoldenEvidence) string {
	parts := make([]string, 0, len(retrieved))
	for _, item := range retrieved {
		if text := strings.TrimSpace(item.Content); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	answer := strings.Join(parts, "\n")
	return truncateEvaluationString(answer, 4096)
}
