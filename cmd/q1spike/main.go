// Command q1spike answers Q1 of the exactly-once gate design: does the
// Temporal dev server under test accept UpdateWorkflow? It starts a scratch
// workflow with an Update handler, calls Update once (accepted), then calls
// a second time with a different UpdateID (must be rejected by the handler's
// own state check). Prints SPIKE-UPDATE-OK or the failure.
package main

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

type reviewResult struct {
	Accepted bool
	Decision string
	Reason   string
}

func scratchWorkflow(ctx workflow.Context) (reviewResult, error) {
	decided := false
	var decision string
	err := workflow.SetUpdateHandler(ctx, "scratch-review", func(ctx workflow.Context, d string) (string, error) {
		if decided {
			return "", fmt.Errorf("already_decided: %s", decision)
		}
		decided = true
		decision = d
		return "recorded:" + d, nil
	})
	if err != nil {
		return reviewResult{}, err
	}
	workflow.GetSignalChannel(ctx, "done").Receive(ctx, nil)
	return reviewResult{Accepted: decided, Decision: decision}, nil
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c, err := client.Dial(client.Options{HostPort: "127.0.0.1:7233"})
	if err != nil {
		fmt.Printf("SPIKE-FAIL dial: %v\n", err)
		return
	}
	defer c.Close()

	w := worker.New(c, "q1-spike", worker.Options{})
	w.RegisterWorkflow(scratchWorkflow)
	go func() { _ = w.Run(nil) }()
	defer w.Stop()

	wid := fmt.Sprintf("q1-spike-%d", time.Now().UnixNano())
	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                                       wid,
		WorkflowTaskTimeout:                      10 * time.Second,
		TaskQueue:                                "q1-spike",
		WorkflowExecutionTimeout:                 60 * time.Second,
		WorkflowIDReusePolicy:                    enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
	}, scratchWorkflow)
	if err != nil {
		fmt.Printf("SPIKE-FAIL start: %v\n", err)
		return
	}

	handle, err := c.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID:   wid,
		RunID:        run.GetRunID(),
		UpdateName:   "scratch-review",
		UpdateID:     "k1",
		Args:         []interface{}{"approved"},
		WaitForStage: client.WorkflowUpdateStageCompleted,
	})
	if err != nil {
		fmt.Printf("SPIKE-UPDATE-REJECTED first update: %v (%T)\n", err, err)
		return
	}
	var v1 string
	if err := handle.Get(ctx, &v1); err != nil {
		fmt.Printf("SPIKE-UPDATE-REJECTED get(k1): %v (%T)\n", err, err)
		return
	}
	fmt.Printf("first update (k1) -> %q\n", v1)

	h2, err := c.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID:   wid,
		RunID:        run.GetRunID(),
		UpdateName:   "scratch-review",
		UpdateID:     "k2",
		Args:         []interface{}{"rejected"},
		WaitForStage: client.WorkflowUpdateStageCompleted,
	})
	if err == nil {
		// Rejections surface at Get (the update stage completed = handler ran and failed).
		var v2 string
		err = h2.Get(ctx, &v2)
	}
	if err == nil {
		fmt.Print("SPIKE-FAIL second update returned nil error - handler state check did not run\n")
		return
	}
	if _, ok := err.(*serviceerror.NotFound); ok {
		fmt.Printf("SPIKE-UPDATE-REJECTED second update: NotFound (server lacks Update support): %v\n", err)
		return
	}
	fmt.Printf("second update (k2) rejected as designed: %v\n", err)
	_ = c.SignalWorkflow(ctx, wid, run.GetRunID(), "done", nil)
	fmt.Println("SPIKE-UPDATE-OK")
}
