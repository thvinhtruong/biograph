package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/truongvinh/my-tu-brain/internal/config"
	"github.com/truongvinh/my-tu-brain/internal/llm"
)

var quizCmd = &cobra.Command{
	Use:   "quiz",
	Short: "Interactive exam-style interrogation based on your lecture notes",
	RunE:  runQuiz,
}

func init() {
	quizCmd.Flags().String("course", "", "Course to quiz on (required)")
	quizCmd.Flags().String("lecture", "", "Filter to a specific lecture file (partial match)")
	quizCmd.MarkFlagRequired("course")
}

func runQuiz(cmd *cobra.Command, args []string) error {
	course, _ := cmd.Flags().GetString("course")
	lectureFilter, _ := cmd.Flags().GetString("lecture")

	cfg, err := config.Load(viper.GetViper())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Find matching lecture files
	courseDir := filepath.Join(cfg.Output.ContentDir, sanitiseCourseName(course))
	pattern := filepath.Join(courseDir, "*.md")
	files, err := filepath.Glob(pattern)
	if err != nil || len(files) == 0 {
		return fmt.Errorf("no lecture files found in %s — ingest some PDFs first", courseDir)
	}

	// Filter by lecture name if specified
	if lectureFilter != "" {
		var filtered []string
		for _, f := range files {
			if strings.Contains(strings.ToLower(filepath.Base(f)), strings.ToLower(lectureFilter)) {
				filtered = append(filtered, f)
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("no files matching %q in %s", lectureFilter, courseDir)
		}
		files = filtered
	}

	// Load ledger content (LLM section only — strip scratchpad)
	var sb strings.Builder
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		content := string(raw)
		// Strip scratchpad so personal notes don't confuse the examiner
		if idx := strings.Index(content, "<!-- my-tu-brain:scratchpad -->"); idx >= 0 {
			content = content[:idx]
		}
		sb.WriteString(content)
		sb.WriteString("\n\n")
	}

	if sb.Len() == 0 {
		return fmt.Errorf("could not read any lecture content")
	}

	fmt.Printf("Starting quiz for course: %s (%d file(s))\n", course, len(files))
	fmt.Println("Type your answer and press Enter. Type 'quit' or 'q' to exit.")
	fmt.Println(strings.Repeat("─", 60))

	client := llm.NewClient(cfg)
	system := reverseInterrogationPrompt(sb.String())

	// Conversation history for multi-turn context
	history := []llm.ChatMessage{}

	// Seed: ask the LLM to start the session
	history = append(history, llm.ChatMessage{
		Role:    "user",
		Content: "Begin the quiz session now.",
	})

	scanner := bufio.NewScanner(os.Stdin)

	for {
		reply, err := client.Chat(system, history)
		if err != nil {
			return fmt.Errorf("LLM error: %w", err)
		}

		fmt.Printf("\n%s\n\n", reply)
		history = append(history, llm.ChatMessage{Role: "assistant", Content: reply})

		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "q" || input == "quit" || input == "exit" {
			fmt.Println("\nQuiz session ended.")
			break
		}

		history = append(history, llm.ChatMessage{Role: "user", Content: input})
	}

	return nil
}

// sanitiseCourseName converts a course flag value to a directory name.
// Mirrors ingestion.sanitiseCourse so paths are consistent.
func sanitiseCourseName(course string) string {
	replacer := strings.NewReplacer(" ", "_", "/", "_", "\\", "_", ":", "_")
	return strings.ToLower(replacer.Replace(course))
}

func reverseInterrogationPrompt(ledger string) string {
	return `You are a rigorous, uncompromising examiner for the Artificial Intelligence and
Machine Learning Master's program at TU Darmstadt.

Your task is to actively interrogate the student based on the provided study ledger.
Your goal is not to test rote memorization, but to test deep theoretical understanding,
mathematical intuition, and awareness of algorithmic failure modes.

Here are your absolute rules of engagement:

1. **One Question at a Time:** NEVER ask multiple questions at once. Ask exactly one
   highly targeted question, wait for the student's response, grade it, then ask the next.
2. **Escalating Difficulty:**
   - Start with a Warm-Up (e.g., "Explain the core mechanism of X in your own words").
   - Move to Mathematical Rigor (e.g., "Derive the loss function for Y").
   - End with Boundary Testing (e.g., "If the data violates assumption Z, how does the complexity degrade?").
3. **The Grading Protocol:** When the student answers, reply with a strict evaluation:
   - **[PASS / FAIL / PARTIAL]**
   - **Critique:** Point out exactly where their theoretical logic was weak, imprecise,
     or mathematically incorrect. Be direct and academic, not polite.
   - **The Correct Formalism:** Provide the exact mathematical or theoretical standard
     they should have used.
4. **Formatting:** Use bold for emphasis, LaTeX blocks ($$...$$) for all math,
   and keep output concise for clean terminal rendering.

Study ledger for this session:
<ledger>
` + ledger + `
</ledger>

Begin by asking your first warm-up question.`
}
