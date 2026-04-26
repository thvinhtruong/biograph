package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/truongvinh/my-tu-brain/internal/config"
	"github.com/truongvinh/my-tu-brain/internal/search"
	"github.com/truongvinh/my-tu-brain/internal/storage"
)

var searchCmd = &cobra.Command{
	Use:   "search <term>",
	Short: "Full-text search across ingested concepts",
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
		return fmt.Errorf("load config: %w", err)
	}

	db, err := storage.Open(cfg)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	nodes, err := search.Search(db, term, course, limit)
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	if len(nodes) == 0 {
		fmt.Println("No results found.")
		return nil
	}

	fmt.Printf("Found %d result(s) for %q:\n\n", len(nodes), term)
	for i, n := range nodes {
		fmt.Printf("%d. %s [%s] — %s\n", i+1, n.DisplayName, n.Category, n.Course)
		fmt.Printf("   %s\n\n", truncate(n.Definition, 120))
	}

	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
