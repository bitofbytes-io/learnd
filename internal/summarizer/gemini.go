package summarizer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

const (
	geminiProvider     = "gemini"
	geminiDefaultModel = "gemini-3.1-flash-lite"
	geminiVersion      = "1.0.0"
)

// GeminiSummarizer implements Summarizer using Google's Gemini API
type GeminiSummarizer struct {
	client    *genai.Client
	model     *genai.GenerativeModel
	modelName string
}

// NewGeminiSummarizer creates a new Gemini summarizer
func NewGeminiSummarizer(ctx context.Context, apiKey string) (*GeminiSummarizer, error) {
	return NewGeminiSummarizerWithModel(ctx, apiKey, geminiDefaultModel)
}

// NewGeminiSummarizerWithModel creates a new Gemini summarizer using the given model.
func NewGeminiSummarizerWithModel(ctx context.Context, apiKey, modelName string) (*GeminiSummarizer, error) {
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		modelName = geminiDefaultModel
	}

	model := client.GenerativeModel(modelName)

	// Configure for concise summaries
	temp := float32(0.3)
	model.Temperature = &temp

	maxTokens := int32(150)
	model.MaxOutputTokens = &maxTokens

	return &GeminiSummarizer{
		client:    client,
		model:     model,
		modelName: modelName,
	}, nil
}

func (g *GeminiSummarizer) Provider() string { return geminiProvider }
func (g *GeminiSummarizer) Model() string    { return g.modelName }
func (g *GeminiSummarizer) Version() string  { return geminiVersion }

func (g *GeminiSummarizer) Summarize(ctx context.Context, input Input) (*Result, error) {
	prompt := buildPrompt(input)

	resp, err := g.model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return nil, fmt.Errorf("gemini generation failed: %w", err)
	}

	text := extractText(resp)
	if text == "" {
		return nil, fmt.Errorf("no text generated")
	}

	return &Result{
		Text:        text,
		Provider:    g.Provider(),
		Model:       g.Model(),
		Version:     g.Version(),
		GeneratedAt: time.Now().UTC(),
	}, nil
}

// Close closes the Gemini client
func (g *GeminiSummarizer) Close() error {
	return g.client.Close()
}

func buildPrompt(input Input) string {
	var sb strings.Builder

	sb.WriteString("Summarize this ")
	sb.WriteString(string(input.SourceType))
	sb.WriteString(" in 1-2 concise sentences for a learning log. ")
	sb.WriteString("Focus on the key takeaway or main topic. Be direct and informative.\n\n")

	if input.Title != "" {
		sb.WriteString("Title: ")
		sb.WriteString(input.Title)
		sb.WriteString("\n\n")
	}

	if input.Description != "" {
		// Limit description to avoid token limits
		desc := input.Description
		if len(desc) > 1000 {
			desc = desc[:1000] + "..."
		}
		sb.WriteString("Description: ")
		sb.WriteString(desc)
		sb.WriteString("\n\n")
	}

	if input.Tag != "" {
		sb.WriteString("Topics: ")
		sb.WriteString(input.Tag)
		sb.WriteString("\n\n")
	}

	sb.WriteString("Summary:")

	return sb.String()
}

func extractText(resp *genai.GenerateContentResponse) string {
	if resp == nil || len(resp.Candidates) == 0 {
		return ""
	}

	candidate := resp.Candidates[0]
	if candidate.Content == nil || len(candidate.Content.Parts) == 0 {
		return ""
	}

	var result strings.Builder
	for _, part := range candidate.Content.Parts {
		if text, ok := part.(genai.Text); ok {
			result.WriteString(string(text))
		}
	}

	return strings.TrimSpace(result.String())
}
