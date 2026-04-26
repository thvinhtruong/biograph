package cli

import (
	"fmt"
	"os"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/truongvinh/my-tu-brain/internal/config"
	"github.com/truongvinh/my-tu-brain/internal/ingestion"
	"github.com/truongvinh/my-tu-brain/internal/storage"
)

var ingestCmd = &cobra.Command{
	Use:   "ingest <path-to-pdf>",
	Short: "Ingest a lecture PDF: extract, synthesise, and write First Thoughts ledger",
	Args:  cobra.ExactArgs(1),
	RunE:  runIngest,
}

func init() {
	ingestCmd.Flags().String("course", "", "Course tag (e.g., deep_learning)")
	ingestCmd.Flags().String("exam-date", "", "Exam date YYYY-MM-DD (e.g., 2026-06-15)")
	ingestCmd.Flags().Int("workers", 0, "Concurrent VLM workers (default: num_cpu)")
	ingestCmd.Flags().String("provider", "", "LLM provider override: anthropic, gemini, openai, ollama")
}

func runIngest(cmd *cobra.Command, args []string) error {
	pdfPath := args[0]

	if _, err := os.Stat(pdfPath); os.IsNotExist(err) {
		return fmt.Errorf("PDF not found: %s", pdfPath)
	}

	cfg, err := config.Load(viper.GetViper())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	course, _ := cmd.Flags().GetString("course")
	examDate, _ := cmd.Flags().GetString("exam-date")
	workers, _ := cmd.Flags().GetInt("workers")
	provider, _ := cmd.Flags().GetString("provider")

	if provider != "" {
		cfg.LLM.Provider = provider
	}
	if workers > 0 {
		cfg.Ingestion.Workers = workers
	}

	db, err := storage.Open(cfg)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	log.Info().Str("pdf", pdfPath).Str("course", course).Msg("starting ingestion")

	pipeline := ingestion.NewPipeline(db, cfg)
	result, err := pipeline.Run(ingestion.Options{
		PDFPath:  pdfPath,
		Course:   course,
		ExamDate: examDate,
		Config:   cfg,
	})
	if err != nil {
		return fmt.Errorf("ingestion failed: %w", err)
	}

	fmt.Printf("\nIngestion complete:\n")
	fmt.Printf("  Pages processed:  %d (%d via VLM)\n", result.PagesProcessed, result.VLMPages)
	fmt.Printf("  Nodes stored:     %d\n", result.NodesStored)
	fmt.Printf("  Ledger written:   %s\n", result.OutputPath)

	return nil
}
