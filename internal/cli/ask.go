package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/truongvinh/biograph/internal/config"
	"github.com/truongvinh/biograph/internal/graph"
	"github.com/truongvinh/biograph/internal/llm"
	"github.com/truongvinh/biograph/internal/search"
	"github.com/truongvinh/biograph/internal/storage"
)

var askCmd = &cobra.Command{
	Use:   "ask <question>",
	Short: "Ask a question using spreading activation retrieval",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runAsk,
}

func init() {
	askCmd.Flags().Int("hops", 0, "Max traversal hops (default: from config)")
	askCmd.Flags().String("course", "", "Filter to a specific course")
}

func runAsk(cmd *cobra.Command, args []string) error {
	query := strings.Join(args, " ")

	cfg, err := config.Load(viper.GetViper())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	hops, _ := cmd.Flags().GetInt("hops")
	if hops > 0 {
		cfg.Activation.MaxHops = hops
	}
	course, _ := cmd.Flags().GetString("course")

	db, err := storage.Open(cfg)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Step 1: Find starting nodes via hybrid router
	router := search.NewRouter(db, cfg)
	startNodes, err := router.Route(query, course)
	if err != nil {
		return fmt.Errorf("routing failed: %w", err)
	}
	if len(startNodes) == 0 {
		fmt.Println("No relevant concepts found in your knowledge graph for this query.")
		return nil
	}

	// Step 2: Spreading activation
	engine := graph.NewActivationEngine(db, cfg)
	activated, err := engine.Activate(startNodes)
	if err != nil {
		return fmt.Errorf("activation failed: %w", err)
	}

	// Step 3: Pack context within token budget
	packer := graph.NewContextPacker(cfg)
	context := packer.Pack(activated)

	// Step 4: Generate answer
	client := llm.NewClient(cfg)
	answer, err := client.Answer(query, context)
	if err != nil {
		return fmt.Errorf("LLM call failed: %w", err)
	}

	// Step 5: Reinforce activated paths (Hebbian learning)
	activatedIDs := make([]string, len(activated))
	for i, n := range activated {
		activatedIDs[i] = n.ID
	}
	engine.Reinforce(activatedIDs)

	fmt.Println(answer)
	fmt.Printf("\nSources: %s\n", strings.Join(startNodes, ", "))

	return nil
}
