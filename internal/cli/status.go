package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/truongvinh/biograph/internal/config"
	"github.com/truongvinh/biograph/internal/storage"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show knowledge base statistics",
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(viper.GetViper())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := storage.Open(cfg)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	stats, err := db.Stats()
	if err != nil {
		return fmt.Errorf("fetch stats: %w", err)
	}

	fmt.Println("=== BioGraph Status ===")
	fmt.Printf("Concepts indexed:  %d\n", stats.NodeCount)
	fmt.Printf("Courses:           %d\n", stats.CourseCount)
	fmt.Printf("Queries logged:    %d\n", stats.QueryCount)
	fmt.Printf("Content dir:       %s\n", cfg.Output.ContentDir)

	return nil
}
