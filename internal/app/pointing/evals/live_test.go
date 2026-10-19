//go:build evals

package evals

import (
	"bufio"
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/pointing"
	"personal-assistant/internal/base/thinking/naming/gliner"
)

// TestRealQuestionsThroughTheRealService : The whole thing end to end --
// the service, the client, the lists and the record -- over questions he
// actually asked.
//
// Needs both the name service and a file of questions, and skips without
// either, so this never fails a build. recent.txt is one question a
// line, and is not kept in the repository because it is his own words:
//
//	mysql ... -N -B -e "SELECT REPLACE(content, '\n', ' ') FROM messages
//	  WHERE role='user' AND kind='chat' ORDER BY created_at DESC LIMIT 30" \
//	  > internal/app/pointing/evals/recent.txt
func TestRealQuestionsThroughTheRealService(t *testing.T) {
	resp, err := http.Get(gliner.DefaultURL + "/health")
	if err != nil {
		t.Skipf("nothing serving at %s", gliner.DefaultURL)
	}
	resp.Body.Close()

	file, err := os.Open("recent.txt")
	if err != nil {
		t.Skip("no recent.txt to read questions from")
	}
	defer file.Close()

	_, record := load(t)
	p := &pointing.Pointer{
		Finder:    gliner.New(gliner.Config{}),
		Known:     record,
		Assistant: Assistant,
	}
	ctx := pointing.WithUser(context.Background(), "usr_evals")

	var asked int
	var slowest time.Duration
	scan := bufio.NewScanner(file)
	for scan.Scan() {
		said := strings.TrimSpace(scan.Text())
		if said == "" {
			continue
		}
		asked++

		started := time.Now()
		points, err := p.Points(ctx, said)
		took := time.Since(started)
		if took > slowest {
			slowest = took
		}
		if err != nil {
			t.Fatalf("Points(%q): %v", said, err)
		}

		his := pointing.Terms(points, pointing.His)
		world := pointing.Terms(points, pointing.Worldly)
		if len(his) == 0 && len(world) == 0 {
			continue
		}
		t.Logf("%-56s his%v world%v", cut(said, 56), his, world)
	}
	t.Logf("%d questions, slowest %v", asked, slowest.Round(time.Millisecond))

	// Every span is cut from the sentence, so nothing can come back
	// that was never said. The check that proves it is worth having.
	if slowest > 500*time.Millisecond {
		t.Errorf("slowest was %v, which is too much to put in front of an answer", slowest)
	}
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
