package model

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	ggufparser "github.com/gpustack/gguf-parser-go"
)

// Structs for parsing GGUF data from Parser
type ModelEstimate struct {
	Estimate     ModelEstimateItems `json:"estimate"`
	Architecture Architecture       `json:"architecture"`
	Metadata     Metadata           `json:"metadata"`
	Tokenizer    Tokenizer          `json:"tokenizer"`
}

type ModelEstimateItems struct {
	Items             []ModelMemory `json:"items"`
	Type              string        `json:"type"`
	Architecture      string        `json:"architecture"`
	ContextSize       int           `json:"contextSize"`
	FlashAttention    bool          `json:"flashAttention"`
	NoMMap            bool          `json:"noMMap"`
	EmbeddingOnly     bool          `json:"embeddingOnly"`
	Distributable     bool          `json:"distributable"`
	LogicalBatchSize  int32         `json:"logicalBatchSize"`
	PhysicalBatchSize int32         `json:"physicalBatchSize"`
}

type ModelMemory struct {
	OffloadLayers uint64         `json:"offloadLayers"`
	FullOffloaded bool           `json:"fullOffloaded"`
	RAM           EstimateRAM    `json:"ram"`
	VRAMs         []EstimateVRAM `json:"vrams"`
}

type EstimateRAM struct {
	UMA    uint64 `json:"uma"`
	NonUMA uint64 `json:"nonuma"`
}

type EstimateVRAM struct {
	UMA    uint64 `json:"uma"`
	NonUMA uint64 `json:"nonuma"`
}

type Architecture struct {
	Type                 string `json:"type"`
	Architecture         string `json:"architecture"`
	MaximumContextLength int    `json:"maximumContextLength"`
	EmbeddingLength      int    `json:"embeddingLength"`
	VocabularyLength     int    `json:"vocabularyLength"`
}

type Metadata struct {
	Type                string `json:"type"`
	Architecture        string `json:"architecture"`
	QuantizationVersion int    `json:"quantizationVersion"`
	Alignment           int    `json:"alignment"`
	Name                string `json:"name"`
	License             string `json:"license"`
	FileType            int    `json:"fileType"`
	LittleEndian        bool   `json:"littleEndian"`
	FileSize            int64  `json:"fileSize"`
	Size                int64  `json:"size"`
	Parameters          int64  `json:"parameters"`
}

type Tokenizer struct {
	Model        string `json:"model"`
	TokensLength int    `json:"tokensLength"`
	TokensSize   int    `json:"tokensSize"`
}

// Default platform footprint from ggufparser
const nonUMARamFootprint = uint64(150 * 1024 * 1024)  // 150 MiB
const nonUMAVramFootprint = uint64(250 * 1024 * 1024) // 250 MiB

func GetModelGGufData(modelPath string) (*ModelEstimate, error) {
	ctx := context.Background()

	// Check if the input is a valid URL
	if isURL(modelPath) {
		fmt.Println("Input is a URL.")
		ggufRemoteData, err := ggufparser.ParseGGUFFileRemote(ctx, modelPath)
		if err != nil {
			return nil, fmt.Errorf("error parsing GGUF file from remote URL: %v", err)
		}
		return estimateModelMemoryUsage(ggufRemoteData)

		// Check if the input is an Ollama model
	} else if strings.HasSuffix(modelPath, "ollama") {
		fmt.Println("Input is an Ollama model.")
		ggufOllamaData, err := ggufparser.ParseGGUFFileFromOllama(ctx, modelPath)
		if err != nil {
			return nil, fmt.Errorf("error parsing GGUF file from Ollama model: %v", err)
		}
		return estimateModelMemoryUsage(ggufOllamaData)

		// Check if the input is a Hugging Face model reference (format: huggingface.co/<repo>/<file>)
	} else if strings.Contains(modelPath, "huggingface.co") {
		fmt.Println("Input is a Hugging Face model.")

		// Parse the URL to extract the repository and filename
		u, err := url.Parse(modelPath)
		if err != nil {
			return nil, fmt.Errorf("invalid Hugging Face URL: %v", err)
		}

		// Example URL: https://huggingface.co/<repo>/<file>.gguf
		parts := strings.Split(u.Path, "/")
		if len(parts) < 3 {
			return nil, fmt.Errorf("invalid Hugging Face model format. Expected format: huggingface.co/<repo>/<file>")
		}

		repo := parts[1] // Repository name
		file := parts[2] // File name

		ggufHuggingFaceData, err := ggufparser.ParseGGUFFileFromHuggingFace(ctx, repo, file)
		if err != nil {
			return nil, fmt.Errorf("error parsing GGUF file from Hugging Face: %v", err)
		}
		return estimateModelMemoryUsage(ggufHuggingFaceData)

		// Otherwise, assume the input is a file path
	} else if fileExists(modelPath) {
		fmt.Println("Input is a file path.")
		ggufData, err := ggufparser.ParseGGUFFile(modelPath)
		if err != nil {
			return nil, fmt.Errorf("error parsing GGUF file from file path: %v", err)
		}
		return estimateModelMemoryUsage(ggufData)
	}

	return nil, fmt.Errorf("unsupported input type")
}

// Helper function to check if the string is a valid URL
func isURL(input string) bool {
	_, err := url.ParseRequestURI(input)
	return err == nil
}

// Helper function to check if the input is a valid file path
func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func estimateModelMemoryUsage(ggufFile *ggufparser.GGUFFile) (*ModelEstimate, error) {

	if ggufFile == nil {
		fmt.Printf("Error Invalid GGUF File \n")

		// Invalid ModelPath return nil and use default values
		return nil, nil
	}

	//
	llamacppRunEstimateOpts := []ggufparser.LLaMACppRunEstimateOption{}
	//
	llamacppRunEstimate := ggufFile.EstimateLLaMACppRun(llamacppRunEstimateOpts...)

	// Summarize the item with mmap and footprint values
	summary := llamacppRunEstimate.SummarizeItem(true, nonUMARamFootprint, nonUMAVramFootprint)
	// Fetch architecture, metadata, and tokenizer from GGUF file
	architecture := ggufFile.Architecture()
	metadata := ggufFile.Metadata()
	tokenizer := ggufFile.Tokenizer()

	// Construct the JSON payload
	payload := ModelEstimate{
		Estimate: ModelEstimateItems{
			Items: []ModelMemory{
				{
					OffloadLayers: summary.OffloadLayers,
					FullOffloaded: summary.FullOffloaded,
					RAM: EstimateRAM{
						UMA:    uint64(summary.RAM.UMA),
						NonUMA: uint64(summary.RAM.NonUMA),
					},
					VRAMs: []EstimateVRAM{
						{
							UMA:    uint64(summary.VRAMs[0].UMA),
							NonUMA: uint64(summary.VRAMs[0].NonUMA),
						},
					},
				},
			},
			Type:              architecture.Type,
			Architecture:      architecture.Architecture,
			ContextSize:       int(llamacppRunEstimate.ContextSize),
			FlashAttention:    llamacppRunEstimate.FlashAttention,
			NoMMap:            llamacppRunEstimate.NoMMap,
			EmbeddingOnly:     llamacppRunEstimate.EmbeddingOnly,
			Distributable:     llamacppRunEstimate.Distributable,
			LogicalBatchSize:  llamacppRunEstimate.LogicalBatchSize,
			PhysicalBatchSize: llamacppRunEstimate.PhysicalBatchSize,
		},
		Architecture: Architecture{
			Type:                 metadata.Type,
			Architecture:         architecture.Architecture,
			MaximumContextLength: int(architecture.MaximumContextLength),
			EmbeddingLength:      int(architecture.EmbeddingLength),
			VocabularyLength:     int(architecture.VocabularyLength),
		},
		Metadata: Metadata{
			Type:                metadata.Type,
			Architecture:        metadata.Architecture,
			QuantizationVersion: int(metadata.QuantizationVersion),
			Name:                metadata.Name,
			License:             metadata.License,
			FileType:            int(metadata.FileType),
			LittleEndian:        metadata.LittleEndian,
			FileSize:            int64(metadata.FileSize),
			Parameters:          int64(metadata.Parameters),
		},
		Tokenizer: Tokenizer{
			Model:        tokenizer.Model,
			TokensLength: int(tokenizer.TokensLength),
			TokensSize:   int(tokenizer.TokensSize),
		},
	}

	return &payload, nil
}