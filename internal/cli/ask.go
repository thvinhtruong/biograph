package cli

import (
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/truongvinh/biograph/internal/config"
	"github.com/truongvinh/biograph/internal/llm"
	"github.com/truongvinh/biograph/internal/search"
	"github.com/truongvinh/biograph/internal/storage"
)

var askCmd = &cobra.Command{
	Use:   "ask <question>",
	Short: "Ask a question answered from your ingested notes",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runAsk,
}

func init() {
	askCmd.Flags().String("course", "", "Filter context to a specific course")
	askCmd.Flags().Int("limit", 10, "Number of nodes to retrieve as context")
}

func runAsk(cmd *cobra.Command, args []string) error {
	query := strings.Join(args, " ")

	cfg, err := config.Load(viper.GetViper())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	course, _ := cmd.Flags().GetString("course")
	limit, _ := cmd.Flags().GetInt("limit")

	db, err := storage.Open(cfg)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	// Step 1: FTS5 retrieval
	nodes, err := search.Search(db, query, course, limit)
	if err != nil || len(nodes) == 0 {
		if err != nil {
			log.Warn().Err(err).Msg("search failed")
		}
		fmt.Println("No relevant concepts found for this query. Try ingesting more content first.")
		return nil
	}

	// Step 2: Assemble context from retrieved nodes
	var sb strings.Builder
	nodeIDs := make([]string, 0, len(nodes))
	for _, n := range nodes {
		sb.WriteString(fmt.Sprintf("## %s (%s)\n%s\n", n.DisplayName, n.Category, n.Definition))
		if len(n.RawLatex) > 0 {
			sb.WriteString("Equations: " + strings.Join(n.RawLatex, " | ") + "\n")
		}
		sb.WriteString("\n")
		nodeIDs = append(nodeIDs, n.ID)
	}

	// Step 3: Generate answer
	client := llm.NewClient(cfg)
	answer, err := client.Answer(query, sb.String())
	if err != nil {
		return fmt.Errorf("LLM call failed: %w", err)
	}

	fmt.Println(answer)
	fmt.Printf("\n[Sources: %s]\n", strings.Join(nodeIDs, ", "))

	// Step 4: Log query
	if err := db.LogQuery(query, nodeIDs); err != nil {
		log.Warn().Err(err).Msg("query log failed")
	}

	return nil
}
