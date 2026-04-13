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
	Short: "Show knowledge graph statistics",
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(viper.GetViper())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	db, err := storage.Open(cfg)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	stats, err := db.Stats()
	if err != nil {
		return fmt.Errorf("failed to fetch stats: %w", err)
	}

	fmt.Println("=== BioGraph Status ===")
	fmt.Printf("Nodes:       %d\n", stats.NodeCount)
	fmt.Printf("Edges:       %d\n", stats.EdgeCount)
	fmt.Printf("Courses:     %d\n", stats.CourseCount)
	fmt.Printf("Queries:     %d\n", stats.QueryCount)
	fmt.Println()
	fmt.Println("Top entities by activation weight:")
	for i, n := range stats.TopNodes {
		fmt.Printf("  %d. %-30s  weight=%.3f  course=%s\n", i+1, n.DisplayName, n.Weight, n.Course)
	}
	fmt.Println()
	fmt.Println("Upcoming exams:")
	for _, e := range stats.UpcomingExams {
		fmt.Printf("  %-20s  %s  (%d days)\n", e.Course, e.ExamDate, e.DaysUntil)
	}

	return nil
}
