package codesearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

const (
	voyageBaseURL = "https://api.voyageai.com/v1"
	// Topes del proveedor: 1000 textos por request, y 120K tokens por lote en
	// voyage-code-3 (los modelos de la serie 4 admiten más, pero el más bajo
	// sirve para todos).
	voyageMaxBatchTexts  = 1000
	voyageMaxBatchTokens = 120_000
	voyageMaxRerankDocs  = 1000
	voyageMaxAttempts    = 4
)

// Voyage habla con la API de Voyage AI para embeber y reordenar. Un solo tipo
// cubre ambas cosas porque comparten host, credencial y política de reintentos.
type Voyage struct {
	APIKey      string
	EmbedModel  string
	RerankModel string
	Dimensions  int
	BaseURL     string
	HTTP        *http.Client
}

// Dims es la dimensión configurada de los vectores.
func (v *Voyage) Dims() int { return v.Dimensions }

// Model identifica el modelo de embeddings en uso.
func (v *Voyage) Model() string { return v.EmbedModel }

type voyageEmbedRequest struct {
	Input           []string `json:"input"`
	Model           string   `json:"model"`
	InputType       string   `json:"input_type"`
	OutputDtype     string   `json:"output_dtype"`
	OutputDimension int      `json:"output_dimension,omitempty"`
}

type voyageEmbedResponse struct {
	Data []struct {
		Embedding []int  `json:"embedding"`
		Index     int    `json:"index"`
		Object    string `json:"object"`
	} `json:"data"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

// Embed devuelve un vector por texto, en el orden de entrada. Los textos se
// mandan en lotes para no rebasar los topes del proveedor.
func (v *Voyage) Embed(ctx context.Context, texts []string, kind InputKind) ([][]int8, error) {
	out := make([][]int8, 0, len(texts))
	for _, batch := range batchTexts(texts) {
		body := voyageEmbedRequest{
			Input:           batch,
			Model:           v.EmbedModel,
			InputType:       string(kind),
			OutputDtype:     "int8",
			OutputDimension: v.Dimensions,
		}
		var resp voyageEmbedResponse
		if err := v.post(ctx, "/embeddings", body, &resp); err != nil {
			return nil, err
		}
		if len(resp.Data) != len(batch) {
			return nil, fmt.Errorf("voyage: se pidieron %d embeddings y llegaron %d", len(batch), len(resp.Data))
		}
		// El proveedor no garantiza el orden de data, solo el campo index.
		ordered := make([][]int8, len(batch))
		for _, d := range resp.Data {
			if d.Index < 0 || d.Index >= len(batch) {
				return nil, fmt.Errorf("voyage: índice %d fuera del lote de %d", d.Index, len(batch))
			}
			if len(d.Embedding) != v.Dimensions {
				return nil, fmt.Errorf("voyage: vector de %d dimensiones, se configuraron %d", len(d.Embedding), v.Dimensions)
			}
			ordered[d.Index] = toInt8(d.Embedding)
		}
		out = append(out, ordered...)
	}
	return out, nil
}

type voyageRerankRequest struct {
	Query           string   `json:"query"`
	Documents       []string `json:"documents"`
	Model           string   `json:"model"`
	TopK            int      `json:"top_k,omitempty"`
	ReturnDocuments bool     `json:"return_documents"`
	Truncation      bool     `json:"truncation"`
}

type voyageRerankResponse struct {
	Data []struct {
		Index          int     `json:"index"`
		RelevanceScore float32 `json:"relevance_score"`
	} `json:"data"`
}

// Rerank reordena docs contra la consulta y recorta a topK. Los documentos se
// mandan completos en un request: el reranker necesita verlos juntos para
// comparar, así que no se puede lotear sin romper el orden global.
func (v *Voyage) Rerank(ctx context.Context, query string, docs []string, topK int) ([]Ranked, error) {
	if len(docs) == 0 {
		return nil, nil
	}
	if len(docs) > voyageMaxRerankDocs {
		docs = docs[:voyageMaxRerankDocs]
	}
	body := voyageRerankRequest{
		Query:      query,
		Documents:  docs,
		Model:      v.RerankModel,
		TopK:       topK,
		Truncation: true,
	}
	var resp voyageRerankResponse
	if err := v.post(ctx, "/rerank", body, &resp); err != nil {
		return nil, err
	}
	out := make([]Ranked, 0, len(resp.Data))
	for _, d := range resp.Data {
		if d.Index < 0 || d.Index >= len(docs) {
			continue
		}
		out = append(out, Ranked{Index: d.Index, Score: d.RelevanceScore})
	}
	return out, nil
}

// post hace la llamada y reintenta los fallos transitorios. El 429 importa
// especialmente: durante un escaneo inicial se disparan cientos de lotes
// seguidos y el límite de tasa se toca seguido.
func (v *Voyage) post(ctx context.Context, path string, body, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	base := v.BaseURL
	if base == "" {
		base = voyageBaseURL
	}
	client := v.HTTP
	if client == nil {
		client = http.DefaultClient
	}

	var lastErr error
	for attempt := range voyageMaxAttempts {
		if attempt > 0 {
			if err := sleepCtx(ctx, backoff(attempt)); err != nil {
				return err
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(data))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+v.APIKey)

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		payload, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode == http.StatusOK {
			return json.Unmarshal(payload, out)
		}
		detail := detailOf(payload)
		lastErr = fmt.Errorf("voyage %s: %s: %s", path, resp.Status, detail)
		// La cuota agotada se marca aparte: no se arregla reintentando y el
		// usuario tiene que enterarse para reponer la cuenta.
		if quotaExhausted(resp.StatusCode, detail) {
			return fmt.Errorf("%w: %s", ErrQuotaExhausted, detail)
		}
		if !retryableStatus(resp.StatusCode) {
			return lastErr
		}
	}
	return lastErr
}

func retryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}

func backoff(attempt int) time.Duration {
	return time.Duration(math.Pow(2, float64(attempt))) * time.Second
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// detailOf saca el mensaje del error de la API, que viene en "detail".
func detailOf(payload []byte) string {
	var e struct {
		Detail string `json:"detail"`
	}
	if json.Unmarshal(payload, &e) == nil && e.Detail != "" {
		return e.Detail
	}
	return strings.TrimSpace(string(payload))
}

// batchTexts parte los textos en lotes que respetan el tope de cantidad y el de
// tokens. Los tokens se estiman por caracteres porque tokenizar aquí costaría
// más que el margen que da estimar de más.
func batchTexts(texts []string) [][]string {
	var out [][]string
	var current []string
	tokens := 0
	for _, t := range texts {
		est := estimateTokens(t)
		if len(current) > 0 && (len(current) >= voyageMaxBatchTexts || tokens+est > voyageMaxBatchTokens) {
			out = append(out, current)
			current, tokens = nil, 0
		}
		current = append(current, t)
		tokens += est
	}
	if len(current) > 0 {
		out = append(out, current)
	}
	return out
}

func estimateTokens(s string) int { return len(s)/4 + 1 }

func toInt8(v []int) []int8 {
	out := make([]int8, len(v))
	for i, x := range v {
		out[i] = int8(max(-128, min(127, x)))
	}
	return out
}

var (
	_ Embedder = (*Voyage)(nil)
	_ Reranker = (*Voyage)(nil)
)
