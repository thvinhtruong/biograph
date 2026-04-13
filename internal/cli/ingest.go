package cli

import (
	"fmt"
	"os"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/truongvinh/biograph/internal/config"
	"github.com/truongvinh/biograph/internal/ingestion"
	"github.com/truongvinh/biograph/internal/storage"
)

var ingestCmd = &cobra.Command{
	Use:   "ingest <path-to-pdf>",
	Short: "Ingest a lecture PDF into the knowledge graph",
	Args:  cobra.ExactArgs(1),
	RunE:  runIngest,
}

func init() {
	ingestCmd.Flags().String("course", "", "Course tag (e.g., deep_learning)")
	ingestCmd.Flags().String("exam-date", "", "Exam date YYYY-MM-DD (e.g., 2026-06-15)")
	ingestCmd.Flags().Bool("force-vlm", false, "Skip text-first pass, send all pages to VLM")
	ingestCmd.Flags().Int("workers", 0, "Number of concurrent VLM workers (default: num_cpu)")
	ingestCmd.Flags().String("provider", "", "VLM provider: openai, anthropic, ollama")
}

func runIngest(cmd *cobra.Command, args []string) error {
	pdfPath := args[0]

	if _, err := os.Stat(pdfPath); os.IsNotExist(err) {
		return fmt.Errorf("PDF not found: %s", pdfPath)
	}

	cfg, err := config.Load(viper.GetViper())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	course, _ := cmd.Flags().GetString("course")
	examDate, _ := cmd.Flags().GetString("exam-date")
	forceVLM, _ := cmd.Flags().GetBool("force-vlm")
	workers, _ := cmd.Flags().GetInt("workers")
	provider, _ := cmd.Flags().GetString("provider")

	if course == "" {
		course = viper.GetString("ingestion.default_course")
	}
	if provider != "" {
		cfg.LLM.Provider = provider
	}
	if workers > 0 {
		cfg.Ingestion.Workers = workers
	}

	db, err := storage.Open(cfg)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	opts := ingestion.Options{
		PDFPath:  pdfPath,
		Course:   course,
		ExamDate: examDate,
		ForceVLM: forceVLM,
		Config:   cfg,
	}

	log.Info().Str("pdf", pdfPath).Str("course", course).Msg("starting ingestion")

	pipeline := ingestion.NewPipeline(db, cfg)
	result, err := pipeline.Run(opts)
	if err != nil {
		return fmt.Errorf("ingestion failed: %w", err)
	}

	fmt.Printf("\nIngestion complete:\n")
	fmt.Printf("  Pages processed:  %d\n", result.PagesProcessed)
	fmt.Printf("  VLM pages:        %d\n", result.VLMPages)
	fmt.Printf("  Entities found:   %d\n", result.EntitiesFound)
	fmt.Printf("  Edges created:    %d\n", result.EdgesCreated)
	fmt.Printf("  Notes written:    %d\n", result.NotesWritten)

	return nil
}
