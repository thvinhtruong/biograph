package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/truongvinh/biograph/internal/config"
	"github.com/truongvinh/biograph/internal/search"
	"github.com/truongvinh/biograph/internal/storage"
)

var searchCmd = &cobra.Command{
	Use:   "search <term>",
	Short: "Full-text search across the knowledge graph",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runSearch,
}

func init() {
	searchCmd.Flags().Int("limit", 10, "Maximum results to return")
	searchCmd.Flags().String("course", "", "Filter by course")
}

func runSearch(cmd *cobra.Command, args []string) error {
	term := strings.Join(args, " ")
	limit, _ := cmd.Flags().GetInt("limit")
	course, _ := cmd.Flags().GetString("course")

	cfg, err := config.Load(viper.GetViper())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	db, err := storage.Open(cfg)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	idx, err := search.OpenIndex(cfg)
	if err != nil {
		return fmt.Errorf("failed to open search index: %w", err)
	}
	defer idx.Close()

	results, err := idx.Search(term, course, limit)
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	if len(results) == 0 {
		fmt.Println("No results found.")
		return nil
	}

	fmt.Printf("Found %d results for %q:\n\n", len(results), term)
	for i, r := range results {
		fmt.Printf("%d. [%.2f] %s (%s)\n", i+1, r.Score, r.DisplayName, r.Course)
		fmt.Printf("   %s\n\n", truncate(r.Definition, 120))
	}

	_ = db
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
