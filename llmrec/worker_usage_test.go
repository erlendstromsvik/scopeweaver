package llmrec

import (
	"testing"

	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/transcript"
	"github.com/google/uuid"
)

func TestWorkerOverrideMetersCompleteAndStreamWithoutBodyRecording(t *testing.T) {
	pg := openTestDB(t)
	defer pg.Close()
	for _, mode := range []string{"complete", "stream", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			profile := "judge-metering-" + uuid.NewString()
			t.Cleanup(func() { _, _ = pg.Exec(`DELETE FROM llm_usage WHERE profile_name=$1`, profile) })
			ctx := WithWorker(transcript.WithSessionID(t.Context(), "exp0-planner"), " judge ")
			ctx = WithWorker(ctx, " ") // An empty override preserves the inherited lane.
			recorder := Wrap(meteredStreamProvider{}, pg, "fixture", profile, "", "", func() bool { return false })
			input, output := 7, 3
			if mode == "complete" {
				if _, _, _, err := recorder.Complete(ctx, llm.CompletionRequest{}); err != nil {
					t.Fatal(err)
				}
			} else {
				input, output = 23, 5
				for event, err := range recorder.Stream(ctx, llm.CompletionRequest{}) {
					if err != nil {
						t.Fatal(err)
					}
					if mode == "cancelled" && event.Type == llm.SEMessageDelta {
						break
					}
				}
			}
			var count, gotInput, gotOutput int
			var worker string
			if err := pg.QueryRow(`SELECT count(*),max(input_tokens),max(output_tokens),max(worker) FROM llm_usage WHERE profile_name=$1`, profile).Scan(&count, &gotInput, &gotOutput, &worker); err != nil {
				t.Fatal(err)
			}
			if count != 1 || gotInput != input || gotOutput != output || worker != "judge" {
				t.Fatalf("count=%d usage=%d/%d worker=%q", count, gotInput, gotOutput, worker)
			}
		})
	}
}
